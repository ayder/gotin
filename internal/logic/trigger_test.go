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

	// Reset and test single match
	responses = nil
	triggered = te.CheckLine("just hello")
	if len(triggered) != 1 {
		t.Errorf("Expected 1 triggered response, got %d", len(triggered))
	}

	// Reset and test no match
	responses = nil
	triggered = te.CheckLine("nothing matches")
	if len(triggered) != 0 {
		t.Errorf("Expected 0 triggered responses, got %d", len(triggered))
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
