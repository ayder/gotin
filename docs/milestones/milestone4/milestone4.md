Here are the iteration steps for **Milestone 4: User Input & Configuration Manager**.

This milestone focuses on turning the "hardcoded" logic into a flexible, user-configurable application. You are effectively building the "Operating System" for your client.

### Iteration 4.1: The Command Router (The Split)

**Goal:** Intercept user input to decide if it goes to the Server or the Client Logic.
**Why:** Currently, everything you type is sent to the MUD. You need a way to say "Connect to X" or "Quit" without that text being sent to the game world as chat.

* **Step 1:** Define a **Command Prefix** (conventionally `/`).
* **Step 2:** Modify the Input Handler (from Milestone 2):
* *Before:* `socket.send(user_input)`
* *After:* Check `if user_input.startswith("/")`:
* **True:** Pass to `handle_local_command(user_input)`
* **False:** Pass to `socket.send(user_input)`




* **Step 3:** Implement `handle_local_command`:
* Parse the string (split by spaces).
* Match the first word (verb) to a function map.
* *Initial verbs:* `/quit`, `/connect <host> <port>`, `/help`.


* **Test:** Type `/help`. It should print help text to the local window *not* the server. Type `say hello`. It should go to the server.

### Iteration 4.2: The Alias Engine

**Goal:** Allow users to define their own shortcuts at runtime.
**Why:** MUDs require repetitive typing. Users need to turn `k` into `kill goblin`.

* **Step 1:** Create a dictionary `aliases = {}`.
* **Step 2:** Add a local command `/alias <key> <value>`.
* Example: `/alias k kill goblin` stores `{"k": "kill goblin"}`.


* **Step 3:** Update the **Command Router** (from 4.1):
* Before sending *any* text to the server, check: `if first_word in aliases`.
* If found, replace the input with the stored value.


* **Step 4:** Support recursion (optional but good): If the expanded alias contains another alias, expand it too (watch out for infinite loops!).
* **Test:** Type `/alias hi say Hello World`. Then type `hi`. The server should receive `say Hello World`.

### Iteration 4.3: Configuration Persistence (Save/Load)

**Goal:** Remember settings after the client is closed.
**Why:** It is annoying to re-type triggers and aliases every time you restart the program.

* **Step 1:** Choose a format (JSON or TOML are best).
* Structure: `{ "aliases": {}, "triggers": [], "last_server": "..." }`.


* **Step 2:** Create `save_config()`:
* Serialize your `aliases` and `triggers` dictionaries to a file (`config.json`).
* Call this function on `/quit`.


* **Step 3:** Create `load_config()`:
* Call this on application startup.
* Read `config.json` and populate the internal dictionaries.


* **Test:** Add an alias. Quit. Restart. Type the alias. It should still work.

### Iteration 4.4: The "Wizard" (First-Run Experience)

**Goal:** Improve usability for new users.
**Why:** A blank black screen is intimidating.

* **Step 1:** Detect if `config.json` is missing (first run).
* **Step 2:** If missing, display a welcome message in the TUI:
* "Welcome to TermMud! Type `/connect <host> <port>` to start."


* **Step 3:** Implement `/connect` logic handles the socket creation dynamically (previously hardcoded in Milestone 1).
* **Test:** Delete your config file. Run the app. Verify the welcome message appears.

---

### Summary Checklist for Milestone 4

| Iteration | Success Criteria |
| --- | --- |
| **4.1 Router** | Commands starting with `/` execute locally; others go to the net. |
| **4.2 Alias** | `/alias` creates a shortcut. Typing the shortcut sends the full text. |
| **4.3 Persistence** | Aliases and settings survive a restart of the application. |
| **4.4 Wizard** | Users can launch the app and connect to any server without changing code. |
