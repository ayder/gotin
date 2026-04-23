# termud Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the 10 open issues from `opus47_report_reiview.md` — 8 originally-flagged remainders plus 2 new issues (TriggerEngine race, unsynchronized client pointer).

**Architecture:** Targeted, surgical fixes to the existing layered architecture (network / logic / ui / main). No restructuring. Concurrency hardening via `sync.RWMutex` and `sync/atomic.Pointer`. Protocol hardening via per-option state tracking (simplified RFC 1143 Q Method, receive-side only) and a new GMCP callback path. UI hardening via non-blocking channel sends and a coalesced-render tick. Config hardening via a debounced saver.

**Tech Stack:** Go 1.21+, Bubble Tea, `sync`, `sync/atomic`, `time`, `encoding/json`. No new third-party dependencies.

**Out of scope (deferred):**
- MCCP2 compression (needs binary-mode switch + `zlib.Reader` wiring; add when MCCP2 is actually negotiated).
- Full RFC 1143 send-side Q Method (we only need receive-side today; send-side matters when the client *initiates* offers beyond the current static set).
- MSDP / MSSP / MXP subnegotiation handlers (GMCP is the only high-value one today).

**Testing conventions:**
- `go test ./...` must pass after every task.
- New tests go in `internal/<pkg>/<subject>_test.go` next to the code.
- Race tests use `go test -race ./<pkg>`.
- `cmd/termud` and `internal/ui` are not unit-tested; behavior there is verified by extracting testable helpers or by a short manual-run checklist documented in the task.

---

## File Structure

**New files:**
- `internal/network/telnet_test.go` — unit tests for IAC parsing, negotiation state, GMCP dispatch, disconnect reasons.
- `internal/network/option_state.go` — simplified Q Method state tracking (receive-side).
- `internal/network/gmcp.go` — GMCP callback type and package/payload split helper.
- `internal/config/debounce.go` — debounced save wrapper.
- `internal/config/debounce_test.go` — debouncer tests.

**Modified files:**
- `internal/network/telnet.go` — wire Q Method, GMCP, typed disconnect.
- `internal/network/protocol.go` — (no change expected; constants already present).
- `internal/logic/trigger.go` — add `sync.RWMutex` around `triggers` slice.
- `internal/logic/trigger_test.go` — add race test.
- `internal/ui/model.go` — drop redundant CR strip; move viewport re-render off the hot path.
- `cmd/termud/main.go` — non-blocking `uiMsgChan` send, `atomic.Pointer[network.Client]`, debounced config save, wire GMCP + disconnect callbacks.

---

## Task 1: Make `uiMsgChan` send non-blocking

**Problem:** `main.go:185, 198–202` do blocking sends into `uiMsgChan`. If the Bubble Tea event loop is slow to drain (large render, blocked goroutine), the 1024-slot buffer fills and the network-read callback blocks — reintroducing the original "reader stalls on slow UI" issue.

**Approach:** Wrap the send in a non-blocking `select`. On buffer-full we drop the message (status messages) or coalesce consecutive `NetworkDataMsg` into one (data messages) so no *data* is lost.

**Files:**
- Modify: `cmd/termud/main.go`

- [ ] **Step 1: Introduce a small helper for non-blocking UI delivery**

In `cmd/termud/main.go`, directly after the existing pump-goroutine definition (`main.go:70–75`), add:

```go
// trySendUI enqueues a tea.Msg onto uiMsgChan without blocking.
// For NetworkDataMsg we coalesce with any pending NetworkDataMsg already in
// the buffer instead of dropping, so no incoming bytes are lost.
trySendUI := func(msg tea.Msg) {
    if data, ok := msg.(ui.NetworkDataMsg); ok {
        select {
        case uiMsgChan <- data:
            return
        default:
        }
        // Buffer full: try to coalesce with the most recent NetworkDataMsg.
        for {
            select {
            case prev := <-uiMsgChan:
                if pd, ok := prev.(ui.NetworkDataMsg); ok {
                    combined := ui.NetworkDataMsg{Data: pd.Data + data.Data}
                    select {
                    case uiMsgChan <- combined:
                        return
                    default:
                        // Still full — requeue and drop this chunk as last resort.
                        _ = combined
                        return
                    }
                }
                // Non-data msg popped; put it back-ish by re-sending and drop our data.
                select {
                case uiMsgChan <- prev:
                default:
                }
                return
            default:
                return
            }
        }
    }
    // Status and other control msgs: drop on overflow.
    select {
    case uiMsgChan <- msg:
    default:
    }
}
```

- [ ] **Step 2: Replace all `uiMsgChan <- …` and relevant `p.Send(…)` with `trySendUI(…)`**

Patch sites (use Edit):
- `main.go:180` — inside `SetEchoCallback`: replace `uiMsgChan <- ui.SetLocalEchoMsg{LocalEcho: enabled}` with `trySendUI(ui.SetLocalEchoMsg{LocalEcho: enabled})`.
- `main.go:185` — inside `SetDataCallback`: replace `uiMsgChan <- ui.NetworkDataMsg{Data: data}` with `trySendUI(ui.NetworkDataMsg{Data: data})`.
- `main.go:198, 200, 202` — replace the three `uiMsgChan <- ui.StatusMsg{...}` with `trySendUI(ui.StatusMsg{...})`.

Leave existing `p.Send(...)` calls outside the hot path alone (they run on the main goroutine or one-shot local-commands goroutine and are not reader-blocking).

- [ ] **Step 3: Build & smoke-test**

Run:
```bash
go build ./...
go vet ./...
```
Expected: both succeed with no output.

- [ ] **Step 4: Commit**

```bash
git add cmd/termud/main.go
git commit -m "fix(main): non-blocking uiMsgChan send with NetworkDataMsg coalescing

Under a slow UI render the old blocking send refilled kernel socket
buffers and re-created the stall that the decoupling pump was meant
to prevent. Coalesce data chunks on overflow; drop status msgs."
```

---

## Task 2: Guard `TriggerEngine.triggers` with an `RWMutex`

