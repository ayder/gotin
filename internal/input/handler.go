package input

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ayder/gotin/internal/command"
	"github.com/ayder/gotin/internal/mapper"
	"github.com/ayder/gotin/internal/parser"
)

// ConnectionAlias maps a name to a host:port pair for quick connecting.
// Protocols is an optional per-connection toggle map keyed by protocol name
// (e.g. {"MCCP2": true, "MXP": false}). Absent entries fall back to the
// installer's defaults.
type ConnectionAlias struct {
	Host           string                 `json:"host"`
	Port           int                    `json:"port"`
	Auto           bool                   `json:"auto"`
	Protocols      map[string]bool        `json:"protocols,omitempty"`
	MappingOptions *mapper.MappingOptions `json:"mapping_options,omitempty"`
}

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

// ParseResult is the outcome of parsing a single piece of user input.
type ParseResult struct {
	// Command is non-nil for local commands that need app-layer handling.
	Command command.Command
	// ServerText is the text to send to the MUD server.
	ServerText string
	// Response is immediate UI feedback (shown in status line or viewport).
	Response string
	// ShowHelp toggles the help widget.
	ShowHelp bool
}

// Handler processes user input and routes it to local commands or server.
type Handler struct {
	// commands maps command verbs to their handler functions.
	commands map[string]func(args []string) ParseResult
	// aliases manages user-defined command aliases.
	aliases *AliasManager
	// connections maps names to host:port aliases.
	connections map[string]ConnectionAlias
}

// NewHandler creates a new command handler with built-in commands.
func NewHandler() *Handler {
	h := &Handler{
		commands:    make(map[string]func(args []string) ParseResult),
		aliases:     NewAliasManager(),
		connections: make(map[string]ConnectionAlias),
	}

	// Register built-in commands
	h.commands["quit"] = h.cmdQuit
	h.commands["q"] = h.cmdQuit // alias
	h.commands["connect"] = h.cmdConnect
	h.commands["help"] = h.cmdHelp
	h.commands["alias"] = h.cmdAlias
	h.commands["trigger"] = h.cmdTrigger
	h.commands["map"] = h.cmdMap
	h.commands["save"] = h.cmdSave
	h.commands["load"] = h.cmdLoad
	h.commands["proto"] = h.cmdProto
	h.commands["connection"] = h.cmdConnection

	return h
}

// cmdProto handles the /proto command: /proto <name> on|off and /proto list.
func (h *Handler) cmdProto(args []string) ParseResult {
	if len(args) == 0 {
		return ParseResult{
			Response: "Usage: /proto <name> on|off\n       /proto list",
		}
	}
	sub := strings.ToLower(args[0])
	if sub == "list" {
		return ParseResult{Command: &command.ProtoList{}}
	}
	if len(args) < 2 {
		return ParseResult{
			Response: "Usage: /proto <name> on|off",
		}
	}
	name := strings.ToUpper(args[0])
	state := strings.ToLower(args[1])
	switch state {
	case "on":
		return ParseResult{
			Command:  &command.ProtoOn{Name: name},
			Response: fmt.Sprintf("[proto] %s enabled", name),
		}
	case "off":
		return ParseResult{
			Command:  &command.ProtoOff{Name: name},
			Response: fmt.Sprintf("[proto] %s disabled", name),
		}
	default:
		return ParseResult{
			Response: "Usage: /proto <name> on|off",
		}
	}
}

// HandleInput processes user input and determines if it's a local or server command.
// Returns a slice of ParseResults to support multi-command alias expansions.
func (h *Handler) HandleInput(text string) []ParseResult {
	text = strings.TrimSpace(text)
	if text == "" {
		return []ParseResult{{ServerText: ""}}
	}

	// Check if it's a local command (starts with /)
	if strings.HasPrefix(text, CommandPrefix) {
		// Parse the local command
		return []ParseResult{h.processLocalCommand(text)}
	}

	// Not a local command - expand aliases before sending to server
	// This returns a slice of commands (for multi-command expansions)
	expandedCommands := h.aliases.Expand(text)

	if len(expandedCommands) == 0 {
		return []ParseResult{{ServerText: text}}
	}

	// Process each expanded command
	var results []ParseResult
	for _, cmd := range expandedCommands {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}

		// Check if expanded command is a local command
		if strings.HasPrefix(cmd, CommandPrefix) {
			results = append(results, h.processLocalCommand(cmd))
		} else {
			results = append(results, ParseResult{
				ServerText: cmd,
			})
		}
	}

	if len(results) == 0 {
		return []ParseResult{{ServerText: text}}
	}

	return results
}

