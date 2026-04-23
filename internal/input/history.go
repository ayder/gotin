package input

// History stores and manages command history for the input.
type History struct {
	commands []string
	index    int // -1 means not browsing history, 0+ means viewing history[index]
}

// NewHistory creates a new History instance.
func NewHistory() *History {
	return &History{
		commands: make([]string, 0),
		index:    -1,
	}
}

// Add appends a command to the history and resets the browsing index.
// Empty commands are not added.
func (h *History) Add(cmd string) {
	if cmd == "" {
		return
	}
	// Avoid adding duplicate consecutive commands
	if len(h.commands) > 0 && h.commands[len(h.commands)-1] == cmd {
		h.index = -1
		return
	}
	h.commands = append(h.commands, cmd)
	h.index = -1
}

// Previous returns the previous command in history (going back in time).
// Returns the command and true if available, empty string and false otherwise.
func (h *History) Previous() (string, bool) {
	if len(h.commands) == 0 {
		return "", false
	}

	if h.index == -1 {
		// Start browsing from the most recent command
		h.index = len(h.commands) - 1
	} else if h.index > 0 {
		// Move further back in history
		h.index--
	}
	// If already at oldest command (index 0), stay there

	return h.commands[h.index], true
}

// Next returns the next command in history (going forward in time).
// Returns the command and true if still in history, empty string and false if past the end.
func (h *History) Next() (string, bool) {
	if len(h.commands) == 0 || h.index == -1 {
		return "", false
	}

	if h.index < len(h.commands)-1 {
		h.index++
		return h.commands[h.index], true
	}

	// Past the most recent command - reset to normal input mode
	h.index = -1
	return "", false
}

// Reset stops browsing history and returns to normal input mode.
func (h *History) Reset() {
	h.index = -1
}

// Len returns the number of commands in history.
func (h *History) Len() int {
	return len(h.commands)
}

// Commands returns a copy of the stored commands.
func (h *History) Commands() []string {
	result := make([]string, len(h.commands))
	copy(result, h.commands)
	return result
}

// SetCommands replaces the stored commands.
func (h *History) SetCommands(commands []string) {
	h.commands = make([]string, 0, len(commands))
	for _, cmd := range commands {
		if cmd != "" {
			h.commands = append(h.commands, cmd)
		}
	}
	h.index = -1
}
