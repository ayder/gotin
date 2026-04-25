package input

import (
	"strings"
	"testing"

	"github.com/ayder/gotin/internal/command"
)

func TestHandleInput_LocalCommand(t *testing.T) {
	h := NewHandler()

	tests := []struct {
		name      string
		input     string
		wantLocal bool
		wantType  string
	}{
		{
			name:      "quit command",
			input:     "/quit",
			wantLocal: true,
			wantType:  "*command.Quit",
		},
		{
			name:      "q alias for quit",
			input:     "/q",
			wantLocal: true,
			wantType:  "*command.Quit",
		},
		{
			name:      "help command",
			input:     "/help",
			wantLocal: true,
			wantType:  "show_help",
		},
		{
			name:      "connect command with args",
			input:     "/connect example.com 1234",
			wantLocal: true,
			wantType:  "*command.Connect",
		},
		{
			name:      "server command",
			input:     "say hello",
			wantLocal: false,
			wantType:  "",
		},
		{
			name:      "empty input",
			input:     "",
			wantLocal: false,
			wantType:  "",
		},
		{
			name:      "unknown local command",
			input:     "/unknown",
			wantLocal: false,
			wantType:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := h.HandleInput(tt.input)
			if len(results) == 0 {
				t.Fatalf("HandleInput(%q) returned empty results", tt.input)
			}
			result := results[0]
			isLocal := result.Command != nil || result.ShowHelp
			if isLocal != tt.wantLocal {
				t.Errorf("HandleInput(%q) isLocal = %v, want %v", tt.input, isLocal, tt.wantLocal)
			}
			if tt.wantType == "show_help" {
				if !result.ShowHelp {
					t.Errorf("HandleInput(%q) ShowHelp = false, want true", tt.input)
				}
			} else if tt.wantType != "" {
				gotType := ""
				switch result.Command.(type) {
				case *command.Quit:
					gotType = "*command.Quit"
				case *command.Connect:
					gotType = "*command.Connect"
				}
				if gotType != tt.wantType {
					t.Errorf("HandleInput(%q) command type = %s, want %s", tt.input, gotType, tt.wantType)
				}
			}
		})
	}
}

func TestHandleInput_ConnectValidation(t *testing.T) {
	h := NewHandler()

	tests := []struct {
		name       string
		input      string
		wantOK     bool
		wantHost   string
		wantPort   int
	}{
		{
			name:     "valid connect",
			input:    "/connect mudserver.com 4000",
			wantOK:   true,
			wantHost: "mudserver.com",
			wantPort: 4000,
		},
		{
			name:     "reconnect without args",
			input:    "/connect",
			wantOK:   true,
			wantHost: "",
			wantPort: 0,
		},
		{
			name:   "missing port",
			input:  "/connect mudserver.com",
			wantOK: false,
		},
		{
			name:   "invalid port",
			input:  "/connect mudserver.com abc",
			wantOK: false,
		},
		{
			name:   "port out of range",
			input:  "/connect mudserver.com 99999",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := h.HandleInput(tt.input)
			if len(results) == 0 {
				t.Fatalf("HandleInput(%q) returned empty results", tt.input)
			}
			result := results[0]
			gotOK := result.Command != nil
			if gotOK != tt.wantOK {
				t.Errorf("HandleInput(%q) ok = %v, want %v", tt.input, gotOK, tt.wantOK)
			}
			if tt.wantOK {
				cmd, ok := result.Command.(*command.Connect)
				if !ok {
					t.Fatalf("expected *command.Connect, got %T", result.Command)
				}
				if cmd.Host != tt.wantHost {
					t.Errorf("host = %q, want %q", cmd.Host, tt.wantHost)
				}
				if cmd.Port != tt.wantPort {
					t.Errorf("port = %d, want %d", cmd.Port, tt.wantPort)
				}
			}
		})
	}
}