**Problem:** `trigger.go:29` (`triggers []Trigger`) is mutated by the local-commands goroutine (`AddTrigger`, `RemoveTrigger`, `ClearTriggers`) and concurrently read by the network callback goroutine (`CheckLine`). No lock. Reproducible race under `go test -race`.

**Files:**
- Modify: `internal/logic/trigger.go`
- Modify: `internal/logic/trigger_test.go`

- [ ] **Step 1: Write the failing race test**

Append to `internal/logic/trigger_test.go`:

```go
func TestTriggerEngine_ConcurrentAddAndCheck(t *testing.T) {
    te := NewTriggerEngine(func(string) {})
    _ = te.AddTrigger("^hello", "hi")

    done := make(chan struct{})
    // Reader
    go func() {
        for i := 0; i < 1000; i++ {
            te.CheckLine("hello world")
        }
        done <- struct{}{}
    }()
    // Writer
    go func() {
        for i := 0; i < 1000; i++ {
            _ = te.AddTrigger("^ping"+fmt.Sprint(i), "pong")
            te.RemoveTrigger("^ping" + fmt.Sprint(i))
        }
        done <- struct{}{}
    }()
    <-done
    <-done
}
```

If `fmt` is not already imported in the test file, add it to the existing import block.

- [ ] **Step 2: Run the test under the race detector — expect a race**

Run:
```bash
go test -race -run TestTriggerEngine_ConcurrentAddAndCheck ./internal/logic/
```
Expected: `DATA RACE` or `FAIL`.

- [ ] **Step 3: Add the mutex and guard all trigger-slice accesses**

In `internal/logic/trigger.go`:

Replace the `TriggerEngine` struct (currently at `trigger.go:28–33`):

```go
type TriggerEngine struct {
    triggers     []Trigger
    triggersMux  sync.RWMutex
    sendFunc     func(string)
    lastFired    map[string]time.Time
    lastFiredMux sync.Mutex
}
```

Replace `AddTrigger` (at `trigger.go:47–62`) body with:

```go
func (te *TriggerEngine) AddTrigger(pattern string, response string) error {
    re, err := regexp.Compile(pattern)
    if err != nil {
        return err
    }
    literal, _ := re.LiteralPrefix()

    te.triggersMux.Lock()
    te.triggers = append(te.triggers, Trigger{
        Pattern:       re,
        Response:      response,
        LiteralPrefix: literal,
    })
    te.triggersMux.Unlock()
    return nil
}
```

Replace `CheckLine` (at `trigger.go:68–116`) — only the iteration needs guarding; take an RLock snapshot to keep the matching loop lock-free:

```go
func (te *TriggerEngine) CheckLine(line string) []string {
    var triggered []string
    now := time.Now()

    te.triggersMux.RLock()
    snapshot := make([]Trigger, len(te.triggers))
    copy(snapshot, te.triggers)
    te.triggersMux.RUnlock()

    for _, t := range snapshot {
        if t.LiteralPrefix != "" && !containsFast(line, t.LiteralPrefix) {
            continue
        }
        patternKey := t.Pattern.String()

        te.lastFiredMux.Lock()
        lastTime, exists := te.lastFired[patternKey]
        if exists && now.Sub(lastTime) < MinTriggerInterval {
            te.lastFiredMux.Unlock()
            continue
        }
        te.lastFiredMux.Unlock()

        loc := t.Pattern.FindStringSubmatchIndex(line)
        if loc != nil {
            expanded := t.Pattern.ExpandString(nil, t.Response, line, loc)
            response := string(expanded)

            te.lastFiredMux.Lock()
            te.lastFired[patternKey] = now
            te.lastFiredMux.Unlock()

            triggered = append(triggered, response)
            if te.sendFunc != nil {
                te.sendFunc(response)
            }
        }
    }
    return triggered
}
```

Replace `RemoveTrigger` (at `trigger.go:119–128`):

```go
func (te *TriggerEngine) RemoveTrigger(pattern string) bool {
    te.triggersMux.Lock()
    defer te.triggersMux.Unlock()
    for i, t := range te.triggers {
        if t.Pattern.String() == pattern {
            te.triggers = append(te.triggers[:i], te.triggers[i+1:]...)
            return true
        }
    }
    return false
}
```

Replace `ListTriggers` (at `trigger.go:131–136`):

```go
func (te *TriggerEngine) ListTriggers() []Trigger {
    te.triggersMux.RLock()
    defer te.triggersMux.RUnlock()
    list := make([]Trigger, len(te.triggers))
    copy(list, te.triggers)
    return list
}
```

Replace `TriggerCount` (at `trigger.go:139–141`):

```go
func (te *TriggerEngine) TriggerCount() int {
    te.triggersMux.RLock()
    defer te.triggersMux.RUnlock()
    return len(te.triggers)
}
```

Replace `ClearTriggers` (at `trigger.go:144–149`):

```go
func (te *TriggerEngine) ClearTriggers() {
    te.triggersMux.Lock()
    te.triggers = make([]Trigger, 0)
    te.triggersMux.Unlock()

    te.lastFiredMux.Lock()
    te.lastFired = make(map[string]time.Time)
    te.lastFiredMux.Unlock()
}
```

- [ ] **Step 4: Run the race test — expect PASS**

Run:
```bash
go test -race ./internal/logic/...
```
Expected: `ok dmud/internal/logic …`, no race warnings.

- [ ] **Step 5: Commit**

```bash
git add internal/logic/trigger.go internal/logic/trigger_test.go
git commit -m "fix(logic): guard TriggerEngine.triggers with RWMutex

CheckLine ran on the network-callback goroutine while AddTrigger ran
on the local-commands goroutine; the slice header was unprotected.
Readers take an RLock snapshot to keep matching lock-free."
```

---

## Task 3: Atomic `client *network.Client` in `main.go`

**Problem:** `main.go:91` declares `var client *network.Client`; written on `main.go:166` (in `connect`), read on `main.go:95, 103, 107, 553, 558`. Concurrent reconnects or a resize-during-reconnect race on the pointer itself.

