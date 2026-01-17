package input

import (
	"strconv"
	"strings"
)

// CommandPrefix is the prefix used to identify local commands.
const CommandPrefix = "/"

// CommandResult represents the result of processing user input.
type CommandResult struct {
	// IsLocal indicates if the command was a local command (started with /).
	IsLocal bool
	// Handled indicates if the local command was recognized and processed.
	Handled bool
	// Response is the text to display to the user (for local commands).
	Response string
	// Action is a special action to perform (e.g., "quit", "connect").
	Action string
	// ActionArgs contains arguments for the action (e.g., host and port for connect).
	ActionArgs map[string]string
	// ServerText is the (possibly alias-expanded) text to send to the server.
	ServerText string
}

// Handler processes user input and routes it to local commands or server.
type Handler struct {
	// commands maps command verbs to their handler functions.
	commands map[string]func(args []string) CommandResult
	// aliases manages user-defined command aliases.
	aliases *AliasManager
}

// NewHandler creates a new command handler with built-in commands.
func NewHandler() *Handler {
	h := &Handler{
		commands: make(map[string]func(args []string) CommandResult),
		aliases:  NewAliasManager(),
	}

	// Register built-in commands
	h.commands["quit"] = h.cmdQuit
	h.commands["q"] = h.cmdQuit // alias
	h.commands["connect"] = h.cmdConnect
	h.commands["help"] = h.cmdHelp
	h.commands["alias"] = h.cmdAlias
	h.commands["unalias"] = h.cmdUnalias
	h.commands["aliases"] = h.cmdAliases

	return h
}

// HandleInput processes user input and determines if it's a local or server command.
// Returns a CommandResult indicating how to handle the input.
func (h *Handler) HandleInput(text string) CommandResult {
	text = strings.TrimSpace(text)
	if text == "" {
		return CommandResult{IsLocal: false}
	}

	// Check if it's a local command (starts with /)
	if strings.HasPrefix(text, CommandPrefix) {
		// Parse the local command
		return h.processLocalCommand(text)
	}

	// Not a local command - expand aliases before sending to server
	expanded := h.aliases.Expand(text)
	return CommandResult{
		IsLocal:    false,
		ServerText: expanded,
	}
}

// processLocalCommand parses and executes a local command.
func (h *Handler) processLocalCommand(text string) CommandResult {
	// Remove the prefix and split into parts
	cmdText := strings.TrimPrefix(text, CommandPrefix)
	parts := strings.Fields(cmdText)

	if len(parts) == 0 {
		return CommandResult{
			IsLocal:  true,
			Handled:  false,
			Response: "Unknown command. Type /help for available commands.",
		}
	}

	verb := strings.ToLower(parts[0])
	args := parts[1:]

	// Look up the command handler
	if cmdFunc, ok := h.commands[verb]; ok {
		return cmdFunc(args)
	}

	// Unknown command
	return CommandResult{
		IsLocal:  true,
		Handled:  false,
		Response: "Unknown command: /" + verb + ". Type /help for available commands.",
	}
}

// cmdQuit handles the /quit command.
func (h *Handler) cmdQuit(args []string) CommandResult {
	return CommandResult{
		IsLocal:  true,
		Handled:  true,
		Response: "Goodbye!",
		Action:   "quit",
	}
}

// cmdConnect handles the /connect <host> <port> command.
func (h *Handler) cmdConnect(args []string) CommandResult {
	if len(args) < 2 {
		return CommandResult{
			IsLocal:  true,
			Handled:  false,
			Response: "Usage: /connect <host> <port>",
		}
	}

	host := args[0]
	portStr := args[1]

	// Validate port
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return CommandResult{
			IsLocal:  true,
			Handled:  false,
			Response: "Invalid port: " + portStr + ". Port must be a number between 1 and 65535.",
		}
	}

	return CommandResult{
		IsLocal:  true,
		Handled:  true,
		Response: "Connecting to " + host + ":" + portStr + "...",
		Action:   "connect",
		ActionArgs: map[string]string{
			"host": host,
			"port": portStr,
		},
	}
}

// cmdHelp handles the /help command.
func (h *Handler) cmdHelp(args []string) CommandResult {
	helpText := `Available commands:
  /help                  - Show this help message
  /connect <host> <port> - Connect to a MUD server
  /quit or /q            - Exit the application
  /alias <key> <value>   - Create an alias (e.g., /alias k kill)
  /unalias <key>         - Remove an alias
  /aliases               - List all aliases

All other input is sent to the connected server.`

	return CommandResult{
		IsLocal:  true,
		Handled:  true,
		Response: helpText,
	}
}

// cmdAlias handles the /alias command.
func (h *Handler) cmdAlias(args []string) CommandResult {
	if len(args) < 2 {
		return CommandResult{
			IsLocal:  true,
			Handled:  false,
			Response: "Usage: /alias <key> <value>\nExample: /alias k kill",
		}
	}

	key := args[0]
	// Join remaining args as the value (allows multi-word aliases)
	value := strings.Join(args[1:], " ")

	h.aliases.Set(key, value)

	return CommandResult{
		IsLocal:  true,
		Handled:  true,
		Response: "Alias set: " + key + " -> " + value,
	}
}

// cmdUnalias handles the /unalias command.
func (h *Handler) cmdUnalias(args []string) CommandResult {
	if len(args) < 1 {
		return CommandResult{
			IsLocal:  true,
			Handled:  false,
			Response: "Usage: /unalias <key>",
		}
	}

	key := args[0]
	if h.aliases.Delete(key) {
		return CommandResult{
			IsLocal:  true,
			Handled:  true,
			Response: "Alias removed: " + key,
		}
	}

	return CommandResult{
		IsLocal:  true,
		Handled:  false,
		Response: "Alias not found: " + key,
	}
}

// cmdAliases handles the /aliases command to list all aliases.
func (h *Handler) cmdAliases(args []string) CommandResult {
	aliases := h.aliases.List()

	if len(aliases) == 0 {
		return CommandResult{
			IsLocal:  true,
			Handled:  true,
			Response: "No aliases defined. Use /alias <key> <value> to create one.",
		}
	}

	var sb strings.Builder
	sb.WriteString("Defined aliases:\n")
	for key, value := range aliases {
		sb.WriteString("  " + key + " -> " + value + "\n")
	}

	return CommandResult{
		IsLocal:  true,
		Handled:  true,
		Response: strings.TrimSuffix(sb.String(), "\n"),
	}
}

// GetAliases returns all defined aliases for persistence.
func (h *Handler) GetAliases() map[string]string {
	return h.aliases.List()
}

// SetAliases loads aliases from a map (used for loading config).
func (h *Handler) SetAliases(aliases map[string]string) {
	for key, value := range aliases {
		h.aliases.Set(key, value)
	}
}
