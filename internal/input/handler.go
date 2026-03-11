package input

import (
	"strconv"
	"strings"

	"dmud/internal/pkg/parser"
)

// parseBraceDelimitedArgs parses brace-delimited arguments from a string.
// Input: "{pattern} {response}" or "pattern response" (fallback)
// Returns the extracted parts. If braces are used, content between matching braces is extracted.
// Example: "{^Hello (.*)} {say Hi $1}" -> ["^Hello (.*)", "say Hi $1"]
func parseBraceDelimitedArgs(text string) []string {
	results, _ := parser.ParseBraceArgs(text)
	return results
}

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
	h.commands["trigger"] = h.cmdTrigger
	h.commands["untrigger"] = h.cmdUntrigger
	h.commands["triggers"] = h.cmdTriggers
	h.commands["map"] = h.cmdMap

	return h
}

// HandleInput processes user input and determines if it's a local or server command.
// Returns a slice of CommandResults to support multi-command alias expansions.
func (h *Handler) HandleInput(text string) []CommandResult {
	text = strings.TrimSpace(text)
	if text == "" {
		return []CommandResult{{IsLocal: false}}
	}

	// Check if it's a local command (starts with /)
	if strings.HasPrefix(text, CommandPrefix) {
		// Parse the local command
		return []CommandResult{h.processLocalCommand(text)}
	}

	// Not a local command - expand aliases before sending to server
	// This returns a slice of commands (for multi-command expansions)
	expandedCommands := h.aliases.Expand(text)

	if len(expandedCommands) == 0 {
		return []CommandResult{{IsLocal: false}}
	}

	// Process each expanded command
	var results []CommandResult
	for _, cmd := range expandedCommands {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}

		// Check if expanded command is a local command
		if strings.HasPrefix(cmd, CommandPrefix) {
			results = append(results, h.processLocalCommand(cmd))
		} else {
			results = append(results, CommandResult{
				IsLocal:    false,
				ServerText: cmd,
			})
		}
	}

	if len(results) == 0 {
		return []CommandResult{{IsLocal: false}}
	}

	return results
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

	// For commands that support brace delimiters, pass the raw text after the verb
	switch verb {
	case "trigger", "untrigger", "alias":
		// Find where the verb ends and extract the rest as raw text
		verbEnd := strings.Index(cmdText, verb) + len(verb)
		rawArgs := strings.TrimSpace(cmdText[verbEnd:])
		if cmdFunc, ok := h.commands[verb]; ok {
			// Pass raw text as single arg for brace parsing
			return cmdFunc([]string{rawArgs})
		}
	}

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
  /help                              - Show this help message
  /connect <host> <port>             - Connect to a MUD server
  /quit or /q                        - Exit the application
  /alias {pattern} {expansion}       - Create an alias
    Examples:
      /alias {k $1} {kill $1; skin corpse}
      /alias {setup} {stand; wear all; look}
      /alias {hi $1} {say Hello $1; smile $1}
  /unalias <trigger>                 - Remove an alias by its trigger word
  /aliases                           - List all aliases
  /trigger {pattern} {response}      - Create a trigger
    Example: /trigger {^Greetings (.*)} {say Hello $1}
  /untrigger {pattern}               - Remove a trigger
  /triggers                          - List all triggers

