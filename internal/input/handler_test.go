package input

import (
	"testing"
)

func TestHandleInput_LocalCommand(t *testing.T) {
	h := NewHandler()

	tests := []struct {
		name      string
		input     string
		wantLocal bool
		wantAction string
	}{
		{
			name:      "quit command",
			input:     "/quit",
			wantLocal: true,
			wantAction: "quit",
		},
		{
			name:      "q alias for quit",
			input:     "/q",
			wantLocal: true,
			wantAction: "quit",
		},
		{
			name:      "help command",
			input:     "/help",
			wantLocal: true,
			wantAction: "",
		},
		{
			name:      "connect command with args",
			input:     "/connect example.com 1234",
			wantLocal: true,
			wantAction: "connect",
		},
		{
			name:      "server command",
			input:     "say hello",
			wantLocal: false,
			wantAction: "",
		},
		{
			name:      "empty input",
			input:     "",
			wantLocal: false,
			wantAction: "",
		},
		{
			name:      "unknown local command",
			input:     "/unknown",
			wantLocal: true,
			wantAction: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := h.HandleInput(tt.input)
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
			result := h.HandleInput(tt.input)
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
	result := h.HandleInput("/help")

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
