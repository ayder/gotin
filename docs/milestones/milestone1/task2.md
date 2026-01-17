# Task 2: The Decoder (The Translator)

## Goal
Convert raw bytes into readable human text, handling encoding errors.

## Steps
1.  **Add Decoder to Client Struct**
    -   File: `internal/network/telnet.go`
    -   Add `decoder *encoding.Decoder` to `Client` struct (use `golang.org/x/text/encoding/charmap` for ISO-8859-1 or `unicode/utf8` logic).
    -   *Note*: MUDs often use ISO-8859-1. Suggest initializing with a fallback strategy.

2.  **Implement Decode Function**
    -   File: `internal/network/decoder.go` (or in `telnet.go`)
    -   Function: `Decode(bytes []byte) string`
    -   Implement logic to convert bytes to string, replacing invalid characters with `?` or `unicode.ReplacementChar`.

3.  **Update Read Loop**
    -   File: `internal/network/telnet.go`
    -   In `ReadLoop`, pass read bytes through `Decode()`.
    -   Print the resulting string instead of raw bytes.

## Verification
-   Run `go run cmd/termud/main.go`
-   Verify output is readable text, though ANSI codes and IAC sequences (`ÿ`) may still be present.