// processLocalCommand parses and executes a local command.
func (h *Handler) processLocalCommand(text string) ParseResult {
	// Remove the prefix and split into parts
	cmdText := strings.TrimPrefix(text, CommandPrefix)
	parts := strings.Fields(cmdText)

	if len(parts) == 0 {
		return ParseResult{
			Response: "Unknown command. Type /help for available commands.",
		}
	}

	verb := strings.ToLower(parts[0])

	// For commands that support brace delimiters, pass the raw text after the verb
	switch verb {
	case "trigger", "alias":
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
	return ParseResult{
		Response: "Unknown command: /" + verb + ". Type /help for available commands.",
	}
}

// cmdQuit handles the /quit command.
func (h *Handler) cmdQuit(args []string) ParseResult {
	return ParseResult{
		Command:  &command.Quit{},
		Response: "Goodbye!",
	}
}

// cmdConnect handles the /connect <host> <port>, /connect <alias>, or /connect command.
func (h *Handler) cmdConnect(args []string) ParseResult {
	if len(args) == 0 {
		return ParseResult{
			Command: &command.Connect{},
		}
	}

	if len(args) == 1 {
		if ca, ok := h.connections[args[0]]; ok {
			return ParseResult{
				Command:  &command.Connect{Host: ca.Host, Port: ca.Port},
				Response: fmt.Sprintf("Connecting to %s:%d (alias: %s)...", ca.Host, ca.Port, args[0]),
			}
		}
		return ParseResult{
			Response: "Unknown connection alias: " + args[0],
		}
	}

	host := args[0]
	portStr := args[1]

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return ParseResult{
			Response: "Invalid port: " + portStr + ". Port must be a number between 1 and 65535.",
		}
	}

	return ParseResult{
		Command:  &command.Connect{Host: host, Port: port},
		Response: "Connecting to " + host + ":" + portStr + "...",
	}
}

// HelpText returns the full help text for display in the help widget.
func (h *Handler) HelpText() string {
	return `Available commands:

Connection
  /connect                            Reconnect to the last connected server
  /connect <host> <port>              Connect to a MUD server
  /connect <alias>                    Connect using a saved alias
  /connection add <name> <host> <port> [auto]
                                      Save a connection alias
  /connection remove <name>           Remove a connection alias
  /connection list                    List saved connection aliases

Aliases
  /alias add {pattern} {expansion}    Create a command alias
  /alias remove <pattern>             Remove a command alias
  /alias list                         List all aliases

Triggers
  /trigger add {pattern} {response}   Create a trigger
  /trigger remove {pattern}           Remove a trigger
  /trigger list                       List all triggers

Protocols
  /proto <name> on|off                Enable/disable a protocol
  /proto list                         List installed protocols

Map
  /map create [filename]              Initialize a new map
  /map paths [directions]             Show or set path directions
  /map dig <dir> <action>             Create a room in a direction
  /map undo                           Undo last map action
  /map delete <room>                  Delete a room
  /map teleport <room>                Teleport to a room
  /map link <dir> <room>              Link a direction to a room
  /map name <name>                    Rename current room
  /map search <query>                 Search for a room
  /map mermaid [scope|radius]         Render Mermaid graph of nearby rooms
  /map show                           Toggle the side-by-side map pane
  /map info                           Show current room info
  /map option                         Show mapping strategy options
  /map option vnum|hash on|off        Enable/disable a mapping strategy
  /map option none                    Disable all mapping strategies
  /map start [room]                   Start auto-mapping
  /map stop                           Stop auto-mapping
  /map exit                           Save map and stop mapping

Other
  /save [filename]                    Save settings to file
  /load [filename]                    Load settings from file
  /quit, /q                           Exit the application
  /help                               Show this help widget

All other input is sent to the connected server.`
}

// cmdHelp handles the /help command.
func (h *Handler) cmdHelp(args []string) ParseResult {
	return ParseResult{ShowHelp: true}
}

