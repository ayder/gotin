Here are the specific iteration steps for **Milestone 1: The Telnet Engine**.

This is the foundation. If this layer is flaky, the UI will freeze and the triggers will fail. The goal here is not just "connecting," but handling the dirty reality of the Telnet protocol (which mixes control codes with text).

### Iteration 1.1: The Raw Socket (The Pipe)

**Goal:** Establish a TCP connection and prove data is flowing.
**Why:** Before worrying about text or protocols, we need a stable bi-directional byte stream.

* **Step 1:** Use your language's socket library (e.g., Python `asyncio.open_connection`, Go `net.Dial`).
* **Step 2:** Connect to a "dumb" endpoint like `towel.blinkenlights.nl` (port 23) or a known MUD.
* **Step 3:** Implement a simple read loop that prints **Raw Byte Arrays** (not strings) to the console.
* *Expected Output:* `b'Welcome to the MUD\r\n'`


* **Test:** Run the script. If you see byte arrays appearing, the network path is clear.

### Iteration 1.2: The Decoder (The Translator)

**Goal:** Convert raw bytes into readable human text, handling encoding errors gracefully.
**Why:** MUDs are old. Some use UTF-8, some use ISO-8859-1 (Latin-1). If you don't handle decoding errors, your client will crash when a weird character arrives.

* **Step 1:** wrap the byte stream in a Decoder.
* **Step 2:** set the decoding strategy to **"Replace"** (ignore errors) rather than "Strict" (crash on error).
* *Python example:* `bytes.decode('utf-8', errors='replace')`


* **Step 3:** Print the resulting string to `stdout`.
* **Test:** Connect to a MUD. You should see readable text. However, you might see weird symbols like `ÿý` or `[31m` mixed in.

### Iteration 1.3: The IAC Stripper (The Filter)

**Goal:** Remove Telnet negotiation codes ("garbage characters") from the visible text.
**Why:** The server sends invisible commands starting with byte `0xFF` (255), known as **IAC** (Interpret As Command). If you print these, they look like junk (`ÿDO`).

* **Step 1:** Implement a Byte Scanner *before* the decoding step (from 1.2).
* **Step 2:** Scan for `0xFF`.
* **Step 3:** Implement the basic "Eat" logic:
* If you see `0xFF` (IAC) + `0xFD` (DO) + `0x18` (TTYPE), **remove those 3 bytes** from the stream.
* Don't respond yet, just delete them so they don't clutter the screen.


* **Test:** Connect again. The text should look much cleaner. The `ÿ` symbols should be gone.

### Iteration 1.4: The Negotiator ( The Handshake)

**Goal:** Politely reply to the Server's requests.
**Why:** If a server asks "Do you support TTYPE?" (`IAC DO TTYPE`) and you ignore it, the server might pause waiting for an answer or disconnect you.

* **Step 1:** Update the Byte Scanner (from 1.3).
* **Step 2:** When you see `IAC DO <OPTION>`, automatically send back `IAC WONT <OPTION>` (Refusal).
* *Strategy:* For the MVP, it is safer to say "I support nothing" (`WONT`/`DONT`) than to stay silent.


* **Test:** Connect to a sophisticated MUD (like Aardwolf or Alter Aeon). The connection should stabilize immediately because you are actively declining advanced options.

---

### Summary Checklist for Milestone 1

| Iteration | Success Criteria |
| --- | --- |
| **1.1 Raw Socket** | I can connect and see raw data (bytes) scrolling on my console. |
| **1.2 Decoder** | I see readable English text. The app does not crash on strange characters. |
| **1.3 IAC Strip** | The `ÿ` (y-umlaut) characters and random junk are gone from the output. |
| **1.4 Negotiator** | The client stays connected indefinitely and doesn't time out during login. |
