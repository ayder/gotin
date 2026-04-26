# Mapper Resilience — Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the broken text-only auto-mapper path with a structural one keyed off MXP tags. Hard-code a t2tmud profile (overridable via `gotin.json`); strictly require MXP to be active for any auto-mapping; produce one cleaned `RoomBlock` per `<expire>`-to-prompt cycle and feed it through a new v2 structural hash.

**Architecture:** New `mapper.MUDProfile` + `mapper.RoomBlockBuffer` accumulate MXP tag callbacks and stripped text between `<expire>` and the prompt line, then emit a `RoomBlock{Description, Exits, Presence}`. New `Engine.HandleRoomBlock` becomes the single auto-mapping ingestion point; `ProcessRoomData` and `HandleGMCPRoomInfo` are no longer called from production code paths. App.go gates auto-mapping on MXP-active + `<expire>` observed.

**Tech Stack:** Go 1.21+, stdlib only (`regexp`, `strings`, `crypto/sha256`, `encoding/json`). Builds on Phase 1's `protolog` + `MappingOptions`.

**Spec:** `docs/superpowers/specs/2026-04-26-mapper-resilience-phase2-design.md`
**MXP corpus reference:** `docs/t2tmud_mxp.md`
**Branch:** `mapper-resilience` (continues from Phase 1; assume Phase 1 changes are present in the working tree).

---

## File Structure

**Create**
- `internal/mapper/profile.go` — `MUDProfile`, `DefaultT2TMUDProfile`, `MergeProfile`, compiled-regex helpers.
- `internal/mapper/profile_test.go`
- `internal/mapper/blockbuf.go` — `RoomBlock`, `RoomBlockBuffer`, ANSI stripper, block parser.
- `internal/mapper/blockbuf_test.go`
- `internal/mapper/testdata/t2tmud_paved_street.txt` — fixture (one room, NPC).
- `internal/mapper/testdata/t2tmud_plains_north_ne.txt` — plains tile 1.
- `internal/mapper/testdata/t2tmud_plains_nw_n.txt` — plains tile 2.
- `internal/mapper/testdata/t2tmud_plains_nw.txt` — plains tile 3.
- `internal/mapper/testdata/t2tmud_plains_landlocked.txt` — plains tile 4.
- `internal/mapper/testdata/t2tmud_plains_white_towers_se.txt` — plains tile 5.
- `internal/mapper/testdata/t2tmud_white_towers_room.txt` — distinct room with sight.
- `internal/mapper/integration_test.go` — fixture-replay end-to-end test.

**Modify**
- `internal/mudproto/mxp/mxp.go` — `SetTagCallback`, `onTag` field, fire from `Filter`.
- `internal/mudproto/mxp/mxp_test.go` — tag callback unit tests.
- `internal/mapper/mapper.go` — `ComputeStructuralHash`, `normaliseDescription`, `structuralIndex`, `MUDProfile`/`sawBlockStart` fields, `SetMUDProfile`, `MarkBlockStartSeen`, `HasBlockStartSeen`, `ResetBlockStart`, `HandleRoomBlock`.
- `internal/mapper/mapper_test.go` — tests for hash, sentinel, HandleRoomBlock.
- `internal/app/app.go` — profile resolution, RoomBlockBuffer wiring, retire `ProcessRoomData`/`HandleGMCPRoomInfo` from production calls, MXP-active gate on `ProcessMovement`, `/map start` warning, `mud_profiles` JSON persistence.
- `gotin.json` — add example `mud_profiles.t2tmud.org` block.

**Checkpoints**
- **A.** `MUDProfile` + JSON merge (Tasks 1–3)
- **B.** MXP tag callback (Tasks 4–5)
- **C.** `RoomBlockBuffer` + parser (Tasks 6–10)
- **D.** Structural hash + Engine integration (Tasks 11–14)
- **E.** Strict gate + `/map start` warning (Tasks 15–16)
- **F.** App wiring + JSON persistence (Tasks 17–20)
- **G.** Fixtures + integration test + manual verification (Tasks 21–23)

Commit after each task. Pause for review at the end of each checkpoint.

---

## Checkpoint A — `MUDProfile` and JSON merge

### Task 1: `MUDProfile` struct + `DefaultT2TMUDProfile`

**Files:**
- Create: `internal/mapper/profile.go`
- Create: `internal/mapper/profile_test.go`

- [ ] **Step 1: Write the failing test**

`internal/mapper/profile_test.go`:

```go
package mapper

import (
	"reflect"
	"testing"
)

func TestDefaultT2TMUDProfile(t *testing.T) {
	p := DefaultT2TMUDProfile()
	if p.Host != "t2tmud.org" {
		t.Errorf("Host = %q, want t2tmud.org", p.Host)
	}
	if p.BlockStartTag != "expire" {
		t.Errorf("BlockStartTag = %q, want expire", p.BlockStartTag)
	}
	wantExitTags := []string{"x", "xx", "t", "w", "ww", "l", "ll", "y", "yy", "z"}
	if !reflect.DeepEqual(p.ExitTags, wantExitTags) {
		t.Errorf("ExitTags = %v, want %v", p.ExitTags, wantExitTags)
	}
	wantPresence := []string{"i30", "i9"}
	if !reflect.DeepEqual(p.PresenceTags, wantPresence) {
		t.Errorf("PresenceTags = %v, want %v", p.PresenceTags, wantPresence)
	}
	if len(p.WeatherPatterns) < 2 {
		t.Errorf("WeatherPatterns: want at least 2, got %d", len(p.WeatherPatterns))
	}
	if p.DoorStatePattern == "" {
		t.Errorf("DoorStatePattern is empty")
	}
	if p.BlockEndPattern == "" {
		t.Errorf("BlockEndPattern is empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mapper/ -run TestDefaultT2TMUDProfile -v`
Expected: FAIL — `undefined: DefaultT2TMUDProfile`, `undefined: MUDProfile`.

- [ ] **Step 3: Implement `MUDProfile` and `DefaultT2TMUDProfile`**

Create `internal/mapper/profile.go`:

