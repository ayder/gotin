package logic

import (
	"testing"
)

func TestTriggerEngine_AddTrigger(t *testing.T) {
	te := NewTriggerEngine(nil)

	// Valid pattern
	err := te.AddTrigger("hello", "world")
	if err != nil {
		t.Errorf("AddTrigger with valid pattern failed: %v", err)
	}
	if te.TriggerCount() != 1 {
		t.Errorf("Expected 1 trigger, got %d", te.TriggerCount())
	}

	// Invalid pattern
	err = te.AddTrigger("[invalid", "response")
	if err == nil {
		t.Error("AddTrigger with invalid pattern should return error")
	}
	if te.TriggerCount() != 1 {
		t.Errorf("Invalid pattern should not add trigger, got %d", te.TriggerCount())
	}
}

func TestTriggerEngine_CheckLine(t *testing.T) {
	tests := []struct {
		name             string
		pattern          string
		response         string
		line             string
		shouldMatch      bool
		expectedResponse string
	}{
		{
			name:             "simple match",
			pattern:          "hello",
			response:         "world",
			line:             "hello there",
			shouldMatch:      true,
			expectedResponse: "world",
		},
		{
			name:             "no match",
			pattern:          "hello",
			response:         "world",
			line:             "goodbye",
			shouldMatch:      false,
			expectedResponse: "",
		},
		{
			name:             "case insensitive match",
			pattern:          "(?i)welcome",
			response:         "Thank you",
			line:             "WELCOME to the game!",
			shouldMatch:      true,
			expectedResponse: "Thank you",
		},
		{
			name:             "regex match",
			pattern:          `You have \d+ gold`,
			response:         "rich",
			line:             "You have 100 gold coins",
			shouldMatch:      true,
			expectedResponse: "rich",
		},
		{
			name:             "word boundary",
			pattern:          `\bWelcome\b`,
			response:         "Thanks",
			line:             "Welcome back!",
			shouldMatch:      true,
			expectedResponse: "Thanks",
		},
		{
			name:             "word boundary no match",
			pattern:          `\bWelcome\b`,
			response:         "Thanks",
			line:             "Unwelcoming place",
			shouldMatch:      false,
			expectedResponse: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sentResponse string
			te := NewTriggerEngine(func(response string) {
				sentResponse = response
			})

			err := te.AddTrigger(tt.pattern, tt.response)
			if err != nil {
				t.Fatalf("AddTrigger failed: %v", err)
			}

			triggered := te.CheckLine(tt.line)

			if tt.shouldMatch {
				if len(triggered) != 1 {
					t.Errorf("Expected 1 triggered response, got %d", len(triggered))
				}
				if sentResponse != tt.expectedResponse {
					t.Errorf("Expected sent response %q, got %q", tt.expectedResponse, sentResponse)
				}
			} else {
				if len(triggered) != 0 {
					t.Errorf("Expected no triggered response, got %d", len(triggered))
				}
				if sentResponse != "" {
					t.Errorf("Expected no sent response, got %q", sentResponse)
				}
			}
		})
	}
}

func TestTriggerEngine_MultipleTriggers(t *testing.T) {
	var responses []string
	te := NewTriggerEngine(func(response string) {
		responses = append(responses, response)
	})

	te.AddTrigger("hello", "hi")
	te.AddTrigger("world", "earth")
	te.AddTrigger("test", "check")

	// Line matches two triggers
	triggered := te.CheckLine("hello world")
	if len(triggered) != 2 {
		t.Errorf("Expected 2 triggered responses, got %d", len(triggered))
	}
	if len(responses) != 2 {
		t.Errorf("Expected 2 sent responses, got %d", len(responses))
	}

	// Note: Immediate subsequent checks of the same triggers will be blocked by cooldown.
	// This is intentional loop prevention behavior.

	// Test no match - this should work regardless of cooldown
	responses = nil
	triggered = te.CheckLine("nothing matches")
	if len(triggered) != 0 {
		t.Errorf("Expected 0 triggered responses, got %d", len(triggered))
	}

	// Test that a different trigger (test) fires even though hello/world are on cooldown
	responses = nil
	triggered = te.CheckLine("test something")
	if len(triggered) != 1 {
		t.Errorf("Expected 1 triggered response for 'test', got %d", len(triggered))
	}
	if len(responses) != 1 || responses[0] != "check" {
		t.Errorf("Expected response 'check', got %v", responses)
	}
}

func TestTriggerEngine_NilSendFunc(t *testing.T) {
	te := NewTriggerEngine(nil)
	te.AddTrigger("hello", "world")

	// Should not panic with nil sendFunc
	triggered := te.CheckLine("hello there")
	if len(triggered) != 1 {
		t.Errorf("Expected 1 triggered response, got %d", len(triggered))
	}
}

func TestProcessor_ProcessLine(t *testing.T) {
	var sentResponse string
	te := NewTriggerEngine(func(response string) {
		sentResponse = response
	})
	te.AddTrigger("(?i)welcome", "Thank you")

	processor := NewProcessor(te)

	// Line that triggers
	result := processor.ProcessLine("Welcome to the game!")
	if result != "Welcome to the game!" {
		t.Errorf("ProcessLine should return original line, got %q", result)
	}
	if sentResponse != "Thank you" {
		t.Errorf("Expected trigger response 'Thank you', got %q", sentResponse)
	}

	// Line that doesn't trigger
	sentResponse = ""
	result = processor.ProcessLine("Goodbye!")
	if result != "Goodbye!" {
		t.Errorf("ProcessLine should return original line, got %q", result)
	}
	if sentResponse != "" {
		t.Errorf("Expected no trigger response, got %q", sentResponse)
	}
}

