### Iteration 2.1: The Static Skeleton (Layout Only)

**Goal:** distinct visual areas on the screen. No typing, no logic. Just boxes.
**Why:** TUI layouts are fragile. If you don't get the resizing and borders right first, adding text later will break everything.

* **Step 1:** Create the main application entry point.
* **Step 2:** Define two "Container" widgets: `OutputPane` and `InputPane`.
* **Step 3:** Apply constraints:
* `InputPane`: Dock to **Bottom**, Height = 1 (or 3 if using borders).
* `OutputPane`: Height = **Fill** (100% of remaining space).


* **Step 4:** Add borders (e.g., green for output, blue for input) so you can visually verify the split.
* **Test:** Run the app. Resize your terminal window. Does the bottom bar stay stuck to the bottom? Does the top bar stretch?

### Iteration 2.2: The Echo Loop (Wiring)

**Goal:** You can type, hit Enter, and "something" happens.
**Why:** This proves you have control over the **Event Loop**.

* **Step 1:** Focus the cursor on the `InputPane` by default.
* **Step 2:** Bind the `Enter` key event.
* **Step 3:** On `Enter`:
* Capture the text value.
* **Clear** the input box (essential for UX).
* **Append** the text to the `OutputPane`.


* **Test:** Type "Hello". Hit Enter. The input box should clear, and "Hello" should appear in the top box.

### Iteration 2.3: The Buffer & Scroll (The "MUD" Behavior)

**Goal:** Handle too much text. MUDs spam text; standard labels cannot handle this.
**Why:** If you just "append text," your memory will explode, or text will disappear off the top without a way to see it.

* **Step 1:** Implement a "Ring Buffer" or use a specialized widget (like `RichLog` in Python or `viewport` in Go).
* **Step 2:** Configure **Auto-Scroll**: When new text is added, force the view to the bottom.
* **Step 3:** Implement **Manual Scroll**:
* Bind `PageUp` / `PageDown`.
* **Logic:** If the user presses `PageUp`, *disable* auto-scroll. If they hit the bottom again, *re-enable* auto-scroll.


* **Test:** Copy-paste a Lorem Ipsum paragraph into your input 20 times. Ensure the old text pushes up and you can scroll back to read it.

### Iteration 2.4: Command History (Polish)

**Goal:** Pressing "Up Arrow" recalls the last command.
**Why:** Critical for MUDs (repeatedly typing `kill goblin` is annoying).

* **Step 1:** Create a list/slice variable: `history = []`.
* **Step 2:** On `Enter`, append the command to `history`.
* **Step 3:** Bind `ArrowUp` / `ArrowDown` on the Input widget.
* **Step 4:** Implement an index pointer.
* `ArrowUp`: `index--`, replace Input text with `history[index]`.
* `ArrowDown`: `index++`.


* **Test:** Type `look`, `inventory`, `score`. Press Up Arrow 3 times. You should see `look` again.

---

### Summary Checklist

| Iteration | Success Criteria |
| --- | --- |
| **2.1 Skeleton** | I see two boxes. Resizing the window adjusts the boxes perfectly. |
| **2.2 Echo** | I type "test", hit Enter, input clears, "test" appears above. |
| **2.3 Scroll** | I can flood the screen with text and scroll up to read history. |
| **2.4 History** | I can use Up/Down arrows to cycle through my previous commands. |
