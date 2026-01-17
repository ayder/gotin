# Product Requirements Document (PRD)

**Project Name:** TermMud (Working Title)
**Type:** Terminal-Based MUD Client
**Interface:** TUI (Text User Interface) / CLI

## 1. Vision

To build a high-performance, modular, and scriptable MUD client that runs entirely in the terminal. It bridges the gap between raw `telnet` and heavy GUI clients (like Mudlet), providing power users with a keyboard-driven, customizable experience capable of handling modern MUD protocols.

## 2. Core Design Principles

These principles will guide every architectural decision.

1. **Asynchrony by Default:** The network stream, the user input, and the rendering loop are independent. A blocked network socket must never freeze the UI; a heavy UI render must never block a trigger from firing.
2. **Strict Separation of Concerns (SoC):**
* The **Network Layer** does not know about the UI (it only emits data).
* The **UI Layer** does not know about the network (it only renders state).
* The **Logic Layer** acts as the bridge.


3. **Stream-to-Line Normalization:** While Telnet is a stream of bytes, MUD logic (triggers) operates on *lines*. The system must normalize incoming streams into coherent lines before processing logic.
4. **Keyboard-Centric Efficiency:** Every function (scrolling, connection management, split resizing) must be accessible via keyboard shortcuts.
5. **Data-Driven Configuration:** Layouts, aliases, and triggers should be defined in external configuration files (JSON/TOML/Lua), not hardcoded.

---

## 3. Functional Requirements

### 3.1. Phase I: The Telnet Engine (Network Layer)

**Goal:** Robust communication with MUD servers.

* **FR-NET-01 (Connection):** Support TCP connections via hostname and port.
* **FR-NET-02 (Protocol Stripping):** Automatically detect and strip IAC (Interpret As Command) sequences (`0xFF`) from the display stream.
* **FR-NET-03 (Negotiation):** Implement a State Machine to handle `WILL`, `WONT`, `DO`, `DONT` negotiations.
* *Must Support:* TTYPE (Terminal Type), NAWS (Negotiate About Window Size).


* **FR-NET-04 (Keep-alive):** Send periodic dummy packets to prevent server timeouts.
* **FR-NET-05 (Encoding):** Handle ISO-8859-1 and UTF-8 decoding seamlessly.
* **FR-NET-06 (Advanced Protocols - Post-MVP):** Support MCCP (Compression) and GMCP (Out-of-band data for UI elements).

### 3.2. Phase II: The TUI (Presentation Layer)

**Goal:** A responsive, glitch-free terminal interface.

* **FR-UI-01 (Viewport):** A scrollable main window displaying game text. Must support ANSI color codes (foreground, background, bold, underline).
* **FR-UI-02 (Input Widget):** A fixed single-line input bar at the bottom.
* Must support standard line editing (Home, End, Arrow keys, Delete, Backspace).


* **FR-UI-03 (Layout Management):** Ability to split the view (e.g., Main game window, Chat window, Map window).
* **FR-UI-04 (Resizing):** Automatically redraw and reflow text when the terminal window is resized.
* **FR-UI-05 (Status Bar):** A dedicated area for connection status, ping time, and (later) character stats.

### 3.3. Phase III: The Middleware (Logic Layer)

**Goal:** Parsing, triggers, and data routing.

* **FR-LOG-01 (Line Assembly):** Buffer incoming bytes until a `\n` or `\r\n` is detected, then dispatch a "Line Event."
* **FR-LOG-02 (Trigger System):**
* Match incoming lines against Regex patterns.
* Execute actions on match (e.g., Send text back to server, gag line, highlight line, play sound).


* **FR-LOG-03 (Aliases):** Map short user commands (e.g., `k rat`) to complex sequences (e.g., `kill rat; kick rat`).
* **FR-LOG-04 (Variable Storage):** Store session variables (e.g., `target=rat`) accessible by triggers and aliases.

### 3.4. Phase IV: User Input & Management

**Goal:** User interaction and configuration.

* **FR-INP-01 (Command Parsing):** Distinguish between:
* **Local Commands:** Prefixed with user-defined char (default `/`). E.g., `/connect`, `/alias`, `/help`.
* **Server Commands:** Everything else. Sent directly to the Telnet Engine.


* **FR-INP-02 (History):** Persist command history across sessions (accessible via Up/Down arrows).
* **FR-INP-03 (Configuration Persistence):** Save profiles (Server IP, Port, Character Name, Triggers) to disk.

---

## 4. Non-Functional Requirements

* **NFR-01 (Performance):** The client must be able to process and display "spam" (fast scrolling text during combat) at 60fps without flickering or freezing input.
* **NFR-02 (Compatibility):** Must run on Linux, macOS, and Windows (via WSL or PowerShell).
* **NFR-03 (Dependencies):** Minimal system dependencies.

---

## 5. Architecture: The Event Loop

This project will follow an **Event-Driven Architecture**.

| Component | Role | Inputs | Outputs |
| --- | --- | --- | --- |
| **Input Controller** | Captures keystrokes | User Keyboard | `CmdEvent` (Local or Net) |
| **Command Parser** | Routes commands | `CmdEvent` | `NetRequest` or `ConfigAction` |
| **Telnet Engine** | Socket I/O | `NetRequest` | `RawBytes` |
| **Protocol Parser** | Cleans stream | `RawBytes` | `LineEvent`, `GMCPEvent` |
| **Trigger Engine** | Logic processing | `LineEvent` | `ModifiedLine`, `AutoResponse` |
| **TUI Renderer** | Draws to screen | `ModifiedLine`, `UIState` | Terminal Screen |

---

## 6. Implementation Milestones

### Milestone 1: "Hello World"

* **Deliverable:** A script that connects to `towel.blinkenlights.nl` (or a MUD) and prints raw text to `stdout`.
* **Focus:** Socket handling and basic encoding.

### Milestone 2: "The Window"

* **Deliverable:** An TUI with an input box and an output box. Text typed in input appears in output.
* **Focus:** TUI library selection (Textual/BubbleTea/Ratatui) and layout basics.

### Milestone 3: "The Connector"

* **Deliverable:** Connecting the TUI to the Socket. You can type in the TUI, send to server, and see the server response in the window.
* **Focus:** Async event loop integration.

### Milestone 4: " The Logic"

* **Deliverable:** Implementation of `/alias` and basic triggers (highlighting text).
* **Focus:** Regex parsing and state management.