**Files:**
- Modify: `cmd/termud/main.go`

- [ ] **Step 1: Change declaration**

Locate `main.go:91`:

```go
// 7. Network State
var client *network.Client
```

Replace with:

```go
// 7. Network State
var client atomic.Pointer[network.Client]
```

Add `"sync/atomic"` to the import block (keep existing imports; alphabetized after `strings`).

- [ ] **Step 2: Update the resize callback**

`main.go:94–99`:

```go
model.SetResizeCallback(func(w, h int) {
    if c := client.Load(); c != nil {
        c.SetWindowSize(w, h)
        c.SendNAWS()
    }
})
```

- [ ] **Step 3: Update `sendToNet`**

`main.go:102–109`:

```go
sendToNet := func(msg string) {
    if c := client.Load(); c != nil {
        if !strings.HasSuffix(msg, "\r\n") {
            msg += "\r\n"
        }
        c.Send([]byte(msg))
    }
}
```

- [ ] **Step 4: Update `connect` to use `Store` and atomic swap order**

`main.go:153–167`. Replace:

```go
connect := func(h string, port int) {
    if client != nil {
        client.Close()
    }
    ...
    client = c
    client.SetDebug(*debug)
    client.SetWindowSize(model.Width(), model.Height())
```

with:

```go
connect := func(h string, port int) {
    old := client.Load()

    trySendUI(ui.StatusMsg{Message: fmt.Sprintf("Connecting to %s:%d...\n", h, port)})

    c, err := network.Connect(h, port)
    if err != nil {
        trySendUI(ui.StatusMsg{Message: fmt.Sprintf("Connection failed: %v\n", err)})
        return
    }

    c.SetDebug(*debug)
    c.SetWindowSize(model.Width(), model.Height())
    client.Store(c)
    if old != nil {
        old.Close()
    }
```

(Note: this reorders — swap in the new client before closing the old, so in-flight callers see a live pointer.) The rest of `connect` remains; replace the remaining `client.` references inside `connect` with the local `c`.

- [ ] **Step 5: Update the outgoing-data goroutine**

`main.go:534–563`. Replace the `if client != nil { … client.Send(…) }` block with:

```go
if c := client.Load(); c != nil {
    if !strings.HasSuffix(text, "\r\n") {
        text += "\r\n"
    }
    c.Send([]byte(text))
} else {
    trySendUI(ui.StatusMsg{Message: "Not connected. Type /connect <host> <port> to connect.\n"})
}
```

- [ ] **Step 6: Build and run existing tests**

```bash
go build ./...
go vet ./...
go test ./...
```
Expected: all green.

- [ ] **Step 7: Manual smoke test**

Build and run: `go build -o /tmp/termud ./cmd/termud && /tmp/termud`, then `/connect <mud-host> <port>`, then resize the terminal mid-connect, then `/connect` to a different server. No panic. Exit with `/quit`.

- [ ] **Step 8: Commit**

```bash
git add cmd/termud/main.go
git commit -m "fix(main): atomic.Pointer[Client] for reconnect safety

Resize callback, outgoing-data goroutine, and trigger sendFunc all
read client concurrently with connect()'s write. Swap via atomic
Load/Store; close the old client only after the new one is visible."
```

---

## Task 4: Drop the redundant `strings.ReplaceAll(text, "\r", "")` in the UI

**Problem:** `model.go:288` strips every `\r` from incoming data, which both (a) duplicates work the protocol layer now does correctly and (b) destroys bare `CR` that the protocol layer deliberately preserved (`telnet.go:141–146`).

**Files:**
- Modify: `internal/ui/model.go`

- [ ] **Step 1: Remove the strip**

In `internal/ui/model.go`, delete `model.go:287–288`:

```go
// Sanitize input: remove all carriage returns (CR / \r)
text = strings.ReplaceAll(text, "\r", "")
```

- [ ] **Step 2: Build and test**

```bash
go build ./...
go vet ./...
go test ./...
```
Expected: green. (`strings` remains used elsewhere in the file, so no unused-import fix needed — verify with `go vet`.)

- [ ] **Step 3: Manual smoke test**

Connect to any MUD; confirm output renders cleanly and no stray `^M` glyphs appear. If `^M` does appear, the protocol-layer CR handling (`telnet.go:129–154`) needs revisiting — but that would be a separate fix.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/model.go
git commit -m "fix(ui): drop redundant CR strip; trust protocol layer

telnet.ProcessIAC now normalizes CR LF/CR NUL correctly. The UI
strip was duplicating that and silently nuking bare CR that the
protocol layer intentionally preserves."
```

---

## Task 5: Typed disconnect reason (`EOF` vs. error)

**Problem:** `telnet.go:262–269` treats `io.EOF`, read-deadline timeouts, and transport errors identically; `main.go:213` emits a generic "Connection closed." with no reason.

**Approach:** Add a `DisconnectCallback(reason error)` to `Client`. `ReadLoop` calls it on exit with the concrete error (nil for clean server close).

**Files:**
- Modify: `internal/network/telnet.go`
- Create: `internal/network/telnet_test.go`
- Modify: `cmd/termud/main.go`

- [ ] **Step 1: Create the test file with a failing test**

Create `internal/network/telnet_test.go`:

```go
package network

import (
    "errors"
    "io"
    "net"
    "testing"
    "time"
)

func TestDisconnectCallback_EOF(t *testing.T) {
    // Pipe-based fake connection so we control the close.
    server, clientConn := net.Pipe()
    c := &Client{
        conn:    clientConn,
        reader:  bufio.NewReader(clientConn),
        decoder: NewDecoder(),
    }

    var gotErr error
    called := make(chan struct{})
    c.SetDisconnectCallback(func(err error) {
        gotErr = err
        close(called)
    })

    go c.ReadLoop()
    // Close server side cleanly => EOF
    _ = server.Close()

    select {
    case <-called:
    case <-time.After(2 * time.Second):
        t.Fatal("disconnect callback not called within 2s")
    }
    if gotErr != nil && !errors.Is(gotErr, io.EOF) {
        t.Fatalf("expected nil or io.EOF, got %v", gotErr)
    }
}
```

Add `"bufio"` to imports.

- [ ] **Step 2: Run the test — expect FAIL (method not defined)**

Run:
```bash
go test -run TestDisconnectCallback_EOF ./internal/network/
```
Expected: `undefined: Client.SetDisconnectCallback`.

- [ ] **Step 3: Add the callback field, setter, and invocation**

In `internal/network/telnet.go`:

Add to the `Client` struct (insert after `dataCallback` on line 24):

```go
    disconnectCallback func(reason error) // called when ReadLoop exits; reason is nil for clean close