func TestProcessor_NilTriggerEngine(t *testing.T) {
	processor := NewProcessor(nil)

	// Should not panic with nil trigger engine
	result := processor.ProcessLine("hello world")
	if result != "hello world" {
		t.Errorf("ProcessLine should return original line, got %q", result)
	}
}

func TestTriggerEngine_RegexGroupSubstitution(t *testing.T) {
	tests := []struct {
		name             string
		pattern          string
		response         string
		line             string
		expectedResponse string
	}{
		{
			name:             "single group substitution",
			pattern:          `^Greetings (.*)`,
			response:         "say Hello $1",
			line:             "Greetings Traveler",
			expectedResponse: "say Hello Traveler",
		},
		{
			name:             "multiple group substitution",
			pattern:          `^(\w+) gives (\w+) to you`,
			response:         "say Thanks $1 for the $2",
			line:             "John gives sword to you",
			expectedResponse: "say Thanks John for the sword",
		},
		{
			name:             "no groups",
			pattern:          `You are hungry`,
			response:         "eat bread",
			line:             "You are hungry",
			expectedResponse: "eat bread",
		},
		{
			name:             "group with special characters",
			pattern:          `(\d+) gold coins`,
			response:         "deposit $1",
			line:             "You found 500 gold coins",
			expectedResponse: "deposit 500",
		},
		{
			name:             "named groups with ${name}",
			pattern:          `(?P<name>\w+) arrives`,
			response:         "wave ${name}",
			line:             "Alice arrives",
			expectedResponse: "wave Alice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sentResponse string
			te := NewTriggerEngine(func(response string) {
				sentResponse = response
			})

			err := te.AddTrigger(tt.pattern, tt.response)
			if err != nil {
				t.Fatalf("AddTrigger failed: %v", err)
			}

			triggered := te.CheckLine(tt.line)

			if len(triggered) != 1 {
				t.Errorf("Expected 1 triggered response, got %d", len(triggered))
			}
			if sentResponse != tt.expectedResponse {
				t.Errorf("Expected response %q, got %q", tt.expectedResponse, sentResponse)
			}
		})
	}
}

func TestTriggerEngine_LoopPrevention(t *testing.T) {
	var callCount int
	te := NewTriggerEngine(func(response string) {
		callCount++
	})

	// Add a trigger that could potentially loop
	te.AddTrigger("hello", "world")

	// First call should fire
	triggered := te.CheckLine("hello there")
	if len(triggered) != 1 {
		t.Errorf("First call should trigger, got %d triggers", len(triggered))
	}
	if callCount != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}

	// Immediate second call should be blocked by cooldown
	triggered = te.CheckLine("hello again")
	if len(triggered) != 0 {
		t.Errorf("Second immediate call should be blocked, got %d triggers", len(triggered))
	}
	if callCount != 1 {
		t.Errorf("Call count should still be 1, got %d", callCount)
	}
}

func TestTriggerEngine_DifferentTriggersNotBlocked(t *testing.T) {
	var responses []string
	te := NewTriggerEngine(func(response string) {
		responses = append(responses, response)
	})

	// Add two different triggers
	te.AddTrigger("hello", "hi")
	te.AddTrigger("world", "earth")

	// First trigger fires
	te.CheckLine("hello there")
	if len(responses) != 1 || responses[0] != "hi" {
		t.Errorf("First trigger should fire, got %v", responses)
	}

	// Different trigger should not be blocked
	te.CheckLine("world here")
	if len(responses) != 2 || responses[1] != "earth" {
		t.Errorf("Different trigger should not be blocked, got %v", responses)
	}
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no ANSI codes",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "single color code",
			input:    "\x1b[31mRed Text\x1b[0m",
			expected: "Red Text",
		},
		{
			name:     "multiple color codes",
			input:    "\x1b[1;32mBold Green\x1b[0m and \x1b[34mBlue\x1b[0m",
			expected: "Bold Green and Blue",
		},
		{
			name:     "complex ANSI sequence",
			input:    "\x1b[38;5;196mExtended Color\x1b[0m",
			expected: "Extended Color",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripANSI(tt.input)
			if result != tt.expected {
				t.Errorf("StripANSI(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestProcessor_ANSIStripping(t *testing.T) {
	var sentResponse string
	te := NewTriggerEngine(func(response string) {
		sentResponse = response
	})
	te.AddTrigger("(?i)welcome", "Thanks")

	processor := NewProcessor(te)

	// Line with ANSI codes - trigger should still match on stripped text
	coloredLine := "\x1b[32mWelcome\x1b[0m to the game!"
	result := processor.ProcessLine(coloredLine)

	// Original line should be returned unchanged
	if result != coloredLine {
		t.Errorf("ProcessLine should return original line with colors, got %q", result)
	}

	// Trigger should have fired (matching on stripped text)
	if sentResponse != "Thanks" {
		t.Errorf("Trigger should fire on ANSI-stripped text, got %q", sentResponse)
	}
}