// cmdAlias handles the /alias command and its subcommands.
func (h *Handler) cmdAlias(args []string) ParseResult {
	if len(args) == 0 || args[0] == "" {
		return ParseResult{
			Response: "Usage: /alias <subcommand> [args...]\nSubcommands: add, remove, list",
		}
	}

	// Extract subcommand and remainder from raw text
	raw := args[0]
	fields := strings.Fields(raw)
	subcmd := strings.ToLower(fields[0])

	var remainder string
	if len(fields) > 0 {
		subcmdEnd := strings.Index(raw, fields[0]) + len(fields[0])
		remainder = strings.TrimSpace(raw[subcmdEnd:])
	}

	switch subcmd {
	case "add":
		if remainder == "" {
			return ParseResult{
				Response: "Usage: /alias add {pattern} {expansion}",
			}
		}
		parsed := parseBraceDelimitedArgs(remainder)
		if len(parsed) < 2 {
			return ParseResult{
				Response: "Usage: /alias add {pattern} {expansion}",
			}
		}

		pattern := parsed[0]
		expansion := parsed[1]
		h.aliases.Set(pattern, expansion)
		return ParseResult{
			Command:  &command.AliasAdd{Pattern: pattern, Expansion: expansion},
			Response: "Alias set: {" + pattern + "} -> {" + expansion + "}",
		}

	case "remove":
		if remainder == "" {
			return ParseResult{
				Response: "Usage: /alias remove <pattern>",
			}
		}

		parsed := parseBraceDelimitedArgs(remainder)
		if len(parsed) < 1 || parsed[0] == "" {
			return ParseResult{
				Response: "Usage: /alias remove <pattern>",
			}
		}
		trigger := parsed[0]

		if h.aliases.Delete(trigger) {
			return ParseResult{
				Command:  &command.AliasRemove{Pattern: trigger},
				Response: "Alias removed: " + trigger,
			}
		}
		return ParseResult{
			Response: "Alias not found: " + trigger,
		}

	case "list":
		aliases := h.aliases.List()
		if len(aliases) == 0 {
			return ParseResult{
				Command:  &command.AliasList{},
				Response: "No aliases defined. Use /alias add {pattern} {expansion} to create one.",
			}
		}
		var sb strings.Builder
		sb.WriteString("Defined aliases:\n")
		for pattern, expansion := range aliases {
			sb.WriteString("  {" + pattern + "} -> {" + expansion + "}\n")
		}
		return ParseResult{
			Command:  &command.AliasList{},
			Response: strings.TrimSuffix(sb.String(), "\n"),
		}

	default:
		return ParseResult{
			Response: "Unknown alias subcommand: " + subcmd + "\nSubcommands: add, remove, list",
		}
	}
}

// cmdConnection handles the /connection command and its subcommands.
func (h *Handler) cmdConnection(args []string) ParseResult {
	if len(args) == 0 {
		return ParseResult{
			Response: "Usage: /connection <subcommand> [args...]\nSubcommands: add, remove, list",
		}
	}

	subcmd := strings.ToLower(args[0])
	subargs := args[1:]

	switch subcmd {
	case "add":
		if len(subargs) < 3 {
			return ParseResult{
				Response: "Usage: /connection add <name> <host> <port> [auto]\nExample: /connection add t2t t2tmud.org 9999 auto",
			}
		}
		name := subargs[0]
		host := subargs[1]
		portStr := subargs[2]
		port, err := strconv.Atoi(portStr)
		if err != nil || port < 1 || port > 65535 {
			return ParseResult{
				Response: "Invalid port: " + portStr,
			}
		}
		auto := false
		if len(subargs) >= 4 && subargs[3] == "auto" {
			auto = true
		}
		h.connections[name] = ConnectionAlias{
			Host:           host,
			Port:           port,
			Auto:           auto,
			MappingOptions: defaultMappingOptionsPtr(),
		}
		autoStr := ""
		if auto {
			autoStr = " (auto)"
		}
		return ParseResult{
			Command:  &command.ConnectionAliasAdd{Name: name, Host: host, Port: port, Auto: auto},
			Response: fmt.Sprintf("Connection alias set: %s -> %s:%d%s", name, host, port, autoStr),
		}

	case "remove":
		if len(subargs) < 1 {
			return ParseResult{
				Response: "Usage: /connection remove <name>",
			}
		}
		name := subargs[0]
		if _, ok := h.connections[name]; ok {
			delete(h.connections, name)
			return ParseResult{
				Command:  &command.ConnectionAliasRemove{Name: name},
				Response: "Connection alias removed: " + name,
			}
		}
		return ParseResult{
			Response: "Connection alias not found: " + name,
		}

	case "list":
		if len(h.connections) == 0 {
			return ParseResult{
				Response: "No connection aliases defined. Use /connection add <name> <host> <port> [auto] to create one.",
			}
		}
		var sb strings.Builder
		sb.WriteString("Connection aliases:\n")
		for name, ca := range h.connections {
			autoStr := ""
			if ca.Auto {
				autoStr = " [auto]"
			}
			sb.WriteString(fmt.Sprintf("  %s -> %s:%d%s\n", name, ca.Host, ca.Port, autoStr))
		}
		return ParseResult{
			Command:  &command.AliasListConnections{},
			Response: strings.TrimSuffix(sb.String(), "\n"),
		}

	default:
		return ParseResult{
			Response: "Unknown connection subcommand: " + subcmd + "\nSubcommands: add, remove, list",
		}
	}
}