func TestHandleInput_ConnectAlias(t *testing.T) {
	h := NewHandler()
	h.SetConnections(map[string]ConnectionAlias{
		"t2t": {Host: "t2tmud.org", Port: 9999},
	})

	results := h.HandleInput("/connect t2t")
	if len(results) == 0 {
		t.Fatal("HandleInput returned empty results")
	}
	result := results[0]
	if result.Command == nil {
		t.Fatal("Expected command for alias connect")
	}
	cmd, ok := result.Command.(*command.Connect)
	if !ok {
		t.Fatalf("expected *command.Connect, got %T", result.Command)
	}
	if cmd.Host != "t2tmud.org" {
		t.Errorf("host = %q, want %q", cmd.Host, "t2tmud.org")
	}
	if cmd.Port != 9999 {
		t.Errorf("port = %d, want %d", cmd.Port, 9999)
	}

	// Unknown alias
	results = h.HandleInput("/connect unknown")
	result = results[0]
	if result.Command != nil {
		t.Error("Expected no command for unknown alias")
	}
}

func TestHandleInput_HelpResponse(t *testing.T) {
	h := NewHandler()
	results := h.HandleInput("/help")
	if len(results) == 0 {
		t.Fatal("HandleInput(/help) returned empty results")
	}
	result := results[0]

	if !result.ShowHelp {
		t.Error("Expected /help to set ShowHelp")
	}
	if result.Command != nil {
		t.Error("Expected /help Command to be nil")
	}
}

func TestParseBraceDelimitedArgs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "two brace-delimited args",
			input:    "{^Greetings (.*)} {say Hello $1}",
			expected: []string{"^Greetings (.*)", "say Hello $1"},
		},
		{
			name:     "nested braces in pattern",
			input:    "{a{b}c} {response}",
			expected: []string{"a{b}c", "response"},
		},
		{
			name:     "simple space-separated",
			input:    "hello world",
			expected: []string{"hello", "world"},
		},
		{
			name:     "mixed braces and spaces",
			input:    "simple {complex arg}",
			expected: []string{"simple", "complex arg"},
		},
		{
			name:     "single braced arg",
			input:    "{pattern with spaces}",
			expected: []string{"pattern with spaces"},
		},
		{
			name:     "empty input",
			input:    "",
			expected: []string{},
		},
		{
			name:     "spaces only",
			input:    "   ",
			expected: []string{},
		},
		{
			name:     "complex regex pattern",
			input:    "{^(\\w+) says: (.*)} {reply $1 $2}",
			expected: []string{"^(\\w+) says: (.*)", "reply $1 $2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseBraceDelimitedArgs(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("parseBraceDelimitedArgs(%q) returned %d args, want %d\nGot: %v\nWant: %v",
					tt.input, len(result), len(tt.expected), result, tt.expected)
				return
			}
			for i, want := range tt.expected {
				if result[i] != want {
					t.Errorf("parseBraceDelimitedArgs(%q)[%d] = %q, want %q",
						tt.input, i, result[i], want)
				}
			}
		})
	}
}

func TestHandleInput_TriggerCommand(t *testing.T) {
	h := NewHandler()

	tests := []struct {
		name         string
		input        string
		wantOK       bool
		wantPattern  string
		wantResponse string
	}{
		{
			name:         "brace delimited trigger add",
			input:        "/trigger add {^Greetings (.*)} {say Hello $1}",
			wantOK:       true,
			wantPattern:  "^Greetings (.*)",
			wantResponse: "say Hello $1",
		},
		{
			name:         "simple trigger add without braces",
			input:        "/trigger add hello world",
			wantOK:       true,
			wantPattern:  "hello",
			wantResponse: "world",
		},
		{
			name:         "trigger add with spaces in pattern",
			input:        "/trigger add {You are hungry} {eat bread}",
			wantOK:       true,
			wantPattern:  "You are hungry",
			wantResponse: "eat bread",
		},
		{
			name:   "missing subcommand",
			input:  "/trigger",
			wantOK: false,
		},
		{
			name:   "trigger add missing arguments",
			input:  "/trigger add {pattern}",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := h.HandleInput(tt.input)
			if len(results) == 0 {
				t.Fatalf("HandleInput(%q) returned empty results", tt.input)
			}
			result := results[0]
			gotOK := result.Command != nil
			if gotOK != tt.wantOK {
				t.Errorf("HandleInput(%q) ok = %v, want %v\nResponse: %s",
					tt.input, gotOK, tt.wantOK, result.Response)
				return
			}
			if tt.wantOK {
				cmd, ok := result.Command.(*command.TriggerAdd)
				if !ok {
					t.Fatalf("expected *command.TriggerAdd, got %T", result.Command)
				}
				if cmd.Pattern != tt.wantPattern {
					t.Errorf("pattern = %q, want %q", cmd.Pattern, tt.wantPattern)
				}
				if cmd.Response != tt.wantResponse {
					t.Errorf("response = %q, want %q", cmd.Response, tt.wantResponse)
				}
			}
		})
	}
}

