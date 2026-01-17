package input

import (
	"testing"
)

func TestAliasManager_SetAndGet(t *testing.T) {
	am := NewAliasManager()

	// Test setting and getting
	am.Set("k", "kill")
	value, ok := am.Get("k")
	if !ok {
		t.Error("Expected alias 'k' to exist")
	}
	if value != "kill" {
		t.Errorf("Expected 'kill', got %q", value)
	}

	// Test non-existent alias
	_, ok = am.Get("nonexistent")
	if ok {
		t.Error("Expected 'nonexistent' to not exist")
	}
}

func TestAliasManager_Delete(t *testing.T) {
	am := NewAliasManager()

	am.Set("k", "kill")
	if !am.Delete("k") {
		t.Error("Expected Delete to return true for existing alias")
	}

	_, ok := am.Get("k")
	if ok {
		t.Error("Expected alias 'k' to be deleted")
	}

	// Deleting non-existent should return false
	if am.Delete("nonexistent") {
		t.Error("Expected Delete to return false for non-existent alias")
	}
}

func TestAliasManager_List(t *testing.T) {
	am := NewAliasManager()

	am.Set("k", "kill")
	am.Set("l", "look")

	list := am.List()
	if len(list) != 2 {
		t.Errorf("Expected 2 aliases, got %d", len(list))
	}
	if list["k"] != "kill" {
		t.Errorf("Expected 'kill', got %q", list["k"])
	}
	if list["l"] != "look" {
		t.Errorf("Expected 'look', got %q", list["l"])
	}
}

func TestAliasManager_Expand(t *testing.T) {
	am := NewAliasManager()

	am.Set("k", "kill")
	am.Set("kk", "kill goblin")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple expansion",
			input:    "k rat",
			expected: "kill rat",
		},
		{
			name:     "multi-word alias",
			input:    "kk",
			expected: "kill goblin",
		},
		{
			name:     "no alias",
			input:    "say hello",
			expected: "say hello",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "alias only",
			input:    "k",
			expected: "kill",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := am.Expand(tt.input)
			if result != tt.expected {
				t.Errorf("Expand(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAliasManager_RecursiveExpansion(t *testing.T) {
	am := NewAliasManager()

	// Chain of aliases
	am.Set("a", "b")
	am.Set("b", "c")
	am.Set("c", "final command")

	result := am.Expand("a test")
	if result != "final command test" {
		t.Errorf("Expected 'final command test', got %q", result)
	}
}

func TestAliasManager_InfiniteLoopProtection(t *testing.T) {
	am := NewAliasManager()

	// Create a loop
	am.Set("a", "b")
	am.Set("b", "a")

	// Should not hang, should stop after MaxAliasExpansionDepth
	result := am.Expand("a")
	// Result should be either "a" or "b" depending on depth
	if result != "a" && result != "b" {
		t.Errorf("Unexpected result for infinite loop: %q", result)
	}
}

func TestHandler_AliasIntegration(t *testing.T) {
	h := NewHandler()

	// Set an alias
	result := h.HandleInput("/alias k kill")
	if !result.Handled {
		t.Error("Expected /alias to be handled")
	}

	// Use the alias
	result = h.HandleInput("k rat")
	if result.IsLocal {
		t.Error("Expected 'k rat' to be a server command")
	}
	if result.ServerText != "kill rat" {
		t.Errorf("Expected ServerText 'kill rat', got %q", result.ServerText)
	}
}

func TestHandler_AliasCommands(t *testing.T) {
	h := NewHandler()

	// Test /alias
	result := h.HandleInput("/alias hi say Hello!")
	if !result.Handled {
		t.Error("Expected /alias to be handled")
	}
	if result.Response != "Alias set: hi -> say Hello!" {
		t.Errorf("Unexpected response: %q", result.Response)
	}

	// Test /aliases
	result = h.HandleInput("/aliases")
	if !result.Handled {
		t.Error("Expected /aliases to be handled")
	}

	// Test /unalias
	result = h.HandleInput("/unalias hi")
	if !result.Handled {
		t.Error("Expected /unalias to be handled")
	}

	// Test removing non-existent alias
	result = h.HandleInput("/unalias nonexistent")
	if result.Handled {
		t.Error("Expected /unalias of non-existent to not be handled")
	}
}