```go
package mapper

// MUDProfile drives the structural block parser. Defaults are tuned for
// t2tmud.org (see docs/t2tmud_mxp.md). Override per-host via gotin.json
// mud_profiles.<host>; absent fields fall back to the in-code default.
type MUDProfile struct {
	Host             string   `json:"host"`
	BlockStartTag    string   `json:"block_start_tag"`
	BlockEndPattern  string   `json:"block_end_pattern"`
	ExitTags         []string `json:"exit_tags"`
	PresenceTags     []string `json:"presence_tags"`
	WeatherPatterns  []string `json:"weather_patterns"`
	DoorStatePattern string   `json:"door_state_pattern"`
}

// DefaultT2TMUDProfile is the in-code default profile.
func DefaultT2TMUDProfile() MUDProfile {
	return MUDProfile{
		Host:          "t2tmud.org",
		BlockStartTag: "expire",
		// ANSI-tolerant prompt: [WA] or [\x1b[1;32mWA\x1b[0m] both match.
		BlockEndPattern: `\nHP:\d+ EP:\d+ \[(?:\x1b\[[0-9;]*[A-Za-z])*\w+(?:\x1b\[[0-9;]*[A-Za-z])*\] > `,
		ExitTags:        []string{"x", "xx", "t", "w", "ww", "l", "ll", "y", "yy", "z"},
		PresenceTags:    []string{"i30", "i9"},
		WeatherPatterns: []string{
			`^The sky is .+\.$`,
			`^A .+ sky .+\.$`,
		},
		DoorStatePattern: `^The .+ (door|gate|portcullis) is (open|closed|locked|broken|sealed|barred)\.$`,
	}
}
```

- [ ] **Step 4: Run test**

Run: `go test ./internal/mapper/ -run TestDefaultT2TMUDProfile -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mapper/profile.go internal/mapper/profile_test.go
git commit -m "feat(mapper): MUDProfile struct and t2tmud default"
```

---

### Task 2: `MergeProfile` per-field merge

**Files:**
- Modify: `internal/mapper/profile.go`
- Modify: `internal/mapper/profile_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/mapper/profile_test.go`:

```go
func TestMergeProfile_EmptyOverrideReturnsBase(t *testing.T) {
	base := DefaultT2TMUDProfile()
	merged := MergeProfile(base, MUDProfile{})
	if !reflect.DeepEqual(merged, base) {
		t.Errorf("empty override mutated base: got %+v", merged)
	}
}

func TestMergeProfile_PartialOverride(t *testing.T) {
	base := DefaultT2TMUDProfile()
	override := MUDProfile{
		BlockStartTag: "myroom",
		ExitTags:      []string{"go"},
	}
	merged := MergeProfile(base, override)
	if merged.BlockStartTag != "myroom" {
		t.Errorf("BlockStartTag not overridden: %q", merged.BlockStartTag)
	}
	if !reflect.DeepEqual(merged.ExitTags, []string{"go"}) {
		t.Errorf("ExitTags not overridden: %v", merged.ExitTags)
	}
	// Untouched fields stay at default.
	if merged.BlockEndPattern != base.BlockEndPattern {
		t.Errorf("BlockEndPattern leaked: %q", merged.BlockEndPattern)
	}
	if !reflect.DeepEqual(merged.PresenceTags, base.PresenceTags) {
		t.Errorf("PresenceTags leaked: %v", merged.PresenceTags)
	}
}

func TestMergeProfile_HostFromOverride(t *testing.T) {
	base := DefaultT2TMUDProfile()
	override := MUDProfile{Host: "example.org"}
	merged := MergeProfile(base, override)
	if merged.Host != "example.org" {
		t.Errorf("Host = %q, want example.org", merged.Host)
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/mapper/ -run TestMergeProfile -v`
Expected: FAIL — `undefined: MergeProfile`.

- [ ] **Step 3: Implement `MergeProfile`**

Append to `internal/mapper/profile.go`:

```go
// MergeProfile returns a new profile in which every field of `over` that is
// non-zero replaces the corresponding field of `base`. Slice fields are
// replaced wholesale (not appended) when non-nil and non-empty.
func MergeProfile(base, over MUDProfile) MUDProfile {
	out := base
	if over.Host != "" {
		out.Host = over.Host
	}
	if over.BlockStartTag != "" {
		out.BlockStartTag = over.BlockStartTag
	}
	if over.BlockEndPattern != "" {
		out.BlockEndPattern = over.BlockEndPattern
	}
	if len(over.ExitTags) > 0 {
		out.ExitTags = append([]string(nil), over.ExitTags...)
	}
	if len(over.PresenceTags) > 0 {
		out.PresenceTags = append([]string(nil), over.PresenceTags...)
	}
	if len(over.WeatherPatterns) > 0 {
		out.WeatherPatterns = append([]string(nil), over.WeatherPatterns...)
	}
	if over.DoorStatePattern != "" {
		out.DoorStatePattern = over.DoorStatePattern
	}
	return out
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mapper/ -run TestMergeProfile -v`
Expected: PASS for all three.

- [ ] **Step 5: Commit**

```bash
git add internal/mapper/profile.go internal/mapper/profile_test.go
git commit -m "feat(mapper): MergeProfile per-field override"
```

---

### Task 3: Compile profile patterns into regexes

**Files:**
- Modify: `internal/mapper/profile.go`
- Modify: `internal/mapper/profile_test.go`

- [ ] **Step 1: Write failing test**

Append to `profile_test.go`:

```go
func TestMUDProfile_Compile(t *testing.T) {
	p := DefaultT2TMUDProfile()
	c, err := p.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if c.EndRe == nil {
		t.Fatal("EndRe nil")
	}
	if !c.EndRe.MatchString("\nHP:70 EP:70 [WA] > ") {
		t.Errorf("EndRe should match captured prompt (no ANSI)")
	}
	if !c.EndRe.MatchString("\nHP:70 EP:70 [\x1b[1;32mWA\x1b[0m] > ") {
		t.Errorf("EndRe should match prompt with ANSI inside brackets")
	}
	if len(c.WeatherR) != len(p.WeatherPatterns) {
		t.Fatalf("WeatherR len = %d, want %d", len(c.WeatherR), len(p.WeatherPatterns))
	}
	if !c.WeatherR[0].MatchString("The sky is dark blue.") {
		t.Errorf("WeatherR[0] should match weather sentence")
	}
	if c.DoorR == nil || !c.DoorR.MatchString("The south door is closed.") {
		t.Errorf("DoorR should match door-state sentence")
	}
}

func TestMUDProfile_Compile_BadRegex(t *testing.T) {
	p := DefaultT2TMUDProfile()
	p.WeatherPatterns = []string{`(unbalanced`}
	if _, err := p.Compile(); err == nil {
		t.Errorf("Compile should fail on bad regex")
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/mapper/ -run TestMUDProfile_Compile -v`
Expected: FAIL — `Compile undefined`.

- [ ] **Step 3: Implement `Compile`**

Append to `profile.go`:

```go
import "regexp"

// CompiledProfile holds compiled regex versions of a MUDProfile's patterns
// for hot-path use.
type CompiledProfile struct {
	Profile  MUDProfile
	EndRe    *regexp.Regexp
	WeatherR []*regexp.Regexp
	DoorR    *regexp.Regexp
}

// Compile builds the regex companions; returns an error if any pattern is
// malformed.
func (p MUDProfile) Compile() (*CompiledProfile, error) {
	endRe, err := regexp.Compile(p.BlockEndPattern)
	if err != nil {
		return nil, fmt.Errorf("block_end_pattern: %w", err)
	}
	weatherR := make([]*regexp.Regexp, 0, len(p.WeatherPatterns))
	for i, pat := range p.WeatherPatterns {
		r, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("weather_patterns[%d]: %w", i, err)
		}
		weatherR = append(weatherR, r)
	}
	var doorR *regexp.Regexp
	if p.DoorStatePattern != "" {
		doorR, err = regexp.Compile(p.DoorStatePattern)
		if err != nil {
			return nil, fmt.Errorf("door_state_pattern: %w", err)
		}
	}
	return &CompiledProfile{
		Profile:  p,
		EndRe:    endRe,
		WeatherR: weatherR,
		DoorR:    doorR,
	}, nil
}
```

Add `import "fmt"` at the top of `profile.go` (the existing file likely has no imports yet).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mapper/ -run TestMUDProfile_Compile -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mapper/profile.go internal/mapper/profile_test.go
git commit -m "feat(mapper): MUDProfile.Compile compiles regex patterns"
```

**CHECKPOINT A complete.**

---

## Checkpoint B — MXP segment sink

This checkpoint replaces the original "tag callback" plan with a
segment-based `Sink` API. The motivation: a single `func(name, body string)`
callback gives subscribers the wrong text-position context — the buffer
appends the entire `Filter(s)` return value at once, but tag callbacks
fired mid-`Filter`. With a segment sink, `Filter` flushes its in-progress
output to `OnText` immediately before each `OnTag` call, so the buffer's
own `b.text.Len()` is automatically correct at the moment of every tag.

The existing `Filter(s) string` API stays — non-buffer callers continue
to receive the concatenated stripped text. The sink hooks are additive.

### Task 4: `mxp.Protocol.SetSink` (segment-based interface)

**Files:**
- Modify: `internal/mudproto/mxp/mxp.go`
- Modify: `internal/mudproto/mxp/mxp_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/mudproto/mxp/mxp_test.go`:

```go
type captureSink struct {
	events []string // formatted as "T:foo" for text, "G:name" for tag
}

func (s *captureSink) OnText(t string)        { s.events = append(s.events, "T:"+t) }
func (s *captureSink) OnTag(name, body string) { s.events = append(s.events, "G:"+name) }

func TestSetSink_InterleavesTextAndTags(t *testing.T) {
	p := New()
	s := &captureSink{}
	p.SetSink(s)

	_ = p.Filter("hello<expire>world<x>east</x>!")

	want := []string{"T:hello", "G:expire", "T:world", "G:x", "T:east", "G:/x", "T:!"}
	if len(s.events) != len(want) {
		t.Fatalf("got %v, want %v", s.events, want)
	}
	for i := range want {
		if s.events[i] != want[i] {
			t.Errorf("events[%d] = %q, want %q", i, s.events[i], want[i])
		}
	}
}

func TestSetSink_NilSafe(t *testing.T) {
	p := New()
	// Without registering a sink, Filter must still strip cleanly.
	out := p.Filter("<x>east</x>")
	if out != "east" {
		t.Errorf("Filter(\"<x>east</x>\") = %q, want \"east\"", out)
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/mudproto/mxp/ -run TestSetSink -v`
Expected: FAIL — `SetSink undefined`, `Sink undefined`.

- [ ] **Step 3: Implement**

In `internal/mudproto/mxp/mxp.go`:

Add the interface (place near the `Mode` constants, before the `Protocol` struct):

```go
// Sink receives interleaved text segments and tag observations from
// Filter() in arrival order. A subscriber that wants to correlate tag
// positions with text offsets can rely on the invariant: when OnTag
// fires, all preceding text segments have already been delivered via
// OnText.
type Sink interface {
	OnText(s string)
	OnTag(name, body string)
}
```

Add to the `Protocol` struct:

```go
type Protocol struct {
	mode        Mode
	defaultMode Mode
	ctx         network.Context
	onRoomName  func(name string)
	sink        Sink
	log         protolog.Logger
	pending     string
}
```

Add the setter (place near `SetRoomNameCallback`):

```go
// SetSink installs a Sink that receives interleaved text segments and
// tag observations. Nil disables sink delivery. Filter()'s string return
// value is unaffected and continues to provide the full stripped text
// for callers that don't need interleaving.
func (p *Protocol) SetSink(s Sink) { p.sink = s }
```

Refactor `Filter` to flush `b` to the sink before each tag callback. The
cleanest shape:

```go
func (p *Protocol) Filter(s string) string {
	if p.pending != "" {
		s = p.pending + s
		p.pending = ""
	}
	if !strings.ContainsAny(s, "<\x1b\n") {
		if p.sink != nil && len(s) > 0 {
			p.sink.OnText(s)
		}
		return s
	}
	logOn := p.logEnabled()
	if logOn && strings.ContainsAny(s, "<\x1b") {
		p.logEvent("chunk", []byte(s), nil)
	}
	var b strings.Builder
	b.Grow(len(s))

	flushText := func() {
		if p.sink == nil {
			return
		}
		if b.Len() == 0 {
			return
		}
		// Slice the new bytes added since last flush.
		// We track the flush boundary via a separate counter.
	}
	// Replaced flushText approach: explicit lastFlush counter.
	lastFlush := 0
	flush := func() {
		if p.sink == nil {
			return
		}
		if b.Len() > lastFlush {
			p.sink.OnText(b.String()[lastFlush:])
			lastFlush = b.Len()
		}
	}

	for i := 0; i < len(s); {
		ch := s[i]
		switch ch {
		case '<':
			if end := findTagEnd(s[i:]); end >= 0 {
				body := s[i+1 : i+end]
				p.handleProbe(body)
				p.handleRoomName(s, i+1, i+end, &b, logOn)
				if logOn {
					p.logEvent("tag", []byte(s[i:i+end+1]), map[string]any{"name": extractTagName(body), "body": body})
				}
				if p.sink != nil {
					flush()
					p.sink.OnTag(extractTagName(body), body)
				}
				i += end + 1
				continue
			}
			p.pending = s[i:]
			flush()
			return b.String()
		case '\x1b':
			if n, num, ok := parseModeEsc(s[i:]); ok {
				p.applyMode(num)
				i += n
				continue
			}
			b.WriteByte(ch)
			i++
		case '\n':
			b.WriteByte(ch)
			p.mode = p.defaultMode
			i++
		default:
			b.WriteByte(ch)
			i++
		}
	}
	flush()
	return b.String()
}
```

(Remove the unused `flushText` closure draft above; the final form uses
only the `flush` closure with the `lastFlush` counter.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mudproto/mxp/ -v`
Expected: PASS for new tests; all existing MXP tests still green
(behaviour without a sink is unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/mudproto/mxp/mxp.go internal/mudproto/mxp/mxp_test.go
git commit -m "feat(mxp): segment-based Sink interface for interleaved tag/text"
```

---

### Task 5: Sink survives split tags

The Phase 1 split-tag buffering (`p.pending`) means partial tags don't
fire the sink until completed. Verify this is correct.

- [ ] **Step 1: Write the test**

Add to `mxp_test.go`:

```go
func TestSetSink_FiresOnceAfterSplitCompletion(t *testing.T) {
	p := New()
	s := &captureSink{}
	p.SetSink(s)

	_ = p.Filter("<x")
	for _, e := range s.events {
		if strings.HasPrefix(e, "G:") {
			t.Errorf("tag event fired on partial tag: %q", e)
		}
	}
	_ = p.Filter(">east</x>")

	var tagEvents []string
	for _, e := range s.events {
		if strings.HasPrefix(e, "G:") {
			tagEvents = append(tagEvents, e[2:])
		}
	}
	want := []string{"x", "/x"}
	if len(tagEvents) != len(want) {
		t.Fatalf("got %v, want %v", tagEvents, want)
	}
	for i := range want {
		if tagEvents[i] != want[i] {
			t.Errorf("tagEvents[%d] = %q, want %q", i, tagEvents[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/mudproto/mxp/ -run TestSetSink_FiresOnceAfterSplitCompletion -v`
Expected: PASS (Phase 1 split-tag buffering already handles this; the
test is a regression guard).

- [ ] **Step 3: Commit**

```bash
git add internal/mudproto/mxp/mxp_test.go
git commit -m "test(mxp): sink fires once after split-tag completion"
```

**CHECKPOINT B complete.**

---

## Checkpoint C — RoomBlockBuffer + parser

### Task 6: `RoomBlock` type and `RoomBlockBuffer` constructor

**Files:**
- Create: `internal/mapper/blockbuf.go`
- Create: `internal/mapper/blockbuf_test.go`

- [ ] **Step 1: Write the failing test**

`internal/mapper/blockbuf_test.go`:

```go
package mapper

import (
	"testing"
)

func TestNewRoomBlockBuffer(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, err := p.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	called := false
	b := NewRoomBlockBuffer(cp, func(rb RoomBlock) { called = true })
	if b == nil {
		t.Fatal("NewRoomBlockBuffer returned nil")
	}
	// No tags or text yet — onClose must not fire.
	if called {
		t.Errorf("onClose fired on construction")
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/mapper/ -run TestNewRoomBlockBuffer -v`
Expected: FAIL — `RoomBlock`, `RoomBlockBuffer`, `NewRoomBlockBuffer` undefined.

- [ ] **Step 3: Implement skeleton**

Create `internal/mapper/blockbuf.go`:

```go
package mapper

import (
	"strings"
)

// RoomBlock holds the parsed output of one MXP-driven room render.
type RoomBlock struct {
	Description string   // Cleaned static description (post-strip, ANSI removed).
	Exits       []string // Normalised short-form exit directions, in tag order, deduplicated.
	Presence    []string // Trimmed text of any presence-tag-marked lines.
	Vnum        string   // Empty unless a future profile populates it.
	Name        string   // From <ROOMNAME> if observed; otherwise empty.
}

type taggedFragment struct {
	name    string
	body    string
	textPos int
}

// RoomBlockBuffer accumulates MXP tag callbacks and stripped text between
// a profile.BlockStartTag and a match against profile.BlockEndPattern.
// It is single-goroutine-safe: callers must not concurrently invoke OnTag,
// OnText, or Reset.
type RoomBlockBuffer struct {
	cp      *CompiledProfile
	open    bool
	text    strings.Builder
	tags    []taggedFragment
	onClose func(RoomBlock)
}

// NewRoomBlockBuffer constructs a buffer that calls onClose with a parsed
// RoomBlock each time a complete block is observed.
func NewRoomBlockBuffer(cp *CompiledProfile, onClose func(RoomBlock)) *RoomBlockBuffer {
	return &RoomBlockBuffer{cp: cp, onClose: onClose}
}

// Reset clears any in-flight block (called on disconnect or manual abort).
func (b *RoomBlockBuffer) Reset() {
	b.open = false
	b.text.Reset()
	b.tags = b.tags[:0]
}
```

- [ ] **Step 4: Run test**

Run: `go test ./internal/mapper/ -run TestNewRoomBlockBuffer -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mapper/blockbuf.go internal/mapper/blockbuf_test.go
git commit -m "feat(mapper): RoomBlock and RoomBlockBuffer skeleton"
```

---

### Task 7: `OnTag` open/close/append behaviour

**Files:**
- Modify: `internal/mapper/blockbuf.go`
- Modify: `internal/mapper/blockbuf_test.go`

- [ ] **Step 1: Write failing tests**

Append to `blockbuf_test.go`:

```go
func TestRoomBlockBuffer_OnTag_BlockStartOpens(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	b := NewRoomBlockBuffer(cp, func(RoomBlock) {})
	b.OnTag("expire", "expire")
	if !b.open {
		t.Errorf("buffer not open after BlockStartTag")
	}
}

func TestRoomBlockBuffer_OnTag_IgnoredWhenClosed(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	b := NewRoomBlockBuffer(cp, func(RoomBlock) {})
	b.OnTag("x", "x")
	if len(b.tags) != 0 {
		t.Errorf("tag recorded while buffer closed: %+v", b.tags)
	}
}

func TestRoomBlockBuffer_OnTag_ReopenDiscardsPartial(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	b := NewRoomBlockBuffer(cp, func(RoomBlock) {})
	b.OnTag("expire", "expire")
	b.OnTag("x", "x")
	b.OnTag("expire", "expire") // re-open: discards in-flight.
	if !b.open {
		t.Errorf("buffer should remain open after re-open")
	}
	if len(b.tags) != 0 {
		t.Errorf("tags survived re-open: %+v", b.tags)
	}
	if b.text.Len() != 0 {
		t.Errorf("text survived re-open: %q", b.text.String())
	}
}

func TestRoomBlockBuffer_OnTag_AppendsWhileOpen(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	b := NewRoomBlockBuffer(cp, func(RoomBlock) {})
	b.OnTag("expire", "expire")
	b.OnTag("x", "x")
	b.OnTag("/x", "/x")
	if len(b.tags) != 2 {
		t.Errorf("len(tags) = %d, want 2", len(b.tags))
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/mapper/ -run TestRoomBlockBuffer_OnTag -v`
Expected: FAIL — `OnTag undefined`.

- [ ] **Step 3: Implement `OnTag`**

Append to `blockbuf.go`:

```go
// OnTag records an MXP tag observation. The block-start tag opens (or
// re-opens) a fresh block; other tags are appended only while the buffer
// is open.
func (b *RoomBlockBuffer) OnTag(name, body string) {
	if name == b.cp.Profile.BlockStartTag {
		b.text.Reset()
		b.tags = b.tags[:0]
		b.open = true
		return
	}
	if !b.open {
		return
	}
	b.tags = append(b.tags, taggedFragment{name: name, body: body, textPos: b.text.Len()})
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mapper/ -run TestRoomBlockBuffer_OnTag -v`
Expected: PASS for all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/mapper/blockbuf.go internal/mapper/blockbuf_test.go
git commit -m "feat(mapper): RoomBlockBuffer.OnTag lifecycle"
```

---

### Task 8: `OnText` accumulates and detects block end

**Files:**
- Modify: `internal/mapper/blockbuf.go`
- Modify: `internal/mapper/blockbuf_test.go`

- [ ] **Step 1: Write failing tests**

Append:

```go
func TestRoomBlockBuffer_OnText_AccumulatesWhileOpen(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	b := NewRoomBlockBuffer(cp, func(RoomBlock) {})
	b.OnTag("expire", "expire")
	b.OnText("    A room.")
	b.OnText("\nThe sky is dark blue.")
	if b.text.String() != "    A room.\nThe sky is dark blue." {
		t.Errorf("text not accumulated: %q", b.text.String())
	}
}

func TestRoomBlockBuffer_OnText_IgnoresWhileClosed(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	b := NewRoomBlockBuffer(cp, func(RoomBlock) {})
	b.OnText("ambient flavor before any block")
	if b.text.Len() != 0 {
		t.Errorf("text accumulated while closed: %q", b.text.String())
	}
}

func TestRoomBlockBuffer_OnText_PromptClosesBlock(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	var emitted []RoomBlock
	b := NewRoomBlockBuffer(cp, func(rb RoomBlock) { emitted = append(emitted, rb) })
	b.OnTag("expire", "expire")
	b.OnText("    A room.\nThe sky is dark blue.\n    The only obvious exit is north.")
	b.OnText("\nHP:70 EP:70 [WA] > ")
	if len(emitted) != 1 {
		t.Fatalf("expected exactly 1 emission, got %d", len(emitted))
	}
	if b.open {
		t.Errorf("buffer should be closed after emit")
	}
}

func TestRoomBlockBuffer_OnText_PromptWithANSIClosesBlock(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	var emitted []RoomBlock
	b := NewRoomBlockBuffer(cp, func(rb RoomBlock) { emitted = append(emitted, rb) })
	b.OnTag("expire", "expire")
	b.OnText("    A room.\n    The only obvious exit is north.")
	// Real t2tmud prompt has ANSI inside the brackets.
	b.OnText("\nHP:70 EP:70 [\x1b[1;32mWA\x1b[0m] > ")
	if len(emitted) != 1 {
		t.Fatalf("expected exactly 1 emission with ANSI prompt, got %d", len(emitted))
	}
	if b.open {
		t.Errorf("buffer should be closed after ANSI prompt")
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/mapper/ -run TestRoomBlockBuffer_OnText -v`
Expected: FAIL — `OnText undefined`.

- [ ] **Step 3: Implement `OnText`**

Append to `blockbuf.go`:

```go
// OnText appends post-MXP-strip text to the in-flight block. When the
// accumulated text contains a match for profile.BlockEndPattern, the
// block is parsed and emitted via the onClose callback.
func (b *RoomBlockBuffer) OnText(s string) {
	if !b.open {
		return
	}
	b.text.WriteString(s)
	full := b.text.String()
	if loc := b.cp.EndRe.FindStringIndex(full); loc != nil {
		body := full[:loc[0]]
		rb := b.parse(body)
		b.open = false
		b.text.Reset()
		b.tags = b.tags[:0]
		if b.onClose != nil {
			b.onClose(rb)
		}
	}
}

// parse is implemented in Task 10. Stub for now.
func (b *RoomBlockBuffer) parse(body string) RoomBlock {
	return RoomBlock{}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mapper/ -run TestRoomBlockBuffer_OnText -v`
Expected: PASS for all three.

- [ ] **Step 5: Commit**

```bash
git add internal/mapper/blockbuf.go internal/mapper/blockbuf_test.go
git commit -m "feat(mapper): RoomBlockBuffer.OnText with end-pattern detection"
```

---

### Task 9: ANSI strip helper

**Files:**
- Modify: `internal/mapper/blockbuf.go`
- Modify: `internal/mapper/blockbuf_test.go`

- [ ] **Step 1: Write failing test**

Append:

```go
func TestStripANSI(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"\x1b[0;32mgreen\x1b[0m", "green"},
		{"a\x1b[1;37mword\x1b[0m b", "aword b"},
		{"\x1b[4z<x>east</x>", "<x>east</x>"}, // mode escapes also stripped
		{"", ""},
	}
	for _, c := range cases {
		got := stripANSI(c.in)
		if got != c.want {
			t.Errorf("stripANSI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/mapper/ -run TestStripANSI -v`
Expected: FAIL — `stripANSI undefined`.

- [ ] **Step 3: Implement `stripANSI`**

Append to `blockbuf.go`:

```go
import "regexp"

// ansiRe matches CSI sequences of the form `ESC [ <params> <final-byte>`
// where the final byte is in the range 0x40..0x7E. Covers both ANSI
// colour codes (`\x1b[0;32m`) and MXP mode escapes (`\x1b[4z`).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// stripANSI removes CSI / mode escape sequences from s.
func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}
```

(If `regexp` is already imported in `blockbuf.go` from earlier tasks, do not duplicate.)

- [ ] **Step 4: Run test**

Run: `go test ./internal/mapper/ -run TestStripANSI -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mapper/blockbuf.go internal/mapper/blockbuf_test.go
git commit -m "feat(mapper): stripANSI helper"
```

---

### Task 10: Block parser produces a complete `RoomBlock`

**Files:**
- Modify: `internal/mapper/blockbuf.go`
- Modify: `internal/mapper/blockbuf_test.go`

- [ ] **Step 1: Write failing tests**

Append:

```go
func TestRoomBlockBuffer_ParsesPlainsTile(t *testing.T) {
	p := DefaultT2TMUDProfile()
	cp, _ := p.Compile()
	var got RoomBlock
	b := NewRoomBlockBuffer(cp, func(rb RoomBlock) { got = rb })

	b.OnTag("expire", "expire")
	b.OnText(
		"    Gently rolling plains extend into the distance.\n" +
			"This tranquil plain manifests a sense of peace.\n" +
			"The Lune River lies north and northeast.\n" +
			"The sky is dark blue and a yellow glow comes from the west.\n" +
			"A crystal clear sky hovers over the landscape.\n" +
			"    There is water to the ",
	)
	b.OnTag("w", "w")
	b.OnText("north")
	b.OnTag("/w", "/w")
	b.OnText(" and ")
	b.OnTag("w", "w")
	b.OnText("northeast")
	b.OnTag("/w", "/w")
	b.OnText(".\n    The only obvious exits are ")
	b.OnTag("x", "x")
	b.OnText("east")
	b.OnTag("/x", "/x")
	b.OnText(", ")
	b.OnTag("x", "x")
	b.OnText("south")
	b.OnTag("/x", "/x")
	b.OnText(".\n")
	b.OnTag("i30", `i30 "orc 1"`)
	b.OnText("An ugly orc")
	b.OnTag("/i30", "/i30")
	b.OnText("\nHP:70 EP:70 [WA] > ")

	if !sliceEq(got.Exits, []string{"e", "n", "ne", "s"}) {
		t.Errorf("Exits = %v, want [e n ne s]", got.Exits)
	}
	if !strings.Contains(got.Description, "Gently rolling plains") {
		t.Errorf("description missing static prose: %q", got.Description)
	}
	if !strings.Contains(got.Description, "Lune River") {
		t.Errorf("description must keep nearby-sight: %q", got.Description)
	}
	if strings.Contains(got.Description, "The sky is") {
		t.Errorf("description should drop weather: %q", got.Description)
	}
	if strings.Contains(got.Description, "ugly orc") {
		t.Errorf("description should drop presence line: %q", got.Description)
	}
	if strings.Contains(got.Description, "obvious exits") {
		t.Errorf("description should drop exits sentence: %q", got.Description)
	}
	if strings.Contains(got.Description, "There is water") {
		t.Errorf("description should drop water sentence: %q", got.Description)
	}
	if len(got.Presence) != 1 || !strings.Contains(got.Presence[0], "ugly orc") {
		t.Errorf("Presence = %v, want one orc line", got.Presence)
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

(Add `"strings"` to test imports if not already there.)

- [ ] **Step 2: Run test**

Run: `go test ./internal/mapper/ -run TestRoomBlockBuffer_ParsesPlainsTile -v`
Expected: FAIL — parser stub returns empty.

- [ ] **Step 3: Implement `parse`**

Replace the stub `parse` in `blockbuf.go` with:

```go
func (b *RoomBlockBuffer) parse(body string) RoomBlock {
	prof := b.cp.Profile

	// Build tag-name lookup sets.
	exitSet := make(map[string]struct{}, len(prof.ExitTags))
	for _, t := range prof.ExitTags {
		exitSet[t] = struct{}{}
	}
	presenceSet := make(map[string]struct{}, len(prof.PresenceTags))
	for _, t := range prof.PresenceTags {
		presenceSet[t] = struct{}{}
	}

	// Exit extraction: walk tags in order; the body of each exit-tag-open
	// (e.g. "x", "w") will be followed by tag-internal text in the body
	// stream. Use the next textual token starting at the tag's textPos.
	var exits []string
	seenExits := make(map[string]struct{})
	for i, t := range b.tags {
		if _, isExit := exitSet[t.name]; !isExit {
			continue
		}
		// Find the matching close. The display text is between tag.textPos
		// and the next close tag (or end of body).
		closePos := len(body)
		for j := i + 1; j < len(b.tags); j++ {
			if b.tags[j].name == "/"+t.name {
				closePos = b.tags[j].textPos
				break
			}
		}
		if t.textPos > len(body) {
			continue
		}
		raw := body[t.textPos:min(closePos, len(body))]
		raw = stripANSI(raw)
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		dir, ok := ParseDirection(raw)
		if !ok {
			continue
		}
		if _, dup := seenExits[string(dir)]; dup {
			continue
		}
		seenExits[string(dir)] = struct{}{}
		exits = append(exits, string(dir))
	}

	// Track byte offsets of each line in the original (pre-strip) body so we
	// can map tag.textPos to a line index.
	lineStarts := []int{0}
	for i := 0; i < len(body); i++ {
		if body[i] == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	lineForOffset := func(off int) int {
		// Find the largest lineStart <= off.
		lo, hi := 0, len(lineStarts)-1
		for lo < hi {
			mid := (lo + hi + 1) / 2
			if lineStarts[mid] <= off {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		return lo
	}

	// Determine which line indices to drop because they contain a
	// presence tag, plus collect the presence display strings.
	dropLines := make(map[int]bool)
	var presence []string
	rawLines := strings.Split(body, "\n")
	for _, t := range b.tags {
		if _, isPres := presenceSet[t.name]; !isPres {
			continue
		}
		idx := lineForOffset(t.textPos)
		if idx < 0 || idx >= len(rawLines) {
			continue
		}
		if !dropLines[idx] {
			plain := strings.TrimSpace(stripANSI(rawLines[idx]))
			if plain != "" {
				presence = append(presence, plain)
			}
		}
		dropLines[idx] = true
	}

	// Build the cleaned description.
	var keep []string
	for i, line := range rawLines {
		if dropLines[i] {
			continue
		}
		plain := strings.TrimSpace(stripANSI(line))
		if plain == "" {
			continue
		}
		if matchesAny(plain, b.cp.WeatherR) {
			continue
		}
		if b.cp.DoorR != nil && b.cp.DoorR.MatchString(plain) {
			continue
		}
		// Drop exits / water-exits sentences (presentation, not identity).
		if strings.HasPrefix(plain, "The only obvious exit") {
			continue
		}
		if strings.HasPrefix(plain, "There is water to the ") {
			continue
		}
		keep = append(keep, plain)
	}

	return RoomBlock{
		Description: strings.Join(keep, "\n"),
		Exits:       exits,
		Presence:    presence,
	}
}

func matchesAny(s string, rs []*regexp.Regexp) bool {
	for _, r := range rs {
		if r.MatchString(s) {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

(`ParseDirection` is the existing helper in `mapper.go`. Confirm it's exported.)

- [ ] **Step 4: Run test**

Run: `go test ./internal/mapper/ -run TestRoomBlockBuffer_ParsesPlainsTile -v`
Expected: PASS.

- [ ] **Step 5: Run full mapper suite**

Run: `go test ./internal/mapper/ -race -v`
Expected: PASS for the package as a whole.

- [ ] **Step 6: Commit**

```bash
git add internal/mapper/blockbuf.go internal/mapper/blockbuf_test.go
git commit -m "feat(mapper): RoomBlockBuffer parser drops dynamic lines, keeps sights"
```

**CHECKPOINT C complete.** Pause for review.

---

## Checkpoint D — Structural hash + Engine integration

### Task 11: `ComputeStructuralHash` + `normaliseDescription`

**Files:**
- Modify: `internal/mapper/mapper.go`
- Modify: `internal/mapper/mapper_test.go`

- [ ] **Step 1: Write failing tests**

Append to `mapper_test.go`:

```go
func TestNormaliseDescription(t *testing.T) {
	in := "  A   plain.\n  Two  spaces.  "
	want := "A plain.\nTwo spaces."
	if got := normaliseDescription(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestComputeStructuralHash_Stable(t *testing.T) {
	a := ComputeStructuralHash("A room.", []string{"n", "e"})
	b := ComputeStructuralHash("A room.", []string{"e", "n"})
	if a != b {
		t.Errorf("hash should ignore exit order: %q vs %q", a, b)
	}
}

func TestComputeStructuralHash_DifferentExitsDiffer(t *testing.T) {
	a := ComputeStructuralHash("A room.", []string{"n"})
	b := ComputeStructuralHash("A room.", []string{"s"})
	if a == b {
		t.Errorf("hash should differ on exit set")
	}
}

func TestComputeStructuralHash_DistinctFromV1(t *testing.T) {
	v2 := ComputeStructuralHash("A room.", []string{"n"})
	v1 := ComputeRoomHash("A room.", []string{"n"})
	if !strings.HasPrefix(v2, "v2:") {
		t.Errorf("v2 hash missing prefix: %q", v2)
	}
	if v1 == v2 {
		t.Errorf("v1 and v2 hashes must not collide")
	}
	if strings.HasPrefix(v1, "v2:") {
		t.Errorf("v1 hash unexpectedly has v2 prefix: %q", v1)
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/mapper/ -run "TestNormaliseDescription|TestComputeStructuralHash" -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Add to `internal/mapper/mapper.go` (next to `ComputeRoomHash` around line 223):

```go
// normaliseDescription trims surrounding whitespace and collapses any run
// of internal whitespace within a line down to a single space. Newlines
// are preserved. Used as the canonical form for structural hashing.
func normaliseDescription(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for _, line := range lines {
		// Collapse runs of whitespace to a single space.
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		out = append(out, strings.Join(fields, " "))
	}
	return strings.Join(out, "\n")
}

// ComputeStructuralHash builds a Phase 2 room-identity hash from a cleaned
// description and a sorted, de-duplicated exit set. The returned value
// is prefixed "v2:" so it is externally distinguishable from legacy
// ComputeRoomHash output (plain 64-hex). The prefix doubles as a debug
// aid when grepping saved maps and JSON logs.
func ComputeStructuralHash(desc string, exits []string) string {
	norm := normaliseDescription(desc)
	sortedExits := append([]string(nil), exits...)
	sort.Strings(sortedExits)
	payload := "v2|" + norm + "|" + strings.Join(sortedExits, ",")
	h := sha256.Sum256([]byte(payload))
	return "v2:" + hex.EncodeToString(h[:])
}
```

(`sort`, `crypto/sha256`, `encoding/hex`, `strings` are already imported in mapper.go.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mapper/ -run "TestNormaliseDescription|TestComputeStructuralHash" -v`
Expected: PASS for all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/mapper/mapper.go internal/mapper/mapper_test.go
git commit -m "feat(mapper): ComputeStructuralHash with v2 prefix"
```

---

### Task 12: Engine state for profile + dual MXP/block-start sentinels

The "MXP active" gate cannot rely on `Installed()` (always true if
compiled in) or `ActiveProtocols()` (true under `AlwaysActive` even
pre-negotiation, see `internal/mudproto/mxp/mxp.go:97`). The only proof
that MXP is genuinely working on this connection is **observation of an
actual MXP-emitted tag**. We therefore introduce TWO independent
sentinels:

- `sawMXP` — flipped on every MXP tag arrival. Means "MXP is active".
- `sawBlockStart` — flipped on the configured `BlockStartTag` tag
  arrival (a strict subset of `sawMXP`). Means "we have observable
  block boundaries on this MUD".

`ProcessMovement` requires BOTH to be true (Task 15). `ResetBlockStart`
clears BOTH on disconnect.

**Files:**
- Modify: `internal/mapper/mapper.go`
- Modify: `internal/mapper/mapper_test.go`

- [ ] **Step 1: Write failing tests**

Append:

```go
func TestEngineMUDProfile_RoundTrip(t *testing.T) {
	e := NewEngine("")
	got := e.GetMUDProfile()
	if got.Host != "t2tmud.org" {
		t.Errorf("default profile host = %q, want t2tmud.org", got.Host)
	}
	custom := MUDProfile{Host: "example.org", BlockStartTag: "ROOM"}
	e.SetMUDProfile(custom)
	if e.GetMUDProfile().Host != "example.org" {
		t.Errorf("SetMUDProfile did not persist host")
	}
}

func TestEngineSentinels(t *testing.T) {
	e := NewEngine("")
	if e.HasMXPSeen() {
		t.Errorf("default HasMXPSeen must be false")
	}
	if e.HasBlockStartSeen() {
		t.Errorf("default HasBlockStartSeen must be false")
	}
	e.MarkMXPSeen()
	if !e.HasMXPSeen() {
		t.Errorf("MarkMXPSeen did not set sawMXP")
	}
	if e.HasBlockStartSeen() {
		t.Errorf("MarkMXPSeen must NOT set sawBlockStart")
	}
	e.MarkBlockStartSeen()
	if !e.HasBlockStartSeen() {
		t.Errorf("MarkBlockStartSeen did not set sawBlockStart")
	}
	e.ResetBlockStart()
	if e.HasMXPSeen() {
		t.Errorf("ResetBlockStart did not clear sawMXP")
	}
	if e.HasBlockStartSeen() {
		t.Errorf("ResetBlockStart did not clear sawBlockStart")
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/mapper/ -run "TestEngineMUDProfile|TestEngineSentinels" -v`
Expected: FAIL — methods undefined.

- [ ] **Step 3: Implement**

In `internal/mapper/mapper.go`, add fields to `Engine`:

```go
type Engine struct {
	// ...existing fields...
	profile         MUDProfile
	sawMXP          bool
	sawBlockStart   bool
	structuralIndex map[string]string // v2 hash -> RoomID
}
```

Update `NewEngine` to initialise:

```go
func NewEngine(path string) *Engine {
	return &Engine{
		data: &Map{
			Rooms: make(map[string]*Room),
		},
		path:            path,
		undo:            make([]mapCommand, 0),
		paths:           DefaultPaths(),
		hashIndex:       make(map[string]string),
		options:         DefaultMappingOptions(),
		profile:         DefaultT2TMUDProfile(),
		structuralIndex: make(map[string]string),
	}
}
```

Add methods (place near existing `SetMappingOptions`):

```go
// SetMUDProfile replaces the MUD profile used by Phase 2 block parsing.
func (e *Engine) SetMUDProfile(p MUDProfile) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.profile = p
}

// GetMUDProfile returns the current MUD profile.
func (e *Engine) GetMUDProfile() MUDProfile {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.profile
}

// MarkMXPSeen flags that at least one MXP-emitted tag has been observed
// on the current connection — the only reliable proof that MXP is
// active end-to-end (Installed and ActiveProtocols both lie under
// AlwaysActive). Auto-mapping requires this AND the block-start
// sentinel.
func (e *Engine) MarkMXPSeen() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sawMXP = true
}

// HasMXPSeen reports whether any MXP tag has been observed yet.
func (e *Engine) HasMXPSeen() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.sawMXP
}

// MarkBlockStartSeen flags that the configured block-start tag has been
// observed at least once on this connection. Auto-mapping requires this
// AND HasMXPSeen.
func (e *Engine) MarkBlockStartSeen() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sawBlockStart = true
}

// HasBlockStartSeen reports the current block-start sentinel state.
func (e *Engine) HasBlockStartSeen() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.sawBlockStart
}

// ResetBlockStart clears BOTH MXP-seen and block-start sentinels.
// Called by app.go on disconnect so a stale flag from a previous
// connection cannot leak into a fresh non-MXP one.
func (e *Engine) ResetBlockStart() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sawMXP = false
	e.sawBlockStart = false
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mapper/ -run "TestEngineMUDProfile|TestEngineSentinels" -v -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mapper/mapper.go internal/mapper/mapper_test.go
git commit -m "feat(mapper): Engine MUDProfile + sawMXP/sawBlockStart sentinels"
```

---

### Task 13: `Engine.HandleRoomBlock`

**Files:**
- Modify: `internal/mapper/mapper.go`
- Modify: `internal/mapper/mapper_test.go`

- [ ] **Step 1: Write failing tests**

Append to `mapper_test.go`:

```go
func TestHandleRoomBlock_NoOpWhenAutoMappingOff(t *testing.T) {
	e := NewEngine("")
	if err := e.Create(""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	processed, _, _, err := e.HandleRoomBlock(RoomBlock{Description: "x", Exits: []string{"n"}})
	if processed || err != nil {
		t.Errorf("processed=%v err=%v with auto-mapping off", processed, err)
	}
}

func TestHandleRoomBlock_NoOpWhenMXPSeenMissing(t *testing.T) {
	e := NewEngine("")
	_ = e.Create("")
	e.StartAutoMapping()
	e.MarkBlockStartSeen() // sawBlockStart only — MXP-seen not set.
	processed, _, _, _ := e.HandleRoomBlock(RoomBlock{Description: "x", Exits: []string{"n"}})
	if processed {
		t.Errorf("processed=true without sawMXP sentinel")
	}
}

func TestHandleRoomBlock_NoOpWhenBlockStartMissing(t *testing.T) {
	e := NewEngine("")
	_ = e.Create("")
	e.StartAutoMapping()
	e.MarkMXPSeen() // sawMXP only — sawBlockStart not set.
	processed, _, _, _ := e.HandleRoomBlock(RoomBlock{Description: "x", Exits: []string{"n"}})
	if processed {
		t.Errorf("processed=true without sawBlockStart sentinel")
	}
}

func TestHandleRoomBlock_PendingMovementCreatesNewRoom(t *testing.T) {
	e := NewEngine("")
	_ = e.Create("")
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: true})
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	if _, _, err := e.ProcessMovement("n"); err != nil {
		t.Fatalf("ProcessMovement: %v", err)
	}
	_, name, loop, err := e.HandleRoomBlock(RoomBlock{Description: "Plaza.", Exits: []string{"s"}})
	if err != nil {
		t.Fatalf("HandleRoomBlock: %v", err)
	}
	if loop {
		t.Errorf("first arrival should not be a loop")
	}
	curr := e.GetCurrent()
	if curr == nil || curr.Description == "" {
		t.Errorf("current room not populated: %+v", curr)
	}
	_ = name
}

func TestHandleRoomBlock_LoopDetectionByStructuralHash(t *testing.T) {
	e := NewEngine("")
	_ = e.Create("")
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: true})
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()

	// First visit: dig n.
	_, _, _ = e.ProcessMovement("n")
	_, _, _, _ = e.HandleRoomBlock(RoomBlock{Description: "Plaza.", Exits: []string{"s"}})
	plazaID := e.data.CurrentRoom

	// Walk back south.
	_, _, _ = e.ProcessMovement("s")

	// Walk east to a different room.
	_, _, _ = e.ProcessMovement("e")
	_, _, _, _ = e.HandleRoomBlock(RoomBlock{Description: "Market.", Exits: []string{"w"}})

	// Walk back west, then north — should LOOP-DETECT to plaza.
	_, _, _ = e.ProcessMovement("w")
	_, _, _ = e.ProcessMovement("n")
	_, _, loop, _ := e.HandleRoomBlock(RoomBlock{Description: "Plaza.", Exits: []string{"s"}})
	if !loop {
		t.Errorf("expected loop-detected back to plaza")
	}
	if e.data.CurrentRoom != plazaID {
		t.Errorf("current = %s, want plazaID %s", e.data.CurrentRoom, plazaID)
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/mapper/ -run TestHandleRoomBlock -v`
Expected: FAIL — `HandleRoomBlock undefined`.

- [ ] **Step 3: Implement `HandleRoomBlock`**

Append to `mapper.go`:

```go
// HandleRoomBlock processes a fully-buffered room render produced by the
// Phase 2 RoomBlockBuffer. Replaces the per-chunk ProcessRoomData /
// HandleGMCPRoomInfo flow when MXP is the data source. Returns:
//   - processed: true if any state change was attempted.
//   - roomName:  the resolved or freshly-assigned room name.
//   - loopDetected: true if the block matched an existing room.
//   - err:       diagnostic for the caller's status line.
func (e *Engine) HandleRoomBlock(b RoomBlock) (processed bool, roomName string, loopDetected bool, err error) {
	if !e.IsAutoMapping() {
		return false, "", false, nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.sawMXP || !e.sawBlockStart {
		return false, "", false, nil
	}

	// No pending movement: refresh current room descriptors.
	if e.pendingDir == "" {
		curr, ok := e.data.Rooms[e.data.CurrentRoom]
		if ok {
			if b.Description != "" {
				curr.Description = b.Description
				if e.options.Hash {
					h := ComputeStructuralHash(b.Description, b.Exits)
					curr.DescriptionHash = h
					e.structuralIndex[h] = curr.ID
				}
			}
			if b.Name != "" {
				curr.Name = b.Name
			}
		}
		return false, "", false, nil
	}

	fromRoom, ok := e.data.Rooms[e.pendingFromRoom]
	if !ok {
		e.pendingDir = ""
		e.pendingFromRoom = ""
		return false, "", false, fmt.Errorf("source room not found")
	}

	// Loop detection — vnum first (rare on t2tmud), then structural hash.
	if e.options.Vnum && b.Vnum != "" {
		if existing, found := e.data.Rooms[b.Vnum]; found {
			e.saveState()
			e.completePendingMovement(fromRoom, e.pendingDir, b.Vnum, existing)
			return true, existing.Name, true, nil
		}
	}
	if e.options.Hash && b.Description != "" {
		h := ComputeStructuralHash(b.Description, b.Exits)
		if existingID, found := e.structuralIndex[h]; found {
			if existing, ok := e.data.Rooms[existingID]; ok {
				e.saveState()
				e.completePendingMovement(fromRoom, e.pendingDir, existingID, existing)
				return true, existing.Name, true, nil
			}
		}
	}

	// New room. ID: vnum if present and Vnum on; else UUID.
	var newID string
	if e.options.Vnum && b.Vnum != "" {
		newID = b.Vnum
	} else {
		newID = uuid.New().String()
	}

	e.saveState()
	var descHash string
	if e.options.Hash {
		descHash = ComputeStructuralHash(b.Description, b.Exits)
	}
	newRoom := e.createRoomV2(fromRoom, e.pendingDir, newID, b.Name, b.Description, descHash)
	return true, newRoom.Name, false, nil
}

// createRoomV2 mirrors createRoom but writes into the structuralIndex
// instead of the legacy hashIndex. Caller must hold e.mu.
func (e *Engine) createRoomV2(fromRoom *Room, dir Direction, id, name, description, descHash string) *Room {
	nx, ny, nz := fromRoom.X, fromRoom.Y, fromRoom.Z
	switch dir {
	case North:
		ny++
	case South:
		ny--
	case East:
		nx++
	case West:
		nx--
	case NorthEast:
		nx++
		ny++
	case SouthWest:
		nx--
		ny--
	case NorthWest:
		nx--
		ny++
	case SouthEast:
		nx++
		ny--
	case Up:
		nz++
	case Down:
		nz--
	}

	effectiveName := name
	if effectiveName == "" || effectiveName == "New Room" {
		if e.pendingName != "" {
			effectiveName = e.pendingName
		}
	}
	e.pendingName = ""

	room := &Room{
		ID:              id,
		Name:            effectiveName,
		Description:     description,
		DescriptionHash: descHash,
		Exits:           make(map[Direction]string),
		X:               nx,
		Y:               ny,
		Z:               nz,
	}

	fromRoom.Exits[dir] = id
	reverse := ReverseDirection(dir)
	if reverse != "" {
		room.Exits[reverse] = fromRoom.ID
	}

	e.data.Rooms[id] = room
	if e.options.Hash && descHash != "" {
		e.structuralIndex[descHash] = id
	}
	e.data.CurrentRoom = id

	e.pendingDir = ""
	e.pendingFromRoom = ""
	return room
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mapper/ -run TestHandleRoomBlock -v -race`
Expected: PASS for all four tests.

- [ ] **Step 5: Run full mapper suite**

Run: `go test ./internal/mapper/ -race -v`
Expected: package PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/mapper/mapper.go internal/mapper/mapper_test.go
git commit -m "feat(mapper): Engine.HandleRoomBlock with structural hash"
```

---

### Task 14: Persist `description_hash` field for v2 entries on save/load

The `Room.DescriptionHash` field already exists. New rooms write the v2 hash there. On `Create()` (load path), `rebuildHashIndex` rebuilds the legacy index. We need a sibling that rebuilds the structural index.

**Files:**
- Modify: `internal/mapper/mapper.go`

- [ ] **Step 1: Write failing test**

Add to `mapper_test.go`:

```go
func TestEngineLoad_RebuildsStructuralIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "map.json")

	e := NewEngine(path)
	if err := e.Create(""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: true})
	e.StartAutoMapping()
	e.MarkMXPSeen()
	e.MarkBlockStartSeen()
	_, _, _ = e.ProcessMovement("n")
	_, _, _, _ = e.HandleRoomBlock(RoomBlock{Description: "Plaza.", Exits: []string{"s"}})

	if err := e.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	e2 := NewEngine(path)
	if err := e2.Create(path); err != nil {
		t.Fatalf("Create(path): %v", err)
	}
	if len(e2.structuralIndex) == 0 {
		t.Errorf("structuralIndex empty after reload")
	}
}
```

(Add `"path/filepath"` to test imports if missing.)

- [ ] **Step 2: Run test**

Run: `go test ./internal/mapper/ -run TestEngineLoad_RebuildsStructuralIndex -v`
Expected: FAIL — load path doesn't populate `structuralIndex`.

- [ ] **Step 3: Modify `rebuildHashIndex` to populate both indices unconditionally**

We deliberately do NOT recompute hashes on load. Recomputing would fail
when stored `Room.Exits` (graph edges actually mapped) is a strict
subset of the exit set the original `RoomBlock` carried — in that case
the recomputed v2 hash would not match the stored `DescriptionHash` and
the room would silently be misclassified. Instead, populate both indices
with every saved hash and let the lookup priority in `HandleRoomBlock`
(structural first) and `HandleGMCPRoomInfo` (legacy first) consult the
right index per code path.

Cross-version hash collision risk is negligible — `ComputeStructuralHash`
emits a `v2:`-prefixed value (Task 11), so v2 entries cannot accidentally
appear in the legacy lookup either; they just won't match a v1 input.

In `internal/mapper/mapper.go`, replace the existing `rebuildHashIndex`:

```go
// rebuildHashIndex rebuilds both the Phase 1 hashIndex and the Phase 2
// structuralIndex from existing rooms after a load. Every saved hash
// is inserted into both maps; the lookup priority in the caller decides
// which index is consulted. Caller must hold e.mu.
func (e *Engine) rebuildHashIndex() {
	e.hashIndex = make(map[string]string)
	e.structuralIndex = make(map[string]string)
	for id, room := range e.data.Rooms {
		if room.DescriptionHash == "" {
			continue
		}
		e.hashIndex[room.DescriptionHash] = id
		e.structuralIndex[room.DescriptionHash] = id
	}
}
```

- [ ] **Step 4: Run test**

Run: `go test ./internal/mapper/ -run TestEngineLoad_RebuildsStructuralIndex -v`
Expected: PASS.

- [ ] **Step 5: Run full mapper suite**

Run: `go test ./internal/mapper/ -race -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/mapper/mapper.go internal/mapper/mapper_test.go
git commit -m "feat(mapper): rebuildHashIndex populates v2 structural index on load"
```

**CHECKPOINT D complete.** Pause for review.

---

## Checkpoint E — Strict gate + `/map start` warning

### Task 15: `ProcessMovement` requires both sentinels

**Files:**
- Modify: `internal/mapper/mapper.go`
- Modify: `internal/mapper/mapper_test.go`

- [ ] **Step 1: Write failing tests**

Append:

```go
func TestProcessMovement_RequiresMXPSeen(t *testing.T) {
	e := NewEngine("")
	_ = e.Create("")
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: true})
	e.StartAutoMapping()
	e.MarkBlockStartSeen() // only block-start; MXP-seen missing.
	processed, _, err := e.ProcessMovement("n")
	if err != nil {
		t.Fatalf("ProcessMovement: %v", err)
	}
	if processed {
		t.Errorf("processed=true without sawMXP sentinel")
	}
	if e.HasPendingMovement() {
		t.Errorf("pending state set without sentinel")
	}
}

func TestProcessMovement_RequiresBlockStartSeen(t *testing.T) {
	e := NewEngine("")
	_ = e.Create("")
	e.SetMappingOptions(MappingOptions{Vnum: false, Hash: true})
	e.StartAutoMapping()
	e.MarkMXPSeen() // only MXP-seen; block-start missing.
	processed, _, _ := e.ProcessMovement("n")
	if processed {
		t.Errorf("processed=true without sawBlockStart sentinel")
	}
	if e.HasPendingMovement() {
		t.Errorf("pending state set without sentinel")
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/mapper/ -run TestProcessMovement_Requires -v`
Expected: FAIL — `ProcessMovement` doesn't yet gate on both sentinels.

- [ ] **Step 3: Update `ProcessMovement`**

In `internal/mapper/mapper.go`, near the start of `ProcessMovement` (after the `IsAutoMapping` check, before parsing direction):

```go
func (e *Engine) ProcessMovement(input string) (processed bool, roomName string, err error) {
	if !e.IsAutoMapping() {
		return false, "", nil
	}
	if !e.HasMXPSeen() || !e.HasBlockStartSeen() {
		return false, "", nil
	}
	// ...rest unchanged...
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mapper/ -run TestProcessMovement -v -race`
Expected: PASS for new tests; existing pending-movement tests must call BOTH `e.MarkMXPSeen()` and `e.MarkBlockStartSeen()` after `e.StartAutoMapping()`.

- [ ] **Step 5: Update existing tests that call `ProcessMovement`**

Run the full suite: `go test ./internal/mapper/ -v`. For every failure that is a previously-green test now failing because `ProcessMovement` returns `processed=false`, add BOTH:

```go
e.MarkMXPSeen()
e.MarkBlockStartSeen()
```

immediately after the `e.StartAutoMapping()` line in that test.

Run again until all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/mapper/mapper.go internal/mapper/mapper_test.go
git commit -m "feat(mapper): ProcessMovement gates on sawMXP + sawBlockStart"
```

---

### Task 16: `/map start` warns when MXP is not active

App-level. The warning is a status message; the mapper still flips
`autoMapping=true` so a later reconnect with MXP will pick up.

The MXP-active check uses `HasMXPSeen()` rather than walking
`ActiveProtocols()` because `ActiveProtocols` returns true under
`AlwaysActive` even when negotiation hasn't completed (or has been
declined). `HasMXPSeen` is set exclusively by an actual tag arrival —
the only reliable proof that MXP works end-to-end.

Caveat: if the user runs `/map start` immediately after connect, no
tag may have arrived yet (the server typically sends `<expire>` only
on the first room render). In that case the warning fires even though
MXP will become active a moment later. We accept this — the warning is
informational, not blocking, and the user can simply re-issue
`/map start` after their first room arrives, or just trust that the
gate inside `ProcessMovement` will silently start auto-mapping the
moment both sentinels are set.

**Files:**
- Modify: `internal/app/app.go` (`MapStart` dispatch handler)

- [ ] **Step 1: Locate the dispatch**

Find the `case *command.MapStart:` block (around `app.go:801`).

- [ ] **Step 2: Insert MXP-active check**

Replace the existing block with:

```go
	case *command.MapStart:
		if c.Query != "" {
			if err := s.mapEngine.Goto(c.Query); err != nil {
				s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Goto Error: %v\n", err)})
			}
		}
		s.mapEngine.StartAutoMapping()
		// "MXP active" = at least one MXP tag observed on this connection.
		// Don't trust ActiveProtocols (returns true under AlwaysActive
		// even pre-negotiation).
		if !s.mapEngine.HasMXPSeen() {
			s.program.Send(ui.StatusMsg{Message: "[Map] Auto-mapping enabled, but no MXP tags have been observed yet. /map dig still works manually; auto-rooms will appear once the server emits MXP-wrapped room data.\n"})
		} else {
			r := s.mapEngine.GetCurrent()
			if r != nil {
				s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Auto-mapping started at: %s\n", r.Name)})
			} else {
				s.program.Send(ui.StatusMsg{Message: "Auto-mapping started.\n"})
			}
		}
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add internal/app/app.go
git commit -m "feat(app): /map start warns when MXP is not active"
```

**CHECKPOINT E complete.**

---

## Checkpoint F — App wiring + JSON persistence

### Task 17: Resolve and apply `MUDProfile` on connect

**Files:**
- Modify: `internal/app/app.go`

- [ ] **Step 1: Add MUDProfile field to gotinData and Session**

In `internal/app/app.go`, update `gotinData`:

```go
type gotinData struct {
	Aliases        map[string]string                `json:"aliases,omitempty"`
	Triggers       []config.TriggerConfig           `json:"triggers,omitempty"`
	Connections    map[string]input.ConnectionAlias `json:"connections,omitempty"`
	MappingOptions *mapper.MappingOptions           `json:"mapping_options,omitempty"`
	MUDProfiles    map[string]mapper.MUDProfile     `json:"mud_profiles,omitempty"`
}
```

Add `mudProfiles map[string]mapper.MUDProfile` to `Session`:

```go
type Session struct {
	// ...existing fields...
	mudProfiles map[string]mapper.MUDProfile
}
```

- [ ] **Step 2: Resolve profile during connect**

In `connect()`, after `installProtocols(c, …)` but before the GMCP/MXP callback wiring (around `app.go:382`):

```go
	// Resolve MUD profile: per-host override merged into the in-code default.
	base := mapper.DefaultT2TMUDProfile()
	resolved := base
	if s.mudProfiles != nil {
		if over, ok := lookupProfile(s.mudProfiles, h); ok {
			resolved = mapper.MergeProfile(base, over)
		}
	}
	s.mapEngine.SetMUDProfile(resolved)
	s.mapEngine.ResetBlockStart()
```

Add the helper near the bottom of the file (above `historyFilePath`):

```go
// lookupProfile finds a MUDProfile by host (case-insensitive).
func lookupProfile(m map[string]mapper.MUDProfile, host string) (mapper.MUDProfile, bool) {
	for k, v := range m {
		if strings.EqualFold(k, host) {
			return v, true
		}
	}
	return mapper.MUDProfile{}, false
}
```

- [ ] **Step 3: Load and persist `mud_profiles`**

In `loadGotinData` and `loadGotinDataFile` (auto and manual /load), add:

```go
	if td.MUDProfiles != nil {
		s.mudProfiles = td.MUDProfiles
	}
```

In `saveGotinData` and `persistGotinData`, add to the `gotinData` initialiser:

```go
		MUDProfiles: s.mudProfiles,
```

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go
git commit -m "feat(app): resolve MUD profile on connect; persist mud_profiles"
```

---

### Task 18: Wire `RoomBlockBuffer` and MXP tag callback

**Files:**
- Modify: `internal/app/app.go`

- [ ] **Step 1: Add buffer field to Session**

```go
type Session struct {
	// ...existing fields...
	roomBuf *mapper.RoomBlockBuffer
}
```

- [ ] **Step 2: Construct on connect when MXP is installed**

In `connect()`, after the block from Task 17 (and before the MXP `SetRoomNameCallback` block), add:

```go
	// Build the room-block buffer — only effective when MXP is installed.
	if findMXP(c) != nil {
		cp, err := resolved.Compile()
		if err != nil {
			s.trySendUI(ui.StatusMsg{Message: fmt.Sprintf("[Map] profile compile error: %v\n", err)})
		} else {
			s.roomBuf = mapper.NewRoomBlockBuffer(cp, s.onRoomBlock)
		}
	} else {
		s.roomBuf = nil
	}
```

- [ ] **Step 3: Install the segment Sink**

Update the MXP-callback block (around `app.go:388`). Use a small adapter
type so MXP-seen and block-start tracking happen independently of the
buffer's existence:

```go
	if m := findMXP(c); m != nil {
		profile := s.mapEngine.GetMUDProfile()
		blockStartTag := profile.BlockStartTag
		var inner mxp.Sink
		if s.roomBuf != nil {
			inner = s.roomBuf
		} else {
			inner = noopSink{}
		}
		m.SetSink(sentinelSink{
			inner:         inner,
			engine:        s.mapEngine,
			blockStartTag: blockStartTag,
		})
		m.SetRoomNameCallback(func(name string) {
			if name == "" {
				return
			}
			s.mapEngine.SetIncomingRoomName(name)
			s.trySendUI(ui.RoomNameMsg{Name: name})
		})
	}
```

Add the adapter types near the bottom of `app.go` (above
`historyFilePath`):

```go
// sentinelSink wraps a mapper Sink so every observed MXP tag updates the
// engine's MXP-seen and (when the tag matches the configured
// block-start tag) block-start sentinels, regardless of whether a
// buffer is currently installed.
type sentinelSink struct {
	inner         mxp.Sink
	engine        *mapper.Engine
	blockStartTag string
}

func (s sentinelSink) OnText(t string) { s.inner.OnText(t) }

func (s sentinelSink) OnTag(name, body string) {
	s.engine.MarkMXPSeen()
	if name == s.blockStartTag {
		s.engine.MarkBlockStartSeen()
	}
	s.inner.OnTag(name, body)
}

// noopSink swallows sink calls; used when the buffer isn't constructed
// (profile-compile failure or MXP not installed). Sentinel tracking
// still wraps it so HasMXPSeen reflects reality.
type noopSink struct{}

func (noopSink) OnText(string)     {}
func (noopSink) OnTag(string, string) {}
```

`mapper.RoomBlockBuffer` must satisfy `mxp.Sink`. Confirm its method set
matches: `OnText(string)` and `OnTag(name, body string)` — already the
case from Tasks 7 and 8.

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: error — `s.onRoomBlock` undefined. We'll add it next.

- [ ] **Step 5: Add `onRoomBlock`**

Below `onGMCPRoomInfo`:

```go
func (s *Session) onRoomBlock(b mapper.RoomBlock) {
	if b.Name != "" {
		s.trySendUI(ui.RoomNameMsg{Name: b.Name})
	}
	processed, roomName, loopDetected, err := s.mapEngine.HandleRoomBlock(b)
	if !processed {
		return
	}
	if err != nil {
		s.trySendUI(ui.StatusMsg{Message: fmt.Sprintf("[Map] Error: %v\n", err)})
	} else if loopDetected {
		s.trySendUI(ui.StatusMsg{Message: fmt.Sprintf("[Map] Loop detected! Linked to existing room: %s\n", roomName)})
	} else {
		s.trySendUI(ui.StatusMsg{Message: fmt.Sprintf("[Map] New room created: %s\n", roomName)})
	}
}
```

- [ ] **Step 6: Build**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add internal/app/app.go
git commit -m "feat(app): wire RoomBlockBuffer with MXP tag callback"
```

---

### Task 19: Retire `ProcessRoomData`/`HandleGMCPRoomInfo` from production paths

The buffer no longer needs explicit text pushes — the MXP `Sink` (Task 18)
already routes interleaved text and tag segments into the buffer at the
correct positions. `onData` just removes the Phase 1 calls.

**Files:**
- Modify: `internal/app/app.go`

- [ ] **Step 1: Update `onData`**

Replace the existing block (around `app.go:496–513`) that calls `ProcessRoomData`:

```go
func (s *Session) onData(c *network.Client, lb *logic.LineBuffer, proc *logic.Processor, data string) {
	if m := findMXP(c); m != nil {
		// Filter() drives the installed Sink (Task 18) for interleaved
		// text + tag segments. Its return value still carries the full
		// stripped text for the UI viewport.
		data = m.Filter(data)
	}
	s.trySendUI(ui.NetworkDataMsg{Data: data})

	logicLines := lb.Feed([]byte(data))
	for _, line := range logicLines {
		proc.ProcessLine(line)
	}
}
```

- [ ] **Step 2: Update `onGMCPRoomInfo`**

Remove the `mapEngine.HandleGMCPRoomInfo(...)` call. Keep the protolog event emission and the `ui.RoomNameMsg`. Replace the function body (currently around `app.go:537–567`) with:

```go
func (s *Session) onGMCPRoomInfo(pkg string, payload []byte) {
	if pkg != "Room.Info" {
		return
	}
	room, err := gmcp.ParseRoomInfo(payload)
	if err != nil {
		return
	}
	if s.protoLog != nil && s.protoLog.Enabled() {
		s.protoLog.Log(protolog.Entry{
			Source: "gmcp", Dir: "rx", Event: "room_info",
			UTF8:   protolog.EncodeUTF8(payload),
			Hex:    protolog.EncodeHex(payload),
			Parsed: map[string]any{
				"vnum":     room.Vnum,
				"name":     room.Name,
				"area":     room.Area,
				"exits":    room.Exits,
				"desc_len": len(room.Description),
			},
		})
	}
	if room.Name != "" {
		s.trySendUI(ui.RoomNameMsg{Name: room.Name})
	}
}
```

- [ ] **Step 3: Reset block buffer on disconnect**

In `onDisconnect` (currently around `app.go:518`), after the existing UI clears, add:

```go
	s.mapEngine.ResetBlockStart()
	if s.roomBuf != nil {
		s.roomBuf.Reset()
	}
```

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go
git commit -m "feat(app): route data to RoomBlockBuffer; retire Phase 1 paths"
```

---

### Task 20: `gotin.json` example entry

**Files:**
- Modify: `gotin.json`

- [ ] **Step 1: Add an example `mud_profiles` block**

Edit `gotin.json` so it matches:

```json
{
  "aliases": {
    "l": "look ; search; rummage through room; forage"
  },
  "connections": {
    "t2t": {
      "host": "t2tmud.org",
      "port": 9999,
      "auto": true,
      "protocols": {
        "GMCP": true,
        "MXP": true
      }
    }
  },
  "mud_profiles": {
    "t2tmud.org": {}
  }
}
```

The empty object is intentional: it documents that the host is recognised
but applies no overrides — defaults from `DefaultT2TMUDProfile()` are
used. Users may add field overrides as needed.

- [ ] **Step 2: Commit**

```bash
git add gotin.json
git commit -m "config: example mud_profiles entry for t2tmud.org"
```

**CHECKPOINT F complete.**

---

## Checkpoint G — Fixtures, integration test, manual verification

### Task 21: Capture fixture files from `gotin.log`

**Files:**
- Create: `internal/mapper/testdata/t2tmud_paved_street.txt`
- Create: `internal/mapper/testdata/t2tmud_plains_north_ne.txt`
- Create: `internal/mapper/testdata/t2tmud_plains_nw_n.txt`
- Create: `internal/mapper/testdata/t2tmud_plains_nw.txt`
- Create: `internal/mapper/testdata/t2tmud_plains_landlocked.txt`
- Create: `internal/mapper/testdata/t2tmud_plains_white_towers_se.txt`
- Create: `internal/mapper/testdata/t2tmud_white_towers_room.txt`

Each fixture is the decoded UTF-8 of a single MXP-rx chunk captured by
`protolog`. Recover them by running:

- [ ] **Step 1: Recovery script**

```bash
mkdir -p internal/mapper/testdata
grep -E '"source":"mxp","dir":"rx","event":"chunk"' gotin.log \
  | python3 -c '
import json, sys, os, re
out_dir = "internal/mapper/testdata"
chunks = []
for line in sys.stdin:
    e = json.loads(line)
    raw = e["utf8"]
    if raw.startswith("\""):
        raw = raw[1:-1]
    decoded = bytes(raw, "utf-8").decode("unicode_escape")
    chunks.append(decoded)

# Hand-pick by content
def write(path, content):
    with open(path, "w") as f:
        f.write(content)

# Heuristic mapping - adjust indices after inspection.
for c in chunks:
    if "paved street" in c.lower() and "seagull" in c:
        write(os.path.join(out_dir, "t2tmud_paved_street.txt"), c); break
for c in chunks:
    if "Gently rolling plains" in c and "Lune River lies north and northeast" in c:
        write(os.path.join(out_dir, "t2tmud_plains_north_ne.txt"), c); break
for c in chunks:
    if "Lune River lies northwest and north" in c:
        write(os.path.join(out_dir, "t2tmud_plains_nw_n.txt"), c); break
for c in chunks:
    if "Lune River lies northwest." in c:
        write(os.path.join(out_dir, "t2tmud_plains_nw.txt"), c); break
for c in chunks:
    if "Gently rolling plains" in c and "Lune River" not in c and "White Towers" not in c:
        write(os.path.join(out_dir, "t2tmud_plains_landlocked.txt"), c); break
for c in chunks:
    if "Gently rolling plains" in c and "White Towers rise up to the southeast" in c:
        write(os.path.join(out_dir, "t2tmud_plains_white_towers_se.txt"), c); break
for c in chunks:
    if "Soaring hills" in c:
        write(os.path.join(out_dir, "t2tmud_white_towers_room.txt"), c); break
'
ls -la internal/mapper/testdata/
```

- [ ] **Step 2: Verify all 7 files exist and contain `<expire>` plus a prompt**

```bash
for f in internal/mapper/testdata/t2tmud_*.txt; do
  printf "%s: " "$f"
  grep -c "<expire>" "$f"
done
```

Expected: each file reports `1` (one `<expire>` per chunk). If any are missing, hand-edit the script's heuristics, re-run, and re-check.

- [ ] **Step 3: Commit**

```bash
git add internal/mapper/testdata/
git commit -m "test(mapper): t2tmud MXP fixtures from real session"
```

---

### Task 22: Integration test — 5 plains tiles → 5 distinct rooms

**Files:**
- Create: `internal/mapper/integration_test.go`

- [ ] **Step 1: Write the test**

`internal/mapper/integration_test.go`:

```go
package mapper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ayder/gotin/internal/mudproto/mxp"
)

// loadFixture reads a captured MXP chunk from testdata.
func loadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// drive feeds a fixture through a fresh MXP Protocol whose interleaved
// text + tag stream is routed into a RoomBlockBuffer via SetSink.
// Returns the emitted RoomBlocks in order.
func drive(t *testing.T, fixtures []string) []RoomBlock {
	t.Helper()
	cp, err := DefaultT2TMUDProfile().Compile()
	if err != nil {
		t.Fatalf("compile profile: %v", err)
	}
	var emitted []RoomBlock
	buf := NewRoomBlockBuffer(cp, func(rb RoomBlock) { emitted = append(emitted, rb) })

	p := mxp.New()
	p.SetSink(buf)
	for _, name := range fixtures {
		raw := loadFixture(t, name)
		_ = p.Filter(raw) // drives the sink; return value unused here
	}
	return emitted
}

func TestIntegration_PlainsFiveTilesProduceFiveDistinctRooms(t *testing.T) {
	blocks := drive(t, []string{
		"t2tmud_plains_north_ne.txt",
		"t2tmud_plains_nw_n.txt",
		"t2tmud_plains_nw.txt",
		"t2tmud_plains_landlocked.txt",
		"t2tmud_plains_white_towers_se.txt",
	})
	if len(blocks) != 5 {
		t.Fatalf("expected 5 RoomBlocks, got %d", len(blocks))
	}

	hashes := make(map[string]bool)
	for i, b := range blocks {
		h := ComputeStructuralHash(b.Description, b.Exits)
		if hashes[h] {
			t.Errorf("tile %d hash collision with earlier tile: %s", i, h)
		}
		hashes[h] = true
	}
	if len(hashes) != 5 {
		t.Errorf("expected 5 distinct hashes, got %d", len(hashes))
	}
}

func TestIntegration_DescriptionDropsWeatherKeepsSights(t *testing.T) {
	blocks := drive(t, []string{"t2tmud_plains_north_ne.txt"})
	if len(blocks) != 1 {
		t.Fatalf("len(blocks) = %d, want 1", len(blocks))
	}
	d := blocks[0].Description
	if !contains(d, "Gently rolling plains") {
		t.Errorf("lost static description: %q", d)
	}
	if !contains(d, "Lune River") {
		t.Errorf("lost nearby-sight: %q", d)
	}
	if contains(d, "The sky is") {
		t.Errorf("kept weather: %q", d)
	}
	if contains(d, "There is water to") {
		t.Errorf("kept water-exits sentence: %q", d)
	}
	if contains(d, "obvious exits") {
		t.Errorf("kept exits sentence: %q", d)
	}
}

func TestIntegration_WhiteTowersDistinctFromPlains(t *testing.T) {
	plains := drive(t, []string{"t2tmud_plains_white_towers_se.txt"})
	heath := drive(t, []string{"t2tmud_white_towers_room.txt"})
	if len(plains) != 1 || len(heath) != 1 {
		t.Fatalf("len plains=%d heath=%d", len(plains), len(heath))
	}
	hp := ComputeStructuralHash(plains[0].Description, plains[0].Exits)
	hh := ComputeStructuralHash(heath[0].Description, heath[0].Exits)
	if hp == hh {
		t.Errorf("plains and heath collided: %s", hp)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/mapper/ -run TestIntegration -v -race`
Expected: PASS for all three.

If a test fails, the most likely cause is a fixture missing the prompt
suffix needed to close the block — confirm each fixture ends with
`HP:nn EP:nn [WA] > `. If not, append it in the fixture file.

- [ ] **Step 3: Commit**

```bash
git add internal/mapper/integration_test.go
git commit -m "test(mapper): integration replay of t2tmud fixtures"
```

---

### Task 23: Final build, full suite, manual verification

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 2: Full test suite, race-checked**

Run: `go test ./... -race`
Expected: PASS for every package.

- [ ] **Step 3: Manual verification on t2tmud**

Build and run:

```bash
go build -o gotin ./cmd/gotin
rm -f gotin.log
./gotin -debug
```

In-client:

1. `/connect t2tmud.org 9999` (or autoconnect via the t2t alias).
2. Confirm `MXP enabled.` appears in the viewport.
3. `/map create t2t_phase2.json`.
4. `/map start`.
5. Walk a route that traverses the plains (where the bug previously
   collapsed multiple tiles): e.g. east into the wilderness, then
   north / northeast / east through several plains tiles, return.
6. `/map show all` — expect every distinct tile to appear as its own
   room with correct exits.
7. `/quit`.

Verify externally:

```bash
jq '.rooms | length' t2t_phase2.json   # expect a count matching the steps walked
grep -c '"event":"tag"' gotin.log      # confirm protolog still firing
```

- [ ] **Step 4: Manual verification — MXP off path**

```bash
./gotin -debug -host t2tmud.org -port 9999
# After connect, type:
set mxp off
# Then in-client:
/map start
```

Expected: status line says
`[Map] Auto-mapping enabled, but MXP is not active on this connection. /map dig still works manually; auto-rooms will not appear until you reconnect with MXP enabled.`

Walking a direction does **not** create rooms. `/map dig east "test"`
does create a room. `/quit`.

- [ ] **Step 5: Push branch and open PR**

```bash
git push -u origin mapper-resilience
gh pr create --title "Mapper Phase 2: structural MXP path with t2tmud profile" \
  --body "$(cat docs/superpowers/specs/2026-04-26-mapper-resilience-phase2-design.md | head -80)"
```

**CHECKPOINT G complete. Phase 2 ready for review.**

---

## Self-Review

**Spec coverage:**
- ✅ Strict gate (MXP-seen sentinel + block-start sentinel) — Tasks 12, 15, 16, 18.
- ✅ Hard-coded t2tmud profile + JSON override — Tasks 1–3, 17.
- ✅ MXP segment Sink (correct text/tag interleaving) — Tasks 4–5.
- ✅ ANSI-tolerant prompt regex — Tasks 1, 8.
- ✅ RoomBlockBuffer with split-chunk safety — Tasks 6–10.
- ✅ Structural hash with externally visible v2: prefix — Task 11.
- ✅ Engine dual sentinels + HandleRoomBlock — Tasks 12–13.
- ✅ Index rebuild populates both maps unconditionally — Task 14.
- ✅ ProcessRoomData / HandleGMCPRoomInfo retired from production — Task 19.
- ✅ Buffer reset on disconnect — Task 19.
- ✅ `mud_profiles` JSON shape — Tasks 17, 20.
- ✅ Acceptance criteria 1–6 — Task 23.

**Type consistency check:**
- `MUDProfile` and `CompiledProfile` distinguished cleanly; tasks always
  pass the compiled form into `RoomBlockBuffer`.
- `RoomBlock` field names (`Description`, `Exits`, `Presence`, `Vnum`,
  `Name`) match between `blockbuf.go`, `mapper.go HandleRoomBlock`, and
  `app.go onRoomBlock`.
- `Engine.{SetMUDProfile, MarkMXPSeen, MarkBlockStartSeen, ResetBlockStart, HasMXPSeen, HasBlockStartSeen, HandleRoomBlock}`
  match between mapper.go and app.go callers.
- `mxp.Sink` interface (`OnText(string)`, `OnTag(name, body string)`)
  matches `RoomBlockBuffer`'s method set; `sentinelSink` adapter wraps
  the buffer to drive engine sentinels.

**Placeholder scan:** No "TBD" / "TODO" / vague guidance. Every code step
shows complete code; every regex is explicit.

**Out-of-scope guard:** No task touches the heuristic non-MXP fallback,
no plugin loader, no MXP keyword storage, no status-bar `<gauge>` UI.

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-04-26-mapper-resilience-phase2.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — Dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**