func defaultMappingOptionsPtr() *mapper.MappingOptions {
	opts := mapper.DefaultMappingOptions()
	return &opts
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

// ClearAliases removes all aliases.
func (h *Handler) ClearAliases() {
	h.aliases.Clear()
}

// GetConnections returns all defined connection aliases.
func (h *Handler) GetConnections() map[string]ConnectionAlias {
	result := make(map[string]ConnectionAlias, len(h.connections))
	for k, v := range h.connections {
		result[k] = v
	}
	return result
}

// SetConnections loads connection aliases from a map.
func (h *Handler) SetConnections(connections map[string]ConnectionAlias) {
	for name, ca := range connections {
		h.connections[name] = ca
	}
}

// ClearConnections removes all connection aliases.
func (h *Handler) ClearConnections() {
	h.connections = make(map[string]ConnectionAlias)
}

// cmdTrigger handles the /trigger command and its subcommands.
func (h *Handler) cmdTrigger(args []string) ParseResult {
	if len(args) == 0 || args[0] == "" {
		return ParseResult{
			Response: "Usage: /trigger <subcommand> [args...]\nSubcommands: add, remove, list",
		}
	}

	// Extract subcommand and remainder from raw text
	raw := args[0]
	fields := strings.Fields(raw)
	subcmd := strings.ToLower(fields[0])

	var remainder string
	if len(fields) > 0 {
		subcmdEnd := strings.Index(raw, fields[0]) + len(fields[0])
		remainder = strings.TrimSpace(raw[subcmdEnd:])
	}

	switch subcmd {
	case "add":
		if remainder == "" {
			return ParseResult{
				Response: "Usage: /trigger add {pattern} {response}\nExample: /trigger add {^Greetings (.*)} {say Hello $1}",
			}
		}
		parsed := parseBraceDelimitedArgs(remainder)
		if len(parsed) < 2 {
			return ParseResult{
				Response: "Usage: /trigger add {pattern} {response}\nExample: /trigger add {^Greetings (.*)} {say Hello $1}",
			}
		}
		pattern := parsed[0]
		response := parsed[1]
		return ParseResult{
			Command:  &command.TriggerAdd{Pattern: pattern, Response: response},
			Response: "Trigger added: " + pattern + " -> " + response,
		}

	case "remove":
		if remainder == "" {
			return ParseResult{
				Response: "Usage: /trigger remove {pattern}",
			}
		}
		parsed := parseBraceDelimitedArgs(remainder)
		if len(parsed) < 1 || parsed[0] == "" {
			return ParseResult{
				Response: "Usage: /trigger remove {pattern}",
			}
		}
		pattern := parsed[0]
		return ParseResult{
			Command:  &command.TriggerRemove{Pattern: pattern},
			Response: "Trigger removed: " + pattern,
		}

	case "list":
		return ParseResult{Command: &command.TriggerList{}}

	default:
		return ParseResult{
			Response: "Unknown trigger subcommand: " + subcmd + "\nSubcommands: add, remove, list",
		}
	}
}

// cmdSave handles the /save [filename] command.
func (h *Handler) cmdSave(args []string) ParseResult {
	filename := "config.json"
	if len(args) > 0 && args[0] != "" {
		filename = args[0]
	}

	return ParseResult{
		Command:  &command.Save{Filename: filename},
		Response: "Saving aliases and triggers to " + filename + "...",
	}
}

// cmdLoad handles the /load [filename] command.
func (h *Handler) cmdLoad(args []string) ParseResult {
	filename := "config.json"
	if len(args) > 0 && args[0] != "" {
		filename = args[0]
	}

	return ParseResult{
		Command:  &command.Load{Filename: filename},
		Response: "Loading aliases and triggers from " + filename + "...",
	}
}

// cmdMap handles the /map command and its subcommands.
func (h *Handler) cmdMap(args []string) ParseResult {
	if len(args) == 0 {
		return ParseResult{
			Response: "Usage: /map <subcommand> [args...]\nSubcommands: create, paths, dig, undo, delete, teleport, link, name, search, mermaid, show, refresh, info, option, start, stop, exit",
		}
	}

	subcmd := strings.ToLower(args[0])
	subargs := args[1:]

	switch subcmd {
	case "create":
		// /map create [filename]
		filename := ""
		if len(subargs) > 0 {
			filename = subargs[0]
		}
		return ParseResult{
			Command:  &command.MapCreate{Filename: filename},
			Response: "Initializing map...",
		}

	case "paths":
		// /map paths [direction list]
		dirs := ""
		if len(subargs) > 0 {
			dirs = strings.Join(subargs, ",")
		}
		return ParseResult{Command: &command.MapPaths{Directions: dirs}}

	case "dig":
		// /map dig <dir> <action...>
		if len(subargs) < 2 {
			return ParseResult{Response: "Usage: /map dig <direction> <action>"}
		}
		return ParseResult{
			Command:  &command.MapDig{Direction: subargs[0], Action: strings.Join(subargs[1:], " ")},
			Response: "Digging " + subargs[0] + "...",
		}

	case "undo":
		return ParseResult{
			Command:  &command.MapUndo{},
			Response: "Undoing last map action...",
		}

	case "delete":
		// /map delete <id or name>
		if len(subargs) < 1 {
			return ParseResult{Response: "Usage: /map delete <room_id or room_name>"}
		}
		return ParseResult{
			Command:  &command.MapDelete{Query: strings.Join(subargs, " ")},
			Response: "Deleting room...",
		}

	case "teleport":
		// /map teleport <room_id or room_name>
		if len(subargs) < 1 {
			return ParseResult{Response: "Usage: /map teleport <room_id or room_name>"}
		}
		return ParseResult{
			Command:  &command.MapGoto{Query: strings.Join(subargs, " ")},
			Response: "Teleporting...",
		}

	case "link":
		// /map link <direction> <room_id or room_name>
		if len(subargs) < 2 {
			return ParseResult{Response: "Usage: /map link <direction> <room_id or room_name>"}
		}
		return ParseResult{
			Command:  &command.MapLink{Direction: subargs[0], Target: strings.Join(subargs[1:], " ")},
			Response: "Linking...",
		}

	case "start":
		// /map start [room_id or room_name] - start auto-mapping
		query := ""
		if len(subargs) > 0 {
			query = strings.Join(subargs, " ")
		}
		return ParseResult{
			Command:  &command.MapStart{Query: query},
			Response: "Starting auto-mapping...",
		}

	case "stop":
		return ParseResult{
			Command:  &command.MapStop{},
			Response: "Stopping auto-mapping...",
		}

	case "name":
		// /map name <name...>
		if len(subargs) < 1 {
			return ParseResult{Response: "Usage: /map name <name>"}
		}
		return ParseResult{
			Command:  &command.MapName{Name: strings.Join(subargs, " ")},
			Response: "Renaming room...",
		}

	case "search":
		// /map search <query...>
		if len(subargs) < 1 {
			return ParseResult{Response: "Usage: /map search <query>"}
		}
		return ParseResult{
			Command:  &command.MapSearch{Query: strings.Join(subargs, " ")},
			Response: "Searching...",
		}

	case "mermaid":
		scope := ""
		if len(subargs) > 0 {
			scope = subargs[0]
		}
		return ParseResult{Command: &command.MapMermaid{Scope: scope}}

	case "show":
		return ParseResult{Command: &command.MapShow{}, Response: "Toggling map pane..."}

	case "refresh":
		return ParseResult{Command: &command.MapRefresh{}, Response: "Refreshing map layout..."}

	case "info":
		return ParseResult{Command: &command.MapInfo{}}

	case "option":
		if len(subargs) == 0 {
			return ParseResult{Command: &command.MapOption{Print: true}}
		}
		strat := strings.ToLower(subargs[0])
		switch strat {
		case "vnum", "hash":
			if len(subargs) != 2 {
				return ParseResult{Response: "Usage: /map option " + strat + " on|off"}
			}
			switch strings.ToLower(subargs[1]) {
			case "on":
				return ParseResult{Command: &command.MapOption{Strategy: strat, Enable: true}}
			case "off":
				return ParseResult{Command: &command.MapOption{Strategy: strat, Enable: false}}
			default:
				return ParseResult{Response: "Usage: /map option " + strat + " on|off"}
			}
		case "none":
			if len(subargs) != 1 {
				return ParseResult{Response: "Usage: /map option none"}
			}
			return ParseResult{Command: &command.MapOption{Strategy: "none"}}
		default:
			return ParseResult{Response: "Usage: /map option [vnum|hash] [on|off] | /map option none | /map option"}
		}

	case "exit":
		return ParseResult{Command: &command.MapExit{}}

	default:
		return ParseResult{
			Response: "Unknown map subcommand: " + subcmd,
		}
	}
}
