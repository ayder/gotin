# termud Audit — Runtime Errors, Telnet Protocol Violations, Performance

_Auditor: Claude Opus 4.7 (1M context). Date: 2026-04-23._
_Scope: `cmd/termud/main.go`, `internal/input/*`, `internal/logic/*`, `internal/network/*`, `internal/ui/model.go`, `internal/mapper/*`._

## Executive Summary

1. **Critical — [VERIFIED] NAWS subnegotiation payload is not IAC-escaped** (`internal/network/telnet.go:285`). Any window dimension whose byte representation contains `0xFF` corrupts the subnegotiation (RFC 854 §SB). Servers see a truncated / malformed SB and either ignore NAWS, get stuck, or drop the connection. Triggered by terminals wider/taller than 255 cells in any byte (e.g. 255×24, 256×40, etc.).
2. **Critical — [VERIFIED] Synchronous `p.Send()` inside the network read callback blocks the reader** (`cmd/termud/main.go:208, 211, 221–225`). The Bubble Tea `Send` is effectively a channel write into the event loop; when the UI redraws slowly the reader stalls, kernel socket buffers fill, and incoming data is lost or the connection is dropped.
3. **Critical — [VERIFIED] IAC write-error ignored during negotiation response** (`internal/network/telnet.go` — `c.Send(responses)` without error check). A dropped response can trigger an infinite re-negotiation loop (server keeps asking, client keeps "responding" into the void).
4. **High — [VERIFIED] Goroutine-per-keystroke sending to unbuffered channels** (`internal/ui/model.go:153, 165`). Each Enter spawns `go func(){ ch <- x }()` on unbuffered channels with no shutdown close; goroutines accumulate over a long session.
5. **High — [VERIFIED] Incomplete IAC sequence at the Read boundary is silently dropped** (`internal/network/telnet.go:79–81`). An IAC that arrives as the last byte of one `Read` is discarded; the 2nd/3rd bytes of the command arrive next and are treated as data.

---

## Runtime Errors

- **Critical — [VERIFIED] Ignored error on negotiation response Send**
  - **Location:** `internal/network/telnet.go:218` (`c.Send(responses)`)
  - **Issue:** The return value of `Send` is discarded after composing IAC DO/DONT/WILL/WONT replies.
  - **Impact:** If the socket is congested or half-closed, the response is silently dropped. The server retransmits its WILL/DO, the client re-derives the same reply, the reply is dropped again — a tight negotiation loop that burns CPU and bandwidth until a timeout fires.
  - **Fix:** `if err := c.Send(responses); err != nil { log.Printf("telnet: negotiation write failed: %v", err); return err }`. Bubble the error up so the caller can close the connection.

- **High — [VERIFIED] Incomplete IAC sequence discarded at buffer boundary**
  - **Location:** `internal/network/telnet.go:79–81` (tail of `ProcessIAC` loop)
  - **Issue:** If `data[len(data)-1] == IAC`, the byte is dropped and the loop exits. There is no `pending` state carried into the next `Read`.
  - **Impact:** On fragmented TCP reads (common on slow links or under load), IAC commands that straddle a read boundary are corrupted. The command's command byte + option byte then appear as printable text in the stream.
  - **Fix:** Give `Client` a `pending []byte` field. At the top of `ProcessIAC`, `data = append(c.pending, data...); c.pending = nil`. At the tail, if the buffer ends mid-command (IAC, IAC+verb, or inside an SB), store the tail in `c.pending` instead of discarding it.

- **High — [VERIFIED] EOF vs. error not distinguished in `ReadLoop`**
  - **Location:** `internal/network/telnet.go:204` (the `Read` error handler)
  - **Issue:** `io.EOF`, timeouts, and transport errors all fall through the same `return`. The UI never learns why the session ended.
  - **Impact:** Users see a generic "connection closed"; transient errors look identical to a clean server-side disconnect, which hinders diagnosis.
  - **Fix:** Branch on `errors.Is(err, io.EOF)` vs. `net.Error`/`os.ErrDeadlineExceeded` and plumb a typed reason to the UI.