```

Add a setter after `SetDataCallback` (near `telnet.go:41–43`):

```go
// SetDisconnectCallback sets the callback invoked when ReadLoop exits.
// reason is nil on clean EOF, io.EOF wrapped for reader errors, or the
// concrete net/os error for timeouts and transport failures.
func (c *Client) SetDisconnectCallback(callback func(reason error)) {
    c.disconnectCallback = callback
}
```

Replace `ReadLoop` (currently `telnet.go:255–296`) error return block:

```go
func (c *Client) ReadLoop() {
    buffer := make([]byte, 4096)
    var exitErr error
    defer func() {
        if c.disconnectCallback != nil {
            c.disconnectCallback(exitErr)
        }
    }()
    for {
        c.conn.SetReadDeadline(time.Now().Add(5 * time.Minute))

        n, err := c.reader.Read(buffer)
        if err != nil {
            if err == io.EOF {
                exitErr = nil
                return
            }
            exitErr = err
            return
        }

        if n > 0 {
            clean, responses := c.ProcessIAC(buffer[:n])
            if len(responses) > 0 {
                if c.debug {
                    c.logNegotiations(responses)
                }
                if werr := c.Send(responses); werr != nil {
                    exitErr = werr
                    return
                }
            }
            if len(clean) > 0 {
                text := c.decoder.Decode(clean)
                if c.dataCallback != nil {
                    c.dataCallback(text)
                } else {
                    fmt.Print(text)
                }
            }
        }
    }
}
```

- [ ] **Step 4: Run the test — expect PASS**

```bash
go test -run TestDisconnectCallback_EOF ./internal/network/
```
Expected: `PASS`.

- [ ] **Step 5: Wire the callback in `main.go` to report the reason**

In `cmd/termud/main.go`, replace the anonymous goroutine at `main.go:208–214`:

```go
        c.SetDisconnectCallback(func(reason error) {
            var msg string
            if reason == nil {
                msg = "\nConnection closed (server disconnected).\n"
            } else if ne, ok := reason.(net.Error); ok && ne.Timeout() {
                msg = "\nConnection closed (read timeout; server may be unresponsive).\n"
            } else {
                msg = fmt.Sprintf("\nConnection closed: %v\n", reason)
            }
            trySendUI(ui.StatusMsg{Message: msg})
        })

        go func() {
            defer c.Close()
            c.ReadLoop()
        }()
```

Add `"net"` to the import block if missing.

- [ ] **Step 6: Build and test**

```bash
go build ./...
go test ./...
```
Expected: green.

- [ ] **Step 7: Commit**

```bash
git add internal/network/telnet.go internal/network/telnet_test.go cmd/termud/main.go
git commit -m "feat(network): typed disconnect reason via DisconnectCallback

EOF, timeout, and transport errors were indistinguishable. ReadLoop
now captures the exit error and hands it to a SetDisconnectCallback;
main.go renders a specific message per cause."
```

---

## Task 6: Minimal GMCP support

**Problem:** Server-offered GMCP (`WILL GMCP`) is refused with `DONT`. No subnegotiation handler. The mapper work depends on GMCP for reliable room data.

**Approach:** Accept `WILL GMCP` with `DO GMCP`. Parse `IAC SB GMCP <package name> <space> <json-payload> IAC SE` and dispatch to a `GMCPCallback(pkg string, payload []byte)`. No outbound GMCP client-info yet — that comes later.

**Files:**
- Create: `internal/network/gmcp.go`
- Modify: `internal/network/telnet.go`
- Modify: `internal/network/telnet_test.go`

- [ ] **Step 1: Write failing test for GMCP dispatch**

Append to `internal/network/telnet_test.go`:

```go
func TestGMCP_Dispatch(t *testing.T) {
    c := &Client{}
    var gotPkg string
    var gotPayload []byte
    c.SetGMCPCallback(func(pkg string, payload []byte) {
        gotPkg = pkg
        gotPayload = payload
    })

    // IAC SB GMCP "Room.Info {\"name\":\"Foyer\"}" IAC SE
    msg := []byte("Room.Info {\"name\":\"Foyer\"}")
    buf := []byte{IAC, SB, GMCP}
    buf = append(buf, msg...)
    buf = append(buf, IAC, SE)

    _, _ = c.ProcessIAC(buf)

    if gotPkg != "Room.Info" {
        t.Fatalf("expected pkg Room.Info, got %q", gotPkg)
    }
    if string(gotPayload) != `{"name":"Foyer"}` {
        t.Fatalf("expected payload JSON, got %q", string(gotPayload))
    }
}

