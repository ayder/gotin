# Review of opus47.md Findings and Applied Fixes

_Reviewer: Claude Opus 4.7 (1M context). Date: 2026-04-23._
_Basis: Independent re-read of the sources cited in `opus47.md` against the current working tree._

## Summary

Of the **25 findings** in `opus47.md`:
- **18 are genuinely fixed** in the working tree. The fixes are, for the most part, correct and on-the-mark.
- **1 was mis-identified** by the original audit (`[DISPUTED]` tag is correct — no real race).
- **6 are still open** to varying degrees; 2 of those are misrepresented in the file as `[VERIFIED]` (meaning the finding is real) but no fix has been applied.
- **2 new issues** surfaced during this review that were not in the original audit.

## What is actually fixed (verified by re-reading the code)

| # | Finding | Where fix lives | Correct? |
|---|---|---|---|
| 1 | NAWS IAC-escape | `telnet.go:357–362` (`buildNAWS` now routes payload through `escapeIAC`) and `telnet.go:228–237` (`escapeIAC` helper) | ✅ Correct. Width/height bytes of `0xFF` are now doubled per RFC 854. |
| 2 | Subnegotiation un-escape | `telnet.go:213–225` (`unescapeSubneg`) called at `telnet.go:111` | ✅ Correct. `IAC IAC` → `IAC` on extraction. |
| 3 | IAC write error handled | `telnet.go:280–283` (negotiation `Send` checks `err` and returns) | ✅ Correct. Tight negotiation loop on a dead socket is no longer possible. |
| 4 | IAC boundary `pending` state | `telnet.go:27` (field), `telnet.go:57–60` (prepend), `telnet.go:76, 93, 115, 119, 149` (save tail) | ✅ Correct. Covers `IAC`-alone, `IAC CMD`, SB-mid, and `CR`-alone boundaries. |
| 5 | Initial NAWS on connect | `main.go:217` (`client.SendNAWS()` after `Connected!`) plus `client.SetWindowSize(...)` on `main.go:168` | ✅ Correct. First NAWS now arrives without requiring a resize. |
| 6 | `Close()` nil guard | `telnet.go:373–377` | ✅ Correct. Double-close during a failed reconnect no longer panics. |
| 7 | Read deadline | `telnet.go:259` (5-minute rolling deadline refreshed each iteration) | ✅ Correct in spirit; see caveats below. |
| 8 | `bufio.Reader` on conn | `telnet.go:248`, reads through `c.reader` at `telnet.go:261` | ✅ Correct. Reduces syscall churn and prepares for MCCP2. |
| 9 | CR NUL / CR LF normalization | `telnet.go:129–154` | ✅ Correct at protocol layer. The UI layer still strips `\r` redundantly (see open items). |
| 10 | SGA acceptance | `telnet.go:177–178, 193–194` (WILL SGA → DO SGA; DO SGA → WILL SGA) | ✅ Correct. |
| 11 | UTF-8 leftover buffer | `decoder.go:12, 27–29, 41–45, 63–92` | ✅ Correct. `findIncompleteUTF8` detects 2/3/4-byte starts and stashes the tail until the next `Decode`. |
| 12 | MUD-option constants added | `protocol.go:19–29` | ✅ Correct as constants. But **see open item #6** — only SGA/NAWS/ECHO actually get wired up in `handleNegotiation`, so GMCP/MSDP/etc. are still refused. |
| 13 | Port validation | `main.go:236–239` | ✅ Correct. Checks both `err != nil` and `port <= 0 || port > 65535`. |
| 14 | Decoupled UI delivery pump | `main.go:70–75` (`uiMsgChan` + pump goroutine) with callbacks at `main.go:180, 185, 198–203` | ✅ Correct. Network callback no longer blocks on `p.Send`. Buffer size 1024 is sensible. See caveat (#7 below). |
| 15 | Goroutine-per-message → non-blocking buffered send | `model.go:151–158, 167–172`; channels sized at `main.go:51–53` (`chan …, 128`) | ✅ Correct. |
| 16 | Atomic config save (`/save`) | `main.go:292–303` (write to `.tmp`, then `os.Rename`) | ✅ Correct. |
| 17 | `json.Unmarshal` error logged on auto-load | `main.go:146–148` | ✅ Correct. |
| 18 | `json.Unmarshal` error surfaced on `/load` | `main.go:317–319` | ✅ Correct. |

## What was mis-identified by the original audit

- **`[DISPUTED]` Data race on `Model.content`** — `internal/ui/model.go`.
  The original audit flagged a race between `appendContent` and `Update`. On re-reading: `Update` has a value receiver (`func (m Model) Update(...)`, `model.go:90`) and `appendContent` is only called from inside `Update`'s switch (`model.go:127, 129, 146, 226, 233`). Bubble Tea's event loop is single-goroutine and serializes `Update` calls, so mutations flow back through the returned `Model` without shared state. The `[DISPUTED]` tag is correct. **No change needed.**

## What is still open

### Still open — already correctly flagged as `[VERIFIED]`

1. **EOF vs. error not distinguished in `ReadLoop`** — `telnet.go:262–269`.
   Both branches `return` with no logging, no typed message to the UI. The only user feedback is the generic `"\nConnection closed.\n"` emitted at `main.go:213`. Read-deadline timeouts and real network errors look identical to a clean server disconnect.
   **Suggested fix:** plumb a reason string or error type through a new callback (or a status channel) so `main.go:213` can say *why* the connection ended.

2. **RFC 1143 Q Method still missing** — `telnet.go:166–201` (`handleNegotiation`).
   `serverEcho` state is tracked only for ECHO and only guards the *echo callback*; the protocol reply is still written on every incoming WILL/DO, regardless of whether the state changed. A server that re-sends `WILL ECHO` during keepalive still gets a fresh `DO ECHO` response. Combined with several servers' own naive re-negotiation, this wastes bytes and, in pathological cases, still risks oscillation. Not a blocker but below RFC 855 best practice.
   **Suggested fix:** add `us, him` state per option (map keyed by option byte) and the six-state machine from RFC 1143; reply only on transitions.

3. **No GMCP/MSDP/MSSP handler** — `protocol.go` has the constants; `telnet.go:167–180` (`WILL` branch) only returns `DO` for `ECHO` and `SGA`. Everything else gets `DONT`, so GMCP is rejected at the door. The executive-summary remediation item "Add SGA + GMCP support (unblocks the mapper work)" is only half done: SGA yes, GMCP no.
   **Suggested fix:** in the `WILL` arm, accept `GMCP` and `MSDP` by replying `DO`. In `handleSubnegotiation` (`telnet.go:204–211`), add a `GMCP` case that emits a typed event (e.g., `gmcpCallback(pkg string, payload []byte)`) so the mapper can consume it.

4. **Viewport full re-render on every `appendContent`** — `model.go:314` still does `viewport.SetContent(strings.Join(m.content, "\n"))` per chunk. Partly mitigated by `maxHistory = 5000` cap (`model.go:306–310`) so cost is bounded, but still allocates a fresh joined string on every network message.
   **Suggested fix:** coalesce via `time.Ticker` (one render per frame ~16ms) in either the pump goroutine or a dedicated render ticker; hand the viewport lines rather than a joined string.

5. **Synchronous `cfgMgr.Save(cfg)` on every handled local command** — `main.go:513–530`.
   Writes to disk on the hot path of every alias/trigger change. Atomic `/save` was fixed, but this path is still direct-write.
   **Suggested fix:** debounce (e.g., 500 ms after last change) and route through the same tmp-file+rename pattern as `/save`.

6. **No MCCP2 binary-mode switch** — `telnet.go` still has no mode flag.
   Unchanged since the audit; not a bug today because MCCP2 isn't negotiated, but this needs to exist before GMCP+MCCP can be safely co-enabled.

### Still open — flagged `[VERIFIED]` in the file but **no fix applied**

7. **`uiMsgChan` is drop-free, so it can still block the reader** — `main.go:70–75`.
   The decoupling is partial: the network callback does `uiMsgChan <- msg` (`main.go:185, 198, 200, 202`), which is a *blocking* send. The channel is buffered to 1024, which is generous, but if the pump goroutine is stalled (e.g., the Bubble Tea program is blocked on a slow render), the buffer fills and the reader once again blocks — just with more slack than before. This partially addresses the original **critical** finding but does not eliminate it.
   **Suggested fix:** change to a non-blocking `select { case uiMsgChan <- msg: default: /* drop or aggregate */ }`, or coalesce consecutive `NetworkDataMsg` into a single batched message when the consumer is slow.

8. **Stray `CR` still stripped at the UI layer** — `model.go:288` (`strings.ReplaceAll(text, "\r", "")`).
   Now redundant with `telnet.go`'s CR NUL/CR LF handling, and actively harms the "keep stray CR for robustness" branch (`telnet.go:141–146`). If the protocol layer deliberately preserves a bare `CR` (e.g., ASCII art), the UI still nukes it.
   **Suggested fix:** drop the `ReplaceAll` from `appendContent` and trust the protocol layer.

## New issues surfaced during this review

### A. Data race on `TriggerEngine.triggers` slice

- **Severity:** High
- **Location:** `internal/logic/trigger.go:29` (`triggers []Trigger`, unguarded) vs. `trigger.go:47–62` (`AddTrigger`), `trigger.go:119–128` (`RemoveTrigger`), `trigger.go:144–149` (`ClearTriggers`), `trigger.go:68–116` (`CheckLine`).
- **Issue:** The `triggers` slice is written from the local-commands goroutine (`main.go:226` range over `localChan`) via `te.AddTrigger`, `te.RemoveTrigger`, `te.ClearTriggers`. It is read from the network-callback goroutine (`main.go:183` → `proc.ProcessLine(line)` → `te.CheckLine`). No mutex guards the slice.
- **Impact:** Under `-race`, flagged. In production, a `/trigger add` during a burst of incoming text can corrupt the slice header (length/cap mismatch) causing either a missed match or, rarely, an out-of-bounds panic.
- **Fix:** Add an `sync.RWMutex` around `triggers`. `AddTrigger`/`RemoveTrigger`/`ClearTriggers` take the write lock; `CheckLine`/`ListTriggers`/`TriggerCount` take the read lock. Note that `lastFiredMux` already exists for the cooldown map — extending protection to `triggers` is a natural addition.

### B. Unsynchronized `client` pointer across goroutines

- **Severity:** Medium
- **Location:** `cmd/termud/main.go:91` (`var client *network.Client`), written at `main.go:166` (`client = c`), read on `main.go:95, 103, 107, 553, 558`.
- **Issue:** `client` is assigned by the local-commands goroutine inside `connect` and read by (at least) the send goroutine (`main.go:553, 558`), the resize callback (`main.go:95`), and `sendToNet` (`main.go:103, 107`, which itself is plumbed through the trigger engine and thus called from the network-callback goroutine). There is no mutex or atomic.
- **Impact:** On reconnect (`connect` replaces `client`), a concurrent `client.Send(...)` may race with the pointer write. Unlikely to panic because both sides see valid pointers, but the send can target the old, just-closed client and fail silently (at best), or race against a nil intermediate if compiler/runtime permits (at worst).
- **Fix:** Wrap access through an `atomic.Pointer[network.Client]` or a small mutex. Also, closing the previous client (`main.go:154–156`) should happen after atomically swapping so in-flight sends can see the new client first.

## Remediation priority (post-review)

1. Make the `uiMsgChan` send non-blocking or introduce a coalescing path. The rest of the decoupling work is wasted if one slow frame can still stall the reader (#7 above).
2. Guard `TriggerEngine.triggers` with a mutex (new issue A).
3. Guard `client` with `atomic.Pointer` (new issue B).
4. Add real GMCP handling (#3 above) — mapper work cited in the original audit depends on this.
5. Implement RFC 1143 Q Method (#2 above) before adding any more options.
6. EOF vs. error discrimination in `ReadLoop` (#1 above).
7. Opportunistic cleanup: remove the redundant UI-layer CR strip, debounce `cfgMgr.Save`, coalesce viewport renders.

## Notes and caveats

- Findings #4 (pending state), #1 (NAWS escape), #2 (SB unescape), #9 (CR handling), #14 (UI pump), and new issues A/B were spot-verified by direct file reads (`telnet.go:1–378`, `model.go:1–466`, `main.go:1–633`, `decoder.go:1–111`, `trigger.go:1–150`, `protocol.go:1–38`).
- The audit of `internal/mapper/` was out of scope then and remains out of scope here; issue B's impact on the mapper was inferred only from how `client` is used in `main.go`.
- No runtime testing or `go test -race` run was performed; the race claims above are by inspection. A `-race` harness on a test that exercises `/trigger add` during a synthetic network read burst would confirm issue A cheaply.