func TestHandleInput_TriggerRemoveCommand(t *testing.T) {
	h := NewHandler()

	tests := []struct {
		name        string
		input       string
		wantOK      bool
		wantPattern string
	}{
		{
			name:        "brace delimited trigger remove",
			input:       "/trigger remove {^Greetings (.*)}",
			wantOK:      true,
			wantPattern: "^Greetings (.*)",
		},
		{
			name:        "simple trigger remove",
			input:       "/trigger remove hello",
			wantOK:      true,
			wantPattern: "hello",
		},
		{
			name:        "trigger remove with spaces in pattern",
			input:       "/trigger remove {pattern with spaces}",
			wantOK:      true,
			wantPattern: "pattern with spaces",
		},
		{
			name:   "missing arguments",
			input:  "/trigger remove",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := h.HandleInput(tt.input)
			if len(results) == 0 {
				t.Fatalf("HandleInput(%q) returned empty results", tt.input)
			}
			result := results[0]
			gotOK := result.Command != nil
			if gotOK != tt.wantOK {
				t.Errorf("HandleInput(%q) ok = %v, want %v\nResponse: %s",
					tt.input, gotOK, tt.wantOK, result.Response)
				return
			}
			if tt.wantOK {
				cmd, ok := result.Command.(*command.TriggerRemove)
				if !ok {
					t.Fatalf("expected *command.TriggerRemove, got %T", result.Command)
				}
				if cmd.Pattern != tt.wantPattern {
					t.Errorf("pattern = %q, want %q", cmd.Pattern, tt.wantPattern)
				}
			}
		})
	}
}

func TestHandleInput_TriggerListCommand(t *testing.T) {
	h := NewHandler()
	results := h.HandleInput("/trigger list")
	if len(results) == 0 {
		t.Fatal("HandleInput(/trigger list) returned empty results")
	}
	result := results[0]
	if result.Command == nil {
		t.Error("Expected /trigger list to produce a command")
	}
	if _, ok := result.Command.(*command.TriggerList); !ok {
		t.Errorf("Expected *command.TriggerList, got %T", result.Command)
	}
}

func TestHandleInput_AliasAddCommand(t *testing.T) {
	h := NewHandler()

	tests := []struct {
		name          string
		input         string
		wantOK        bool
		wantPattern   string
		wantExpansion string
		wantResponse  string
	}{
		{
			name:          "alias add brace delimited",
			input:         "/alias add {k $1} {kill $1; skin corpse}",
			wantOK:        true,
			wantPattern:   "k $1",
			wantExpansion: "kill $1; skin corpse",
			wantResponse:  "Alias set: {k $1} -> {kill $1; skin corpse}",
		},
		{
			name:          "alias add simple",
			input:         "/alias add k kill",
			wantOK:        true,
			wantPattern:   "k",
			wantExpansion: "kill",
			wantResponse:  "Alias set: {k} -> {kill}",
		},
		{
			name:   "alias add missing args",
			input:  "/alias add {pattern}",
			wantOK: false,
		},
		{
			name:   "alias add no args",
			input:  "/alias add",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := h.HandleInput(tt.input)
			if len(results) == 0 {
				t.Fatalf("HandleInput(%q) returned empty results", tt.input)
			}
			result := results[0]
			gotOK := result.Command != nil
			if gotOK != tt.wantOK {
				t.Errorf("HandleInput(%q).ok = %v, want %v\nResponse: %s",
					tt.input, gotOK, tt.wantOK, result.Response)
				return
			}
			if tt.wantOK {
				if result.Response != tt.wantResponse {
					t.Errorf("HandleInput(%q).Response = %q, want %q",
						tt.input, result.Response, tt.wantResponse)
				}
				aliases := h.GetAliases()
				if aliases[tt.wantPattern] != tt.wantExpansion {
					t.Errorf("Alias not set correctly: got %q, want %q",
						aliases[tt.wantPattern], tt.wantExpansion)
				}
			}
		})
	}
}

