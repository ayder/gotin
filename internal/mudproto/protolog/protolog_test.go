package protolog

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestEntryJSONShape(t *testing.T) {
	e := Entry{
		TS:     "2026-04-26T12:00:00.000000000Z",
		Source: "mxp",
		Dir:    "rx",
		Event:  "tag",
		UTF8:   `<ROOMNAME>X</ROOMNAME>`,
		Hex:    "3c524f4f4e414d453e583c2f524f4f4d4e414d453e",
		Parsed: map[string]any{"name": "ROOMNAME"},
	}
	blob, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(blob, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, k := range []string{"ts", "source", "dir", "event", "utf8", "hex", "parsed"} {
		if _, ok := got[k]; !ok {
			t.Errorf("entry missing key %q in %s", k, string(blob))
		}
	}
}

func TestJSONLinesLogger_WritesLinePerEntry(t *testing.T) {
	var buf bytes.Buffer
	lg := NewJSONLinesLogger(&buf, func() bool { return true })
	lg.Log(Entry{Source: "mxp", Dir: "rx", Event: "chunk", UTF8: "x", Hex: "78"})
	lg.Log(Entry{Source: "gmcp", Dir: "rx", Event: "chunk", UTF8: "y", Hex: "79"})

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), buf.String())
	}
	for _, line := range lines {
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Errorf("Unmarshal %q: %v", line, err)
		}
		if e.TS == "" {
			t.Errorf("entry has empty TS: %q", line)
		}
	}
}

func TestJSONLinesLogger_DisabledIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	lg := NewJSONLinesLogger(&buf, func() bool { return false })
	lg.Log(Entry{Source: "mxp", Dir: "rx", Event: "chunk", UTF8: "x"})
	if buf.Len() != 0 {
		t.Errorf("disabled logger wrote bytes: %q", buf.String())
	}
}

func TestJSONLinesLogger_Concurrent(t *testing.T) {
	var buf bytes.Buffer
	lg := NewJSONLinesLogger(&buf, func() bool { return true })
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lg.Log(Entry{Source: "raw", Dir: "rx", Event: "chunk", UTF8: "x", Hex: "78"})
		}()
	}
	wg.Wait()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 100 {
		t.Fatalf("got %d lines, want 100", len(lines))
	}
	for _, line := range lines {
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Errorf("malformed line %q: %v", line, err)
		}
	}
}

func TestHexUTF8Reversibility(t *testing.T) {
	raw := []byte("<\x1b[1z>X\n")
	h := EncodeHex(raw)
	dec, err := hex.DecodeString(h)
	if err != nil {
		t.Fatalf("hex decode: %v", err)
	}
	if !bytes.Equal(dec, raw) {
		t.Errorf("hex round-trip mismatch")
	}
	if s := EncodeUTF8(raw); s != `"<\x1b[1z>X\n"` {
		t.Errorf("EncodeUTF8 = %q, want %q", s, `"<\x1b[1z>X\n"`)
	}
}