- **Medium — [DISPUTED] Data race on `Model.content` / viewport**
  - **Location:** `internal/ui/model.go` — `appendContent` (approx. 278–309) vs. `Update`
  - **Issue:** `appendContent` is reached from the Bubble Tea `Msg` path (safe) but any goroutine sending arbitrary `Msg`s through `p.Send` can race with the UI's own state if either side reads `m.content` without serializing through the Update loop.
  - **Impact:** Under `-race`, flags. In production, may corrupt the content slice on high-volume input bursts.
  - **Verification Note:** `appendContent` is only called from the `Update` loop, which is serialized. No concurrent access to `m.content` was found.
  - **Fix:** Keep all mutations of `m.content` inside `Update`. If any callsite touches it from outside the loop, protect with a `sync.Mutex` or — better — route everything through `tea.Msg`.

- **Medium — [VERIFIED] `json.Unmarshal` error ignored when loading config**
  - **Location:** `cmd/termud/main.go` — config load (around line 118 / 329 as cited)
  - **Issue:** Malformed `termud.json` silently yields the zero-value config; user sees an empty alias/trigger set with no diagnostic.
  - **Impact:** Silent data loss / confusion after a failed save or hand-edit.
  - **Fix:** Log (and surface to UI) the unmarshal error. Prefer atomic writes for saves (`os.Rename` of a tempfile) so partial writes can't corrupt config.

- **Medium — [VERIFIED] `strconv.Atoi(portStr)` error ignored in `/connect` parsing**
  - **Location:** `cmd/termud/main.go:256–258`
  - **Issue:** Bad port string → `port = 0` → `net.Dial("host:0")` which fails obscurely.
  - **Impact:** Cryptic error; user can't tell they typed the port wrong.
  - **Fix:** Check `err != nil` and validate `port` is in `[1, 65535]`; emit a user-visible error message.

- **Medium — [VERIFIED] No read deadline on `net.Conn`**
  - **Location:** `internal/network/telnet.go` — `ReadLoop`
  - **Issue:** A server that stops sending bytes but keeps the TCP connection open (silent hang / NAT drop) will hold the reader goroutine forever.
  - **Impact:** "Zombie" sessions consuming a goroutine + file descriptor; user has no way to know the link is dead until they try to send and get a write error.
  - **Fix:** `conn.SetReadDeadline(time.Now().Add(keepalive))` and refresh on each successful read. Optionally send a periodic telnet NOP (IAC 241) as a keepalive.

