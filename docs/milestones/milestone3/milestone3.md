Here are the specific iteration steps for **Milestone 3: The "Parser" Loop (Middleware)**.

This phase is critical because it changes your client from a "dumb display" into an intelligent agent. You are moving from simply *displaying* data to *understanding* it.

### Iteration 3.1: The "Interceptor" Function (Refactor)

**Goal:** Re-route the data flow so text doesn't go straight from Socket → Screen.
**Why:** We need a choke point to inspect data. Currently, your code likely reads the socket and immediately prints. We need to insert a processing step.

* **Step 1:** Create a new function called `process_incoming_data(raw_data)`.
* **Step 2:** Modify your Network Loop:
* *Old:* `data = socket.read(); ui.display(data)`
* *New:* `data = socket.read(); process_incoming_data(data)`


* **Step 3:** Inside `process_incoming_data`, simply call `ui.display(raw_data)` for now.
* **Test:** Run the client. It should look exactly the same as before. If no text appears, the "pipe" is broken.

### Iteration 3.2: The Line Buffer (De-fragmentation)

**Goal:** Convert a stream of random bytes into complete, coherent lines of text.
**Why:** TCP packets are fragmented. You might receive "The gob" in packet A and "lin hits you!" in packet B. If you try to regex "The gob", it won't match "The goblin".

* **Step 1:** Create a persistent string variable `buffer = ""`.
* **Step 2:** Inside `process_incoming_data(new_data)`:
* Append `new_data` to `buffer`.


* **Step 3:** Check `buffer` for newline characters (`\n` or `\r\n`).
* **Step 4:** Split the buffer:
* Extract all **complete lines**.
* Pass each complete line to a new function `handle_line(line_text)`.
* **Crucial:** Keep the remainder (incomplete text after the last newline) in the `buffer`.


* **Test:** Connect to a MUD. The output should still look normal. The change is invisible to the user but vital for the next step.

### Iteration 3.3: The Regex Engine (The "Brain")

**Goal:** Make the client "read" the text and react to it.
**Why:** This is the foundation of triggers and automation.

* **Step 1:** Define a simple trigger list structure.
* `triggers = [{ pattern: "Hi", response: "Hello!" }]`


* **Step 2:** Inside `handle_line(line_text)`:
* Loop through your `triggers` list.
* Run a Regex check: `if match(trigger.pattern, line_text):`


* **Step 3:** If a match is found:
* Send `trigger.response` directly to the Network Socket.
* (Optional) Print a debug message to the UI: `[Auto-Replied: Hello!]`


* **Test:** Connect to a local server (or have a friend message you inside the MUD). When the specific word appears, your client should instantly fire the response.

### Iteration 3.4: The ANSI Stripper (The "Janitor")

**Goal:** Ensure triggers work even on colored text.
**Why:** The server sends `\x1b[31mHi\x1b[0m`. Your regex looking for `^Hi` will fail because the string actually starts with `\x1b`.

* **Step 1:** Create a utility function `strip_ansi(text)` using a standard regex to remove color codes.
* **Step 2:** Update `handle_line(line_text)`:
* Create a clean copy: `clean_text = strip_ansi(line_text)`.
* Run your Trigger Regex against `clean_text`.
* **Important:** Still send the *original* `line_text` (with colors) to the UI so the user sees the colors.


* **Test:** Set a trigger for a word that is usually colored (like "Health" or "Damage"). Ensure it fires even when the text is red or bold.

---

### Summary Checklist for Milestone 3

| Iteration | Success Criteria |
| --- | --- |
| **3.1 Interceptor** | Code is refactored. Text flows through a handler function before reaching the UI. |
| **3.2 Line Buffer** | Incoming packet fragments are stitched together. Logic operates on full sentences. |
| **3.3 Regex Trigger** | Client detects specific words and automatically sends a command back. |
| **3.4 ANSI Strip** | Triggers work reliably even if the target text is colored or bolded by the server. |
