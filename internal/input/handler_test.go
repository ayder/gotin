package input

import (
	"testing"
)

func TestHandleInput_LocalCommand(t *testing.T) {
	h := NewHandler()

	tests := []struct {
		name       string
		input      string
		wantLocal  bool
		wantAction string
	}{
		{
			name:       "quit command",
			input:      "/quit",
			wantLocal:  true,
			wantAction: "quit",
		},
		{
			name:       "q alias for quit",
			input:      "/q",
			wantLocal:  true,
			wantAction: "quit",
		},
		{
			name:       "help command",
			input:      "/help",
			wantLocal:  true,
			wantAction: "",
		},
		{
			name:       "connect command with args",
			input:      "/connect example.com 1234",
			wantLocal:  true,
			wantAction: "connect",
		},
		{
			name:       "server command",
			input:      "say hello",
			wantLocal:  false,
			wantAction: "",
		},
		{
			name:       "empty input",
			input:      "",
			wantLocal:  false,
			wantAction: "",
		},
		{
			name:       "unknown local command",
			input:      "/unknown",
			wantLocal:  true,
			wantAction: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := h.HandleInput(tt.input)
			if len(results) == 0 {
				t.Fatalf("HandleInput(%q) returned empty results", tt.input)
			}
			result := results[0]
			if result.IsLocal != tt.wantLocal {
				t.Errorf("HandleInput(%q).IsLocal = %v, want %v", tt.input, result.IsLocal, tt.wantLocal)
			}
			if result.Action != tt.wantAction {
				t.Errorf("HandleInput(%q).Action = %q, want %q", tt.input, result.Action, tt.wantAction)
			}
		})
	}
}

func TestHandleInput_ConnectValidation(t *testing.T) {
	h := NewHandler()

	tests := []struct {
		name        string
		input       string
		wantHandled bool
		wantHost    string
		wantPort    string
	}{
		{
			name:        "valid connect",
			input:       "/connect mudserver.com 4000",
			wantHandled: true,
			wantHost:    "mudserver.com",
			wantPort:    "4000",
		},
		{
			name:        "missing port",
			input:       "/connect mudserver.com",
			wantHandled: false,
		},
		{
			name:        "invalid port",
			input:       "/connect mudserver.com abc",
			wantHandled: false,
		},
		{
			name:        "port out of range",
			input:       "/connect mudserver.com 99999",
			wantHandled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := h.HandleInput(tt.input)
			if len(results) == 0 {
				t.Fatalf("HandleInput(%q) returned empty results", tt.input)
			}
			result := results[0]
			if result.Handled != tt.wantHandled {
				t.Errorf("HandleInput(%q).Handled = %v, want %v", tt.input, result.Handled, tt.wantHandled)
			}
			if tt.wantHandled {
				if result.ActionArgs["host"] != tt.wantHost {
					t.Errorf("HandleInput(%q).ActionArgs[host] = %q, want %q", tt.input, result.ActionArgs["host"], tt.wantHost)
				}
				if result.ActionArgs["port"] != tt.wantPort {
					t.Errorf("HandleInput(%q).ActionArgs[port] = %q, want %q", tt.input, result.ActionArgs["port"], tt.wantPort)
				}
			}
		})
	}
}

func TestHandleInput_HelpResponse(t *testing.T) {
	h := NewHandler()
	results := h.HandleInput("/help")
	if len(results) == 0 {
		t.Fatal("HandleInput(/help) returned empty results")
	}
	result := results[0]

	if !result.IsLocal {
		t.Error("Expected /help to be a local command")
	}
	if !result.Handled {
		t.Error("Expected /help to be handled")
	}
	if result.Response == "" {
		t.Error("Expected /help to have a response")
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
		wantHandled  bool
		wantPattern  string
		wantResponse string
	}{
		{
			name:         "brace delimited trigger",
			input:        "/trigger {^Greetings (.*)} {say Hello $1}",
			wantHandled:  true,
			wantPattern:  "^Greetings (.*)",
			wantResponse: "say Hello $1",
		},
		{
			name:         "simple trigger without braces",
			input:        "/trigger hello world",
			wantHandled:  true,
			wantPattern:  "hello",
			wantResponse: "world",
		},
		{
			name:         "trigger with spaces in pattern",
			input:        "/trigger {You are hungry} {eat bread}",
			wantHandled:  true,
			wantPattern:  "You are hungry",
			wantResponse: "eat bread",
		},
		{
			name:        "missing arguments",
			input:       "/trigger",
			wantHandled: false,
		},
		{
			name:        "only pattern",
			input:       "/trigger {pattern}",
			wantHandled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := h.HandleInput(tt.input)
			if len(results) == 0 {
				t.Fatalf("HandleInput(%q) returned empty results", tt.input)
			}
			result := results[0]
			if result.Handled != tt.wantHandled {
				t.Errorf("HandleInput(%q).Handled = %v, want %v\nResponse: %s",
					tt.input, result.Handled, tt.wantHandled, result.Response)
				return
			}
			if tt.wantHandled {
				if result.Action != "trigger_add" {
					t.Errorf("HandleInput(%q).Action = %q, want 'trigger_add'", tt.input, result.Action)
				}
				if result.ActionArgs["pattern"] != tt.wantPattern {
					t.Errorf("HandleInput(%q) pattern = %q, want %q",
						tt.input, result.ActionArgs["pattern"], tt.wantPattern)
				}
				if result.ActionArgs["response"] != tt.wantResponse {
					t.Errorf("HandleInput(%q) response = %q, want %q",
						tt.input, result.ActionArgs["response"], tt.wantResponse)
				}
			}
		})
	}
}

func TestHandleInput_UntriggerCommand(t *testing.T) {
	h := NewHandler()

	tests := []struct {
		name        string
		input       string
		wantHandled bool
		wantPattern string
	}{
		{
			name:        "brace delimited untrigger",
			input:       "/untrigger {^Greetings (.*)}",
			wantHandled: true,
			wantPattern: "^Greetings (.*)",
		},
		{
			name:        "simple untrigger",
			input:       "/untrigger hello",
			wantHandled: true,
			wantPattern: "hello",
		},
		{
			name:        "untrigger with spaces in pattern",
			input:       "/untrigger {pattern with spaces}",
			wantHandled: true,
			wantPattern: "pattern with spaces",
		},
		{
			name:        "missing arguments",
			input:       "/untrigger",
			wantHandled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := h.HandleInput(tt.input)
			if len(results) == 0 {
				t.Fatalf("HandleInput(%q) returned empty results", tt.input)
			}
			result := results[0]
			if result.Handled != tt.wantHandled {
				t.Errorf("HandleInput(%q).Handled = %v, want %v\nResponse: %s",
					tt.input, result.Handled, tt.wantHandled, result.Response)
				return
			}
			if tt.wantHandled {
				if result.Action != "trigger_remove" {
					t.Errorf("HandleInput(%q).Action = %q, want 'trigger_remove'", tt.input, result.Action)
				}
				if result.ActionArgs["pattern"] != tt.wantPattern {
					t.Errorf("HandleInput(%q) pattern = %q, want %q",
						tt.input, result.ActionArgs["pattern"], tt.wantPattern)
				}
			}
		})
	}
}