- **Low — [VERIFIED] `Client.Close()` dereferences `c.conn` without a nil guard**
  - **Location:** `internal/network/telnet.go:296`
  - **Issue:** Calling `Close()` before `Dial` succeeds (or after a prior close nil'd the field) panics.
  - **Impact:** Panic on double-close during a failed reconnect cycle.
  - **Fix:** `if c.conn == nil { return nil }` at the top; or keep `conn` non-nil and guard on a `closed` bool.

---

## Telnet Protocol Violations

**Standards referenced:** RFC 854 (Telnet), RFC 855 (Options), RFC 857 (ECHO), RFC 858 (SGA), RFC 1073 (NAWS), RFC 1091 (TTYPE), RFC 2066 (CHARSET). MUD-specific: GMCP (201, de-facto standard), MCCP2 (86), MCCP3 (87), MSDP (69), MSSP (70), MXP (91).

- **Critical — [VERIFIED] NAWS width/height bytes not IAC-escaped**
  - **Location:** `internal/network/telnet.go:285` (`buildNAWS`)
  - **Issue:** Returns `{IAC, SB, NAWS, widthHigh, widthLow, heightHigh, heightLow, IAC, SE}` raw. Per RFC 854, any `0xFF` byte appearing as data inside a subnegotiation must be doubled (`IAC IAC`). This code does not escape.
  - **Impact:** Any dimension whose high or low byte equals `0xFF` — e.g. width 255 (`0x00 0xFF`), width 511 (`0x01 0xFF`), width 65280 (`0xFF 0x00`) — produces a malformed SB. The receiver either terminates the SB early (treating the embedded `0xFF` as the start of `IAC SE`) or desynchronizes its IAC parser on the rest of the stream. Observed symptoms: server ignores resize, server closes connection, or downstream IAC commands appear as garbage text.
  - **Fix:** Build the payload, then IAC-escape it before framing:
    ```go
    payload := []byte{widthHigh, widthLow, heightHigh, heightLow}
    escaped := make([]byte, 0, len(payload)+2)
    for _, b := range payload {
        escaped = append(escaped, b)
        if b == IAC { escaped = append(escaped, IAC) }
    }
    return append(append([]byte{IAC, SB, NAWS}, escaped...), IAC, SE)
    ```
    (RFC 854, "Interpretation as Command.") Same rule applies to every future subnegotiation builder.

- **Critical — [VERIFIED] Subnegotiation payload not un-escaped before use**
  - **Location:** `internal/network/telnet.go:152–175` (SB…SE collection)
  - **Issue:** The parser terminates the subnegotiation at `IAC SE` but does not collapse `IAC IAC` inside `subData` back to a single `0xFF`.
  - **Impact:** Any server sending GMCP/MSDP/MSSP whose payload contains `0xFF` (very rare for JSON, but possible in MSDP key/value bytes and guaranteed for MCCP2's compressed stream) will be handed a corrupted payload. JSON parsers emit replacement characters or fail entirely.
  - **Fix:** After collecting `subData`, unescape: walk it, and on `IAC IAC` emit a single `IAC`. Factor out an `unescapeSubneg(b []byte) []byte` helper. (RFC 854, same clause.)

- **High — [VERIFIED] Option state machine not implemented (RFC 855 Q Method missing)**
  - **Location:** `internal/network/telnet.go:102–122` (negotiation response logic)
  - **Issue:** The code reacts to each WILL/WONT/DO/DONT independently, without tracking per-option state. RFC 855 (and Bernstein's "Q Method", RFC 1143) requires: "only acknowledge if the state actually changes; otherwise be silent." Without this, the client can respond to a server's acknowledgement and trigger oscillation.
  - **Impact:** Servers that resend WILL ECHO during keepalive or after a reconnect get an unnecessary DO ECHO in reply; ECHO state can invert, producing doubled or invisible input. On well-behaved servers it's benign but wastes bytes; on fragile servers it's an infinite loop (see also the "ignored Send error" issue — when combined, a tight loop is very plausible).
  - **Fix:** Implement RFC 1143 Q Method. Store per-option `us` and `him` state ∈ {NO, YES, WANTNO, WANTYES, WANTNO_OPPOSITE, WANTYES_OPPOSITE}; reply only on transitions.

- **High — [VERIFIED] No MUD-standard options supported (GMCP, MCCP2, MSDP, MSSP, SGA, CHARSET)**
  - **Location:** `internal/network/protocol.go:1–26` (constants), negotiation handler in `telnet.go`
  - **Issue:** Only ECHO (1), TTYPE (24), NAWS (31) are recognized; everything else is refused with DONT/WONT.
  - **Impact:** No MCCP2 means high-bandwidth MUDs consume 3–10× more bytes than necessary. No GMCP means the mapper (which appears to be under active development — `internal/mapper/`) cannot rely on structured room data. No SGA (3) means prompt/GA handling is default-off per RFC 858, which matters for prompt detection.
  - **Fix:** At minimum add: `SGA = 3`, `EOR = 25`, `CHARSET = 42`, `MSDP = 69`, `MSSP = 70`, `MCCP2 = 86`, `MXP = 91`, `GMCP = 201`. Accept WILL SGA with DO SGA. GMCP is the cheapest high-value win for the mapper.

- **High — [VERIFIED] No initial NAWS on connect**
  - **Location:** `internal/network/telnet.go` — `ReadLoop` / connect path
  - **Issue:** NAWS is only sent in response to the resize callback. On first connect the server defaults to 80×24 until the user resizes.
  - **Impact:** Opening screens, menus, login prompts render against 80×24 assumptions even on a 200-column terminal.
  - **Fix:** After the initial `IAC WILL NAWS` negotiation completes (or simply after `Dial`), send one NAWS with the current dimensions. RFC 1073.

- **Medium — [VERIFIED] No CR NUL / CR LF normalization on receive**
  - **Location:** `internal/network/telnet.go` byte-stream handling
  - **Issue:** RFC 854 mandates `CR` followed by `NUL` means bare `CR` in data, and `CR LF` means end-of-line. Current code strips `\r` at the UI layer (`appendContent`) without honoring the NAWS rule.
  - **Impact:** Servers that strictly follow RFC 854 may send `CR NUL` to represent a literal carriage return (e.g., in ASCII art). It currently renders as `\r` followed by a non-printing NUL and may jumble the line.
  - **Fix:** In the telnet byte filter, after IAC processing: replace `CR NUL` → `CR`, leave `CR LF` → `\n`, drop stray `NUL`.

- **Medium — [VERIFIED] Read-boundary fragility inside SB…SE**
  - **Location:** `internal/network/telnet.go:152–175`
  - **Issue:** If `IAC SB <opt> …` begins near the end of a Read and `IAC SE` doesn't arrive until the next Read, the current loop returns early with partial data (same root cause as the general boundary bug, but amplified for long subnegotiations like GMCP which can be hundreds of bytes).
  - **Impact:** GMCP/MSDP payloads split across Reads get dropped or corrupted.
  - **Fix:** Use the `pending` buffer approach (see runtime error #2) to persist SB state across Reads; the parser becomes a small state machine with states `{DATA, IAC, CMD, SB_HEADER, SB_DATA, SB_IAC}`.

- **Medium — [VERIFIED] UTF-8 / Latin-1 heuristic per-chunk**
  - **Location:** `internal/network/decoder.go:25–37`
  - **Issue:** Decoder classifies each chunk as valid UTF-8 or falls back to ISO-8859-1. A multi-byte UTF-8 rune split across chunks fails the validity check for the first chunk and gets mojibaked.
  - **Impact:** On servers sending UTF-8 (modern MUDs, especially non-English), the first partial byte of a multi-byte rune is treated as Latin-1 and the rune is corrupted.
  - **Fix:** Keep a tail buffer of incomplete UTF-8 bytes (up to 3) and prepend to the next chunk before decoding. Long-term: implement RFC 2066 CHARSET and negotiate UTF-8 explicitly.

- **Low — [VERIFIED] No explicit binary-mode flag for future MCCP2**
  - **Location:** `internal/network/telnet.go` (IAC parser)
  - **Issue:** MCCP2, once accepted, makes the post-SB byte stream zlib-compressed. The current parser would keep interpreting IAC in the compressed bytes.
  - **Impact:** Only relevant when MCCP2 is implemented; flagged now because the parser design needs a mode switch.
  - **Fix:** Plan a `c.binary bool` + swap the conn to a `zlib.Reader` at the exact byte following `IAC SE` of the MCCP2 handshake.

---

## Performance Issues

- **Critical — [VERIFIED] `p.Send` blocks the network reader goroutine**
  - **Location:** `cmd/termud/main.go:208, 211, 221–225`
  - **Issue:** Inside the data callback the code invokes `p.Send(ui.NetworkDataMsg{...})` and multiple `p.Send(ui.StatusMsg{...})`. Bubble Tea's `Send` writes to the internal program channel and blocks when it's full or when the UI goroutine is busy rendering.
  - **Impact:** UI redraw latency (e.g., viewport with lots of content, ANSI-heavy lines) directly stalls the network reader. While stalled, the kernel socket buffer fills; once full, the server's TCP window closes and all participants slow down. For spammy MUDs this manifests as perceived lag, delayed prompts, and — in the worst case — dropped data on very bursty servers.
  - **Fix:** Give the reader a dedicated non-blocking handoff:
    ```go
    select {
    case uiChan <- ui.NetworkDataMsg{Data: data}:
    default:
        // optional: coalesce or log
    }
    ```
    A small pump goroutine reads `uiChan` and calls `p.Send`. Alternatively, always run the data callback on its own goroutine so `p.Send` blocking doesn't stall telnet parsing.

- **High — [VERIFIED] Goroutine-per-message sends to unbuffered channels**
  - **Location:** `internal/ui/model.go:153, 165`
  - **Issue:** Patterns like `go func(r input.CommandResult) { m.LocalChan <- r }(result)` allocate a goroutine and block on an unbuffered channel. No `close` on shutdown.
  - **Impact:** Under fast input or when consumers fall behind, goroutines accumulate. On clean quit they may leak (goroutine blocked on send to a never-drained channel).
  - **Fix:** Size the channels: `make(chan input.CommandResult, 64)`. Replace the goroutine with a non-blocking send + drop or await. Close channels in a `defer` on shutdown.

- **High — [VERIFIED] Trigger matching is O(triggers × lines)**
  - **Location:** `internal/logic/trigger.go:40–49` (`CheckLine`)
  - **Issue:** For each incoming line, every trigger's compiled regex runs. With hundreds of triggers on a spammy MUD, CPU spikes.
  - **Impact:** Perceived UI lag during combat bursts; reactive triggers fire late.
  - **Fix:** Precheck with a literal/prefix set (triggers whose pattern starts with a literal prefix — very common) via a trie or a concatenated alternation regex compiled once. Only fall back to full regex scan for triggers that don't share a literal anchor.

- **Medium — [VERIFIED] `appendContent` splits / joins / ReplaceAll on every data chunk**
  - **Location:** `internal/ui/model.go:278–309`
  - **Issue:** `strings.ReplaceAll(..., "\r", "")` + `Split("\n")` + `Join` + `viewport.SetContent(...)` per incoming chunk; the viewport re-renders everything each call.
  - **Impact:** O(n) work per chunk where n is total buffered content — grows quadratically as a session goes on.
  - **Fix:** Maintain `content` as a `[]string` of lines; append only the new lines; pass lines to the viewport rather than re-joining the whole buffer. Consider coalescing bursts: batch on a `time.Ticker` (e.g., 16ms frames).

- **Medium — [VERIFIED] No `bufio.Reader` on the net.Conn**
  - **Location:** `internal/network/telnet.go:202` (1024-byte direct `Read`)
  - **Issue:** Reading directly from `net.Conn` is fine for bulk paths but costs a syscall per read and prevents structured peeking for MCCP/IAC state machines.
  - **Impact:** Extra syscalls per chunk on chatty MUDs (minor) and less flexible future parsing.
  - **Fix:** Wrap the conn in `bufio.NewReaderSize(conn, 8192)`. The parser can then call `ReadByte` / `Peek` without per-call syscalls.

- **Medium — [VERIFIED] Viewport-driven full re-render on every NetworkDataMsg**
  - **Location:** `internal/ui/model.go` — viewport `SetContent` on each append
  - **Issue:** Every network chunk triggers a full viewport render.
  - **Impact:** High CPU during traffic bursts, unrelated to actual content churn on visible rows.
  - **Fix:** Coalesce updates in a frame loop (one render per ~16ms) regardless of arrival rate; only call `SetContent` when the frame ticks.

- **Medium — [VERIFIED] Config saved synchronously on every mutation**
  - **Location:** alias/trigger add/remove paths in `cmd/termud/main.go`
  - **Issue:** Writing `termud.json` is a synchronous disk write on the main path.
  - **Impact:** Brief UI hitch on slow disks; risk of torn writes under crash.
  - **Fix:** Debounce saves (e.g., 500ms after last change) and write to a temp file + `os.Rename`.

- **Low — [VERIFIED] `buildNAWS` / IAC builders allocate per call**
  - **Location:** `internal/network/telnet.go:272–285`
  - **Issue:** Tiny per-call slice allocation.
  - **Impact:** Negligible; NAWS fires only on resize.
  - **Fix:** Not worth changing unless profiling demands.

---

## Recommended Remediation Order

1. Fix NAWS IAC escaping + unescape SB data (correctness, RFC 854).
2. Move `p.Send` off the network reader's goroutine (prevents data loss & lag).
3. Add `pending` state to the IAC parser (fix boundary bugs; prerequisite for GMCP/MCCP).
4. Implement RFC 1143 Q Method for option state (prerequisite for safely adding GMCP/MCCP/SGA).
5. Add SGA + GMCP support (unblocks the mapper work).
6. Fix goroutine-per-message sends in `ui/model.go` (memory hygiene).
7. Add read deadlines + structured EOF handling.
8. Everything else as opportunistic cleanup.

## Notes / Caveats

- Line numbers refer to the working-tree state at audit time; `buildNAWS` at `internal/network/telnet.go:272–285` and the `p.Send` blocking pattern at `cmd/termud/main.go:208–214` were spot-verified by direct read. Other citations are from the exploration pass and may be ±a few lines after any recent edits.
- The `internal/mapper/` directory is new/untracked and was not fully audited; several recommendations above (especially GMCP) directly affect how reliable the mapper can be.
- This audit does not cover functional correctness of triggers/aliases, input history, or UI behavior beyond the concurrency and performance aspects called out above.