func TestHandleInput_AliasRemoveCommand(t *testing.T) {
	h := NewHandler()
	h.SetAliases(map[string]string{"k": "kill", "l": "look"})

	tests := []struct {
		name        string
		input       string
		wantOK      bool
		wantPattern string
		shouldExist bool
	}{
		{
			name:        "alias remove simple",
			input:       "/alias remove k",
			wantOK:      true,
			wantPattern: "k",
			shouldExist: false,
		},
		{
			name:        "alias remove braced",
			input:       "/alias remove {l}",
			wantOK:      true,
			wantPattern: "l",
			shouldExist: false,
		},
		{
			name:        "alias remove not found",
			input:       "/alias remove x",
			wantOK:      false,
			wantPattern: "x",
			shouldExist: false,
		},
		{
			name:   "alias remove no args",
			input:  "/alias remove",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := h.HandleInput(tt.input)
			if len(results) == 0 {
				t.Fatalf("HandleInput(%q) returned empty results", tt.input)
			}
			result := results[0]
			gotOK := result.Command != nil
			if gotOK != tt.wantOK {
				t.Errorf("HandleInput(%q).ok = %v, want %v\nResponse: %s",
					tt.input, gotOK, tt.wantOK, result.Response)
				return
			}
			aliases := h.GetAliases()
			_, exists := aliases[tt.wantPattern]
			if exists != tt.shouldExist {
				t.Errorf("Alias %q existence = %v, want %v", tt.wantPattern, exists, tt.shouldExist)
			}
		})
	}
}

func TestHandleInput_AliasListCommand(t *testing.T) {
	h := NewHandler()

	// Empty list
	results := h.HandleInput("/alias list")
	if len(results) == 0 {
		t.Fatal("HandleInput(/alias list) returned empty results")
	}
	result := results[0]
	if result.Command == nil {
		t.Error("Expected /alias list to produce a command")
	}
	if result.Response != "No aliases defined. Use /alias add {pattern} {expansion} to create one." {
		t.Errorf("Unexpected response for empty alias list: %q", result.Response)
	}

	// With aliases
	h.SetAliases(map[string]string{"k": "kill", "l": "look"})
	results = h.HandleInput("/alias list")
	result = results[0]
	if result.Command == nil {
		t.Error("Expected /alias list to produce a command")
	}
	if !strings.Contains(result.Response, "{k} -> {kill}") || !strings.Contains(result.Response, "{l} -> {look}") {
		t.Errorf("Alias list response missing expected aliases: %q", result.Response)
	}
}

func TestHandleInput_ConnectionAliasCommand(t *testing.T) {
	h := NewHandler()

	// Add connection alias
	results := h.HandleInput("/connection add t2t t2tmud.org 9999")
	if len(results) == 0 {
		t.Fatal("HandleInput returned empty results")
	}
	result := results[0]
	if result.Command == nil {
		t.Fatalf("Expected command, got response: %s", result.Response)
	}
	if result.Response != "Connection alias set: t2t -> t2tmud.org:9999" {
		t.Errorf("Unexpected response: %q", result.Response)
	}

	conns := h.GetConnections()
	if conns["t2t"].Host != "t2tmud.org" || conns["t2t"].Port != 9999 {
		t.Errorf("Connection alias not set correctly: %+v", conns["t2t"])
	}

	// List connection aliases
	results = h.HandleInput("/connection list")
	result = results[0]
	if result.Command == nil {
		t.Fatalf("Expected command, got response: %s", result.Response)
	}
	if !strings.Contains(result.Response, "t2t -> t2tmud.org:9999") {
		t.Errorf("Connection list missing alias: %q", result.Response)
	}

	// Remove connection alias
	results = h.HandleInput("/connection remove t2t")
	result = results[0]
	if result.Command == nil {
		t.Fatalf("Expected command, got response: %s", result.Response)
	}
	conns = h.GetConnections()
	if _, ok := conns["t2t"]; ok {
		t.Error("Connection alias should have been removed")
	}
}