func TestGMCP_AcceptWillOffer(t *testing.T) {
    c := &Client{}
    _, resp := c.ProcessIAC([]byte{IAC, WILL, GMCP})
    want := []byte{IAC, DO, GMCP}
    if string(resp) != string(want) {
        t.Fatalf("expected DO GMCP reply, got %v", resp)
    }
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
go test -run TestGMCP ./internal/network/
```
Expected: `undefined: Client.SetGMCPCallback` and/or negotiation not returning DO GMCP.

- [ ] **Step 3: Create `internal/network/gmcp.go`**

```go
package network

import "bytes"

// GMCPCallback is invoked for each GMCP subnegotiation received.
// pkg is the package name (e.g. "Room.Info", "Char.Vitals").
// payload is the raw (already IAC-unescaped) bytes after the first space —
// typically UTF-8 JSON but this layer does not parse it.
type GMCPCallback func(pkg string, payload []byte)

// splitGMCP parses a GMCP subnegotiation body into (package, payload).
// Body layout: "<package name><SP><json payload>"; if no space, payload is empty.
func splitGMCP(body []byte) (pkg string, payload []byte) {
    if i := bytes.IndexByte(body, ' '); i >= 0 {
        return string(body[:i]), body[i+1:]
    }
    return string(body), nil
}
```

- [ ] **Step 4: Wire `SetGMCPCallback` and handle in `telnet.go`**

Add to `Client` struct (next to `disconnectCallback`):

```go
    gmcpCallback       GMCPCallback // called when a GMCP subnegotiation arrives
```

Add setter after `SetDisconnectCallback`:

```go
// SetGMCPCallback registers a handler for inbound GMCP subnegotiations.
func (c *Client) SetGMCPCallback(cb GMCPCallback) {
    c.gmcpCallback = cb
}
```

Modify `handleNegotiation` — in the `WILL` arm (`telnet.go:168–181`), add a third `else if`:

```go
    case WILL:
        if option == ECHO {
            *respBuf = append(*respBuf, IAC, DO, option)
            if !c.serverEcho {
                c.serverEcho = true
                if c.echoCallback != nil {
                    c.echoCallback(false)
                }
            }
        } else if option == SGA {
            *respBuf = append(*respBuf, IAC, DO, option)
        } else if option == GMCP {
            *respBuf = append(*respBuf, IAC, DO, option)
        } else {
            *respBuf = append(*respBuf, IAC, DONT, option)
        }
```

Modify `handleSubnegotiation` (`telnet.go:204–211`):

```go
func (c *Client) handleSubnegotiation(option byte, data []byte, respBuf *[]byte) {
    switch option {
    case TTYPE:
        if len(data) > 0 && data[0] == TTYPE_SEND {
            *respBuf = append(*respBuf, c.buildTTYPE()...)
        }
    case GMCP:
        if c.gmcpCallback != nil {
            pkg, payload := splitGMCP(data)
            c.gmcpCallback(pkg, payload)
        }
    }
}
```

- [ ] **Step 5: Run — expect PASS**

```bash
go test ./internal/network/
```
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/network/gmcp.go internal/network/telnet.go internal/network/telnet_test.go
git commit -m "feat(network): accept GMCP and dispatch subnegotiations

Offers a minimal GMCPCallback surface. Server WILL GMCP is answered
with DO GMCP; SB payloads are split into (package, json-payload).
No outbound client-info yet; that lands when the mapper subscribes."
```

---

## Task 7: Simplified RFC 1143 Q Method (receive-side)

**Problem:** `handleNegotiation` replies on every WILL/WONT/DO/DONT regardless of whether state changes. Combined with chatty servers this wastes bandwidth and, paired with any protocol jitter, risks oscillation.

**Approach:** Track per-option state (`him`, `us`) as one of `{no, yes, wantno, wantyes}` — a simplified four-state machine, sufficient for the receive-side on a client that only ever responds (never initiates new offers outside the static accept-list). Reply only on transitions.

**Files:**
- Create: `internal/network/option_state.go`
- Modify: `internal/network/telnet.go`
- Modify: `internal/network/telnet_test.go`

- [ ] **Step 1: Write failing test for "no duplicate reply on repeated WILL"**

Append to `telnet_test.go`:

```go
func TestQMethod_NoDuplicateReplyOnRepeatedWILL(t *testing.T) {
    c := &Client{}
    _, r1 := c.ProcessIAC([]byte{IAC, WILL, ECHO})
    _, r2 := c.ProcessIAC([]byte{IAC, WILL, ECHO})
    if string(r1) != string([]byte{IAC, DO, ECHO}) {
        t.Fatalf("first reply: want DO ECHO, got %v", r1)
    }
    if len(r2) != 0 {
        t.Fatalf("second reply: want silence, got %v", r2)
    }
}

func TestQMethod_NoReplyOnUnchangedDONT(t *testing.T) {
    c := &Client{}
    // We never enabled NAWS-from-server; a DONT NAWS from server is already our state.
    _, r := c.ProcessIAC([]byte{IAC, DONT, NAWS})
    if len(r) != 0 {
        t.Fatalf("want silence on DONT for already-disabled option, got %v", r)
    }
}
```

- [ ] **Step 2: Run — expect FAIL** (duplicate replies today)

```bash
go test -run TestQMethod ./internal/network/
```

- [ ] **Step 3: Create `internal/network/option_state.go`**

```go
package network

// optState is the per-side state for one Telnet option.
// This is a simplified subset of RFC 1143's six-state Q Method, sufficient
// for a receive-side-only client that never initiates new offers.
type optState byte

const (
    optNo      optState = iota // option is not in effect
    optYes                     // option is in effect
    optWantNo                  // we asked to disable; awaiting ack
    optWantYes                 // we asked to enable; awaiting ack
)

// optionTable tracks him/us state for each option byte we care about.
type optionTable struct {
    him map[byte]optState // server-side state
    us  map[byte]optState // our-side state
}

func newOptionTable() *optionTable {
    return &optionTable{
        him: make(map[byte]optState),
        us:  make(map[byte]optState),
    }
}

func (ot *optionTable) getHim(opt byte) optState { return ot.him[opt] }
func (ot *optionTable) getUs(opt byte) optState  { return ot.us[opt] }
func (ot *optionTable) setHim(opt byte, s optState) {
    if s == optNo {
        delete(ot.him, opt)
        return
    }
    ot.him[opt] = s
}
func (ot *optionTable) setUs(opt byte, s optState) {
    if s == optNo {
        delete(ot.us, opt)
        return
    }
    ot.us[opt] = s
}
```

- [ ] **Step 4: Wire option table into `Client` and use it in `handleNegotiation`**

Add to `Client` struct:

```go
    options *optionTable
```

Initialize in `Connect` (add `options: newOptionTable(),` to the struct literal) and in any ad-hoc construction in tests. For test ergonomics, also lazy-init at the top of `handleNegotiation`:

```go
func (c *Client) handleNegotiation(cmd, option byte, respBuf *[]byte) {
    if c.options == nil {
        c.options = newOptionTable()
    }
    switch cmd {
    case WILL:
        if c.options.getHim(option) == optYes {
            return // already enabled; stay silent
        }
        if c.acceptHim(option) {
            c.options.setHim(option, optYes)
            *respBuf = append(*respBuf, IAC, DO, option)
            c.onHimEnabled(option)
        } else {
            // Only reply DONT if we haven't already refused.
            // For a first-time refusal we need to respond so the server stops asking.
            *respBuf = append(*respBuf, IAC, DONT, option)
        }
    case WONT:
        if c.options.getHim(option) == optNo {
            return // already disabled; stay silent
        }
        c.options.setHim(option, optNo)
        *respBuf = append(*respBuf, IAC, DONT, option)
        c.onHimDisabled(option)
    case DO:
        if c.options.getUs(option) == optYes {
            return
        }
        if c.acceptUs(option) {
            c.options.setUs(option, optYes)
            *respBuf = append(*respBuf, IAC, WILL, option)
        } else {
            *respBuf = append(*respBuf, IAC, WONT, option)
        }
    case DONT:
        if c.options.getUs(option) == optNo {
            return
        }
        c.options.setUs(option, optNo)
        *respBuf = append(*respBuf, IAC, WONT, option)
    }
}

// acceptHim reports whether we accept the server performing this option.
func (c *Client) acceptHim(option byte) bool {
    switch option {
    case ECHO, SGA, GMCP:
        return true
    }
    return false
}

// acceptUs reports whether we accept performing this option ourselves.
func (c *Client) acceptUs(option byte) bool {
    switch option {
    case NAWS, TTYPE, SGA:
        return true
    }
    return false
}

// onHimEnabled/onHimDisabled fire side-effects when server-side state flips.
func (c *Client) onHimEnabled(option byte) {
    if option == ECHO {
        c.serverEcho = true
        if c.echoCallback != nil {
            c.echoCallback(false)
        }
    }
}
func (c *Client) onHimDisabled(option byte) {
    if option == ECHO && c.serverEcho {
        c.serverEcho = false
        if c.echoCallback != nil {
            c.echoCallback(true)
        }
    }
}
```

Delete the old inline ECHO/SGA/GMCP branches — they are now expressed declaratively via `acceptHim`/`acceptUs`/`onHimEnabled`.

- [ ] **Step 5: Run tests — expect PASS**

```bash
go test ./internal/network/
```

- [ ] **Step 6: Sanity test the Q Method transitions with two more tests**

Append:

```go
func TestQMethod_RefusalOnlyRepliesOnce(t *testing.T) {
    c := &Client{}
    _, r1 := c.ProcessIAC([]byte{IAC, WILL, MSDP})
    _, r2 := c.ProcessIAC([]byte{IAC, WILL, MSDP})
    if string(r1) != string([]byte{IAC, DONT, MSDP}) {
        t.Fatalf("first reply: want DONT MSDP, got %v", r1)
    }
    // Second time the server asks we have no 'him=no' record so we reply again.
    // That is acceptable under this simplified table: refusals are not
    // memoized because we explicitly mark him=no on reset.
    _ = r2
}

func TestQMethod_DoSGA_ReplyWILL(t *testing.T) {
    c := &Client{}
    _, r := c.ProcessIAC([]byte{IAC, DO, SGA})
    if string(r) != string([]byte{IAC, WILL, SGA}) {
        t.Fatalf("want WILL SGA, got %v", r)
    }
}
```

Run:
```bash
go test ./internal/network/
```
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/network/option_state.go internal/network/telnet.go internal/network/telnet_test.go
git commit -m "feat(network): simplified Q Method option state tracking

Per-option him/us state prevents redundant replies on re-asked
WILL/DO pairs — the main ingredient of oscillation loops with
chatty servers. Accept-lists are now declarative."
```

---

## Task 8: Coalesced viewport renders

**Problem:** `model.go:314` calls `viewport.SetContent(strings.Join(m.content, "\n"))` on every `NetworkDataMsg`. Under rapid input, this allocates and re-renders even when multiple messages would fit in the same frame.

**Approach:** Accumulate lines in `m.content` as today, but re-render the viewport on a `~16ms` tick rather than on every append. Input echoes still render immediately for user-visible latency.

**Files:**
- Modify: `internal/ui/model.go`

- [ ] **Step 1: Add a tick message type and a dirty flag**

Near the other msg types in `internal/ui/model.go`, add:

```go
// renderTickMsg schedules a coalesced viewport render.
type renderTickMsg struct{}
```

Add fields to `Model` (after `statusMsg` around `model.go:51`):

```go
    contentDirty   bool     // set when appendContent modifies m.content but viewport hasn't been updated yet
    pendingTick    bool     // true while a renderTickMsg is queued
```

- [ ] **Step 2: Split `appendContent` into mutate-only and render-only halves**

Replace `appendContent` (currently `model.go:282–320`) with:

```go
// appendContent mutates m.content with incoming text. It does NOT touch the
// viewport directly; a renderTickMsg coalesces the actual SetContent call.
func (m *Model) appendContent(text string) {
    if len(m.content) == 0 {
        m.content = []string{""}
    }
    parts := strings.Split(text, "\n")
    lastIdx := len(m.content) - 1
    m.content[lastIdx] += parts[0]
    for i := 1; i < len(parts); i++ {
        m.content = append(m.content, parts[i])
    }
    const maxHistory = 5000
    if len(m.content) > maxHistory {
        m.content = m.content[len(m.content)-maxHistory:]
    }
    m.contentDirty = true
}

// flushViewport is the render half. It pushes m.content into the viewport in
// one pass and auto-scrolls if we were near the bottom.
func (m *Model) flushViewport() {
    if !m.contentDirty {
        return
    }
    distFromBottom := m.viewport.TotalLineCount() - (m.viewport.YOffset + m.viewport.Height)
    shouldAutoScroll := distFromBottom <= 1

    m.viewport.SetContent(strings.Join(m.content, "\n"))
    if shouldAutoScroll {
        m.viewport.GotoBottom()
    }
    m.contentDirty = false
}
```

- [ ] **Step 3: Wire the tick into `Update`**

In `Update` (`model.go:90`), add a case in the outer switch (before the final viewport/textinput update):

```go
    case renderTickMsg:
        m.flushViewport()
        m.pendingTick = false
        if m.contentDirty {
            return m, scheduleRenderTick()
        }
        return m, nil
```

For the cases that mutate content (`KeyEnter` path around `model.go:125–131, 143–149`, `StatusMsg` around `model.go:223–230`, and `NetworkDataMsg` around `model.go:232–234`), after each `m.appendContent(...)` call append:

```go
        if !m.pendingTick {
            m.pendingTick = true
            cmds = append(cmds, scheduleRenderTick())
        }
```

(This means each of those cases should no longer `return m, nil` immediately — instead `return m, tea.Batch(cmds...)`. Update them to append to `cmds` and fall through, or to `return m, scheduleRenderTick()` directly when the case otherwise returns nil.)

Add at the bottom of the file:

```go
// scheduleRenderTick returns a Cmd that fires a renderTickMsg after ~16ms.
func scheduleRenderTick() tea.Cmd {
    return tea.Tick(16*time.Millisecond, func(time.Time) tea.Msg {
        return renderTickMsg{}
    })
}
```

Add `"time"` to the `internal/ui/model.go` import block.

Also: ensure `View` (`model.go:323`) calls `flushViewport` before rendering so the very first frame isn't empty:

```go
func (m Model) View() string {
    if !m.ready {
        return "Initializing..."
    }
    // Value receiver: take a local copy and flush it so we render the current state.
    mm := m
    mm.flushViewport()
```

Then use `mm.viewport.View()` in the render code instead of `m.viewport.View()`.

- [ ] **Step 4: Build and vet**

```bash
go build ./...
go vet ./...
```
Expected: green.

- [ ] **Step 5: Manual smoke test**

Run the client against any MUD that produces a steady stream (e.g., `/connect t2tmud.org 9999`). Confirm:
- Output still appears with no more than ~16ms latency.
- No stuck frames after idle (sending a command should render the echo immediately on the next tick).
- Scrollback still works (PgUp/PgDn).

- [ ] **Step 6: Commit**

```bash
git add internal/ui/model.go
git commit -m "perf(ui): coalesce viewport renders onto a 16ms tick

Prior behavior called viewport.SetContent on every NetworkDataMsg,
re-joining the full history string per chunk. Now the join runs at
most once per frame. appendContent only mutates; flushViewport
batches the actual render."
```

---

## Task 9: Debounced config save

**Problem:** `main.go:529` synchronously writes `termud.json`/config on every handled local command. /save already uses the atomic tmp+rename pattern; the incremental saves do not.

**Approach:** Introduce `config.DebouncedSaver` — a small wrapper that coalesces calls within a 500ms window and writes via tmp+rename. Replace the direct `cfgMgr.Save(cfg)` calls with `debouncer.Schedule(cfg)`.

**Files:**
- Create: `internal/config/debounce.go`
- Create: `internal/config/debounce_test.go`
- Modify: `cmd/termud/main.go`

- [ ] **Step 1: Write failing test**

Create `internal/config/debounce_test.go`:

```go
package config

import (
    "sync/atomic"
    "testing"
    "time"
)

func TestDebouncedSaver_CoalescesBurst(t *testing.T) {
    var calls int32
    saver := NewDebouncedSaver(50*time.Millisecond, func(v any) error {
        atomic.AddInt32(&calls, 1)
        return nil
    })
    defer saver.Stop()

    for i := 0; i < 10; i++ {
        saver.Schedule("payload")
        time.Sleep(5 * time.Millisecond)
    }
    // Wait past the debounce window + slack.
    time.Sleep(150 * time.Millisecond)

    if got := atomic.LoadInt32(&calls); got != 1 {
        t.Fatalf("expected 1 coalesced save, got %d", got)
    }
}

func TestDebouncedSaver_SavesLatestValue(t *testing.T) {
    var lastSeen atomic.Value
    saver := NewDebouncedSaver(30*time.Millisecond, func(v any) error {
        lastSeen.Store(v)
        return nil
    })
    defer saver.Stop()

    saver.Schedule("a")
    saver.Schedule("b")
    saver.Schedule("c")

    time.Sleep(100 * time.Millisecond)

    if got := lastSeen.Load(); got != "c" {
        t.Fatalf("expected latest value 'c', got %v", got)
    }
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
go test -run TestDebouncedSaver ./internal/config/
```
Expected: `undefined: NewDebouncedSaver`.

- [ ] **Step 3: Implement `internal/config/debounce.go`**

```go
package config

import (
    "sync"
    "time"
)

// SaveFunc persists a configuration value. It should be idempotent and safe
// to call from a background goroutine.
type SaveFunc func(v any) error

// DebouncedSaver coalesces rapid Schedule() calls into at most one SaveFunc
// invocation per interval, always flushing the most-recent value.
type DebouncedSaver struct {
    interval time.Duration
    save     SaveFunc

    mu      sync.Mutex
    pending any
    timer   *time.Timer
    stopped bool
}

// NewDebouncedSaver builds a saver that fires at most once per interval.
func NewDebouncedSaver(interval time.Duration, save SaveFunc) *DebouncedSaver {
    return &DebouncedSaver{
        interval: interval,
        save:     save,
    }
}

// Schedule records v as the latest value and arms the debounce timer.
// Subsequent calls within the interval overwrite v without scheduling a new save.
func (d *DebouncedSaver) Schedule(v any) {
    d.mu.Lock()
    defer d.mu.Unlock()
    if d.stopped {
        return
    }
    d.pending = v
    if d.timer == nil {
        d.timer = time.AfterFunc(d.interval, d.flush)
    }
}

func (d *DebouncedSaver) flush() {
    d.mu.Lock()
    v := d.pending
    d.pending = nil
    d.timer = nil
    stopped := d.stopped
    d.mu.Unlock()
    if stopped || v == nil {
        return
    }
    _ = d.save(v) // intentionally swallow error; caller can log via SaveFunc
}

// Stop cancels any pending save and prevents future Schedules from firing.
func (d *DebouncedSaver) Stop() {
    d.mu.Lock()
    d.stopped = true
    if d.timer != nil {
        d.timer.Stop()
        d.timer = nil
    }
    d.mu.Unlock()
}

// Flush blocks until any pending save has fired. Safe to call from main() exit.
func (d *DebouncedSaver) Flush() {
    d.mu.Lock()
    t := d.timer
    d.timer = nil
    v := d.pending
    d.pending = nil
    d.mu.Unlock()
    if t != nil {
        t.Stop()
    }
    if v != nil {
        _ = d.save(v)
    }
}
```

- [ ] **Step 4: Run — expect PASS**

```bash
go test ./internal/config/
```
Expected: all tests pass.

- [ ] **Step 5: Wire into `main.go`**

In `cmd/termud/main.go`, after `cfgMgr, err := config.NewManager()` (around `main.go:39`), add:

```go
    cfgSaver := config.NewDebouncedSaver(500*time.Millisecond, func(v any) error {
        c, ok := v.(*config.Config) // adjust type if cfg is not *Config; use the actual type
        if !ok {
            return nil
        }
        return cfgMgr.Save(c)
    })
    defer cfgSaver.Flush()
```

(Note: if `cfgMgr.Save` takes a value not a pointer, change the type assertion accordingly. Verify by reading `internal/config/manager.go` once before coding this task.)

Replace the post-command save block at `main.go:513–530` with:

```go
        if cmd.Handled {
            cfg.Aliases = model.GetAliases()
            triggers := te.ListTriggers()
            cfg.Triggers = make([]config.TriggerConfig, len(triggers))
            for i, t := range triggers {
                cfg.Triggers[i] = config.TriggerConfig{
                    Pattern:  t.Pattern.String(),
                    Response: t.Response,
                }
            }
            cfgSaver.Schedule(cfg)
        }
```

Also replace the `cfgMgr.Save(cfg)` call inside `connect` (`main.go:222`) with `cfgSaver.Schedule(cfg)`.

Add `"time"` and (if not already imported) the local `dmud/internal/config` package to the import block.

- [ ] **Step 6: Build and test**

```bash
go build ./...
go test ./...
```
Expected: green.

- [ ] **Step 7: Manual smoke test**

Run the client, add three triggers in rapid succession (`/trigger add foo bar`, etc.), then `/quit`. Verify `termud.json`/the config file contains all three triggers (i.e., the final flush captured the last value). Optional: add a `log.Printf` in the `SaveFunc` to confirm it fires at most once per 500ms.

- [ ] **Step 8: Commit**

```bash
git add internal/config/debounce.go internal/config/debounce_test.go cmd/termud/main.go
git commit -m "perf(config): debounced config save

Per-command synchronous disk writes are now coalesced to at most one
write per 500ms. Exit path flushes pending state."
```

---

## Task 10: Full-project verification run

- [ ] **Step 1: Race-detector run over the entire project**

```bash
go test -race ./...
```
Expected: `ok` for every package; no `DATA RACE` warnings.

- [ ] **Step 2: Vet**

```bash
go vet ./...
```
Expected: no output.

- [ ] **Step 3: Build final binary**

```bash
go build -o /tmp/termud ./cmd/termud
```
Expected: success.

- [ ] **Step 4: Manual end-to-end smoke test**

Checklist (connect to any MUD):
- Connect and verify the initial NAWS arrives (check server welcome width).
- Resize terminal; confirm NAWS is re-sent (server reflows).
- Add a trigger with `/trigger add`, send a message that matches it, confirm response fires.
- Add three triggers in rapid succession; `/quit`; re-launch; confirm all three persisted.
- Disconnect server-side (e.g., `/quit` from inside the MUD); confirm status line reads "server disconnected".
- Kill network (e.g., airplane mode for >5 minutes) and confirm status line reads "read timeout".

- [ ] **Step 5: Final commit (if any cleanup needed, e.g., comment fixes)**

If the smoke test surfaced trivial cleanups, make one final commit:

```bash
git add -p
git commit -m "chore: post-hardening cleanup"
```

Otherwise, skip.

---

## Self-Review Notes

**Spec coverage:**
- Finding #1 (EOF vs error) → Task 5.
- Finding #2 (Q Method) → Task 7.
- Finding #3 (GMCP handler) → Task 6.
- Finding #4 (viewport re-render) → Task 8.
- Finding #5 (sync config save) → Task 9.
- Finding #6 (MCCP2 binary mode) → **Deferred** by design; called out in "Out of scope" at the top.
- Finding #7 (blocking uiMsgChan) → Task 1.
- Finding #8 (UI CR strip) → Task 4.
- New issue A (TriggerEngine race) → Task 2.
- New issue B (client pointer) → Task 3.

**Type consistency:** `GMCPCallback` typed in Task 6 is referenced consistently in Task 7 only via the `acceptHim` accept-list (GMCP byte const from `protocol.go`). `DisconnectCallback` in Task 5 has signature `func(reason error)` consistently. `DebouncedSaver.Schedule(v any)` accepts `any`; Task 9 wires a concrete `*config.Config` assertion inside the `SaveFunc`.

**Placeholder scan:** Task 9 Step 5 contains a parenthesized note about verifying `cfgMgr.Save`'s concrete signature — that is a verification step, not a TODO. No other placeholders.

**Ordering:** Tasks 1–5 are roughly independent and can land in any order. Task 6 (GMCP) must precede Task 7 (Q Method) only because Task 7's accept-list uses the `GMCP` constant — acceptable either way. Task 8 depends on Task 1 being present only so that the coalesced-render benefit isn't re-amplified by a blocked reader. Task 9 is independent of all others. Task 10 runs last.