All other input is sent to the connected server.`

	return CommandResult{
		IsLocal:  true,
		Handled:  true,
		Response: helpText,
	}
}

// cmdAlias handles the /alias {pattern} {expansion} command.
// Supports brace delimiters for patterns and expansions with spaces and special characters.
// Example: /alias {k $1} {kill $1; skin corpse}
func (h *Handler) cmdAlias(args []string) CommandResult {
	if len(args) == 0 || args[0] == "" {
		return CommandResult{
			IsLocal:  true,
			Handled:  false,
			Response: "Usage: /alias {pattern} {expansion}\nExamples:\n  /alias {k $1} {kill $1; skin corpse}\n  /alias {setup} {stand; wear all; look}",
		}
	}

	// Parse brace-delimited arguments from the raw text
	parsed := parseBraceDelimitedArgs(args[0])

	if len(parsed) < 2 {
		return CommandResult{
			IsLocal:  true,
			Handled:  false,
			Response: "Usage: /alias {pattern} {expansion}\nExamples:\n  /alias {k $1} {kill $1; skin corpse}\n  /alias {setup} {stand; wear all; look}",
		}
	}

	pattern := parsed[0]
	expansion := parsed[1]

	h.aliases.Set(pattern, expansion)

	return CommandResult{
		IsLocal:  true,
		Handled:  true,
		Response: "Alias set: {" + pattern + "} -> {" + expansion + "}",
	}
}

// cmdUnalias handles the /unalias command.
func (h *Handler) cmdUnalias(args []string) CommandResult {
	if len(args) < 1 {
		return CommandResult{
			IsLocal:  true,
			Handled:  false,
			Response: "Usage: /unalias <trigger>",
		}
	}

	// The trigger is the first word of the pattern
	trigger := args[0]
	if h.aliases.Delete(trigger) {
		return CommandResult{
			IsLocal:  true,
			Handled:  true,
			Response: "Alias removed: " + trigger,
		}
	}

	return CommandResult{
		IsLocal:  true,
		Handled:  false,
		Response: "Alias not found: " + trigger,
	}
}

// cmdAliases handles the /aliases command to list all aliases.
func (h *Handler) cmdAliases(args []string) CommandResult {
	aliases := h.aliases.List()

	if len(aliases) == 0 {
		return CommandResult{
			IsLocal:  true,
			Handled:  true,
			Response: "No aliases defined. Use /alias {pattern} {expansion} to create one.",
		}
	}

	var sb strings.Builder
	sb.WriteString("Defined aliases:\n")
	for pattern, expansion := range aliases {
		sb.WriteString("  {" + pattern + "} -> {" + expansion + "}\n")
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
	for pattern, expansion := range aliases {
		h.aliases.Set(pattern, expansion)
	}
}

// cmdTrigger handles the /trigger {pattern} {response} command.
// Supports brace delimiters for patterns/responses with spaces.
func (h *Handler) cmdTrigger(args []string) CommandResult {
	if len(args) == 0 || args[0] == "" {
		return CommandResult{
			IsLocal:  true,
			Handled:  false,
			Response: "Usage: /trigger {pattern} {response}\nExample: /trigger {^Greetings (.*)} {say Hello $1}",
		}
	}

	// Parse brace-delimited arguments from the raw text
	parsed := parseBraceDelimitedArgs(args[0])

	if len(parsed) < 2 {
		return CommandResult{
			IsLocal:  true,
			Handled:  false,
			Response: "Usage: /trigger {pattern} {response}\nExample: /trigger {^Greetings (.*)} {say Hello $1}",
		}
	}

	pattern := parsed[0]
	response := parsed[1]

	return CommandResult{
		IsLocal:  true,
		Handled:  true,
		Response: "Trigger added: " + pattern + " -> " + response,
		Action:   "trigger_add",
		ActionArgs: map[string]string{
			"pattern":  pattern,
			"response": response,
		},
	}
}

// cmdUntrigger handles the /untrigger {pattern} command.
// Supports brace delimiters for patterns with spaces.
func (h *Handler) cmdUntrigger(args []string) CommandResult {
	if len(args) == 0 || args[0] == "" {
		return CommandResult{
			IsLocal:  true,
			Handled:  false,
			Response: "Usage: /untrigger {pattern}",
		}
	}

	// Parse brace-delimited arguments from the raw text
	parsed := parseBraceDelimitedArgs(args[0])

	if len(parsed) < 1 || parsed[0] == "" {
		return CommandResult{
			IsLocal:  true,
			Handled:  false,
			Response: "Usage: /untrigger {pattern}",
		}
	}

	pattern := parsed[0]

	return CommandResult{
		IsLocal:  true,
		Handled:  true,
		Response: "Trigger removed: " + pattern,
		Action:   "trigger_remove",
		ActionArgs: map[string]string{
			"pattern": pattern,
		},
	}
}

// cmdTriggers handles the /triggers command to list available triggers.
func (h *Handler) cmdTriggers(args []string) CommandResult {
	return CommandResult{
		IsLocal: true,
		Handled: true,
		Action:  "trigger_list",
	}
}

// cmdMap handles the /map command and its subcommands.
func (h *Handler) cmdMap(args []string) CommandResult {
	if len(args) == 0 {
		return CommandResult{
			IsLocal:  true,
			Handled:  false, // Show Help
			Response: "Usage: /map <subcommand> [args...]\nSubcommands: create, paths, dig, undo, delete, goto, link, name, search, show, info, start, stop, exit",
		}
	}

	subcmd := strings.ToLower(args[0])
	subargs := args[1:]

	result := CommandResult{
		IsLocal:    true,
		Handled:    true,
		Action:     "map_" + subcmd,
		ActionArgs: make(map[string]string),
	}

	switch subcmd {
	case "create":
		// /map create [filename]
		if len(subargs) > 0 {
			result.ActionArgs["filename"] = subargs[0]
		}
		result.Response = "Initializing map..."

	case "paths":
		// /map paths [direction list]
		// If no args, show current paths. If args, set paths.
		if len(subargs) > 0 {
			result.ActionArgs["directions"] = strings.Join(subargs, ",")
		}

	case "dig":
		// /map dig <dir> <action...>
		if len(subargs) < 2 {
			return CommandResult{IsLocal: true, Handled: false, Response: "Usage: /map dig <direction> <action>"}
		}
		result.ActionArgs["direction"] = subargs[0]
		result.ActionArgs["action"] = strings.Join(subargs[1:], " ")
		result.Response = "Digging " + subargs[0] + "..."

	case "undo":
		result.Response = "Undoing last map action..."

	case "delete":
		// /map delete <id or name>
		if len(subargs) < 1 {
			return CommandResult{IsLocal: true, Handled: false, Response: "Usage: /map delete <room_id or room_name>"}
		}
		result.ActionArgs["query"] = strings.Join(subargs, " ")
		result.Response = "Deleting room..."

	case "goto":
		// /map goto <room_id or room_name>
		if len(subargs) < 1 {
			return CommandResult{IsLocal: true, Handled: false, Response: "Usage: /map goto <room_id or room_name>"}
		}
		result.ActionArgs["query"] = strings.Join(subargs, " ")
		result.Response = "Teleporting..."

	case "link":
		// /map link <direction> <room_id or room_name>
		if len(subargs) < 2 {
			return CommandResult{IsLocal: true, Handled: false, Response: "Usage: /map link <direction> <room_id or room_name>"}
		}
		result.ActionArgs["direction"] = subargs[0]
		result.ActionArgs["target"] = strings.Join(subargs[1:], " ")
		result.Response = "Linking..."

	case "start":
		// /map start [room_id or room_name] - start auto-mapping
		if len(subargs) > 0 {
			result.ActionArgs["query"] = strings.Join(subargs, " ")
		}
		result.Response = "Starting auto-mapping..."

	case "stop":
		// /map stop - stop auto-mapping
		result.Response = "Stopping auto-mapping..."

	case "name":
		// /map name <name...>
		if len(subargs) < 1 {
			return CommandResult{IsLocal: true, Handled: false, Response: "Usage: /map name <name>"}
		}
		result.ActionArgs["name"] = strings.Join(subargs, " ")
		result.Response = "Renaming room..."

	case "search":
		// /map search <query...>
		if len(subargs) < 1 {
			return CommandResult{IsLocal: true, Handled: false, Response: "Usage: /map search <query>"}
		}
		result.ActionArgs["query"] = strings.Join(subargs, " ")
		result.Response = "Searching..."

	case "show":
		if len(subargs) > 0 {
			result.ActionArgs["scope"] = subargs[0]
		}
	case "info", "exit":
		// No args needed

	default:
		return CommandResult{
			IsLocal:  true,
			Handled:  false,
			Response: "Unknown map subcommand: " + subcmd,
		}
	}

	return result
}
