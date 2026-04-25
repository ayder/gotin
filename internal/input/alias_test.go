package input

import (
	"reflect"
	"testing"

	"github.com/ayder/gotin/internal/command"
)

func TestAliasManager_SetAndGet(t *testing.T) {
	am := NewAliasManager()

	// Test setting and getting with new pattern format
	am.Set("k $1", "kill $1")
	alias, ok := am.Get("k")
	if !ok {
		t.Error("Expected alias 'k' to exist")
	}
	if alias.Pattern != "k $1" {
		t.Errorf("Expected pattern 'k $1', got %q", alias.Pattern)
	}
	if alias.Expansion != "kill $1" {
		t.Errorf("Expected expansion 'kill $1', got %q", alias.Expansion)
	}

	// Test non-existent alias
	_, ok = am.Get("nonexistent")
	if ok {
		t.Error("Expected 'nonexistent' to not exist")
	}
}

func TestAliasManager_Delete(t *testing.T) {
	am := NewAliasManager()

	am.Set("k $1", "kill $1")
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

	am.Set("k $1", "kill $1")
	am.Set("l", "look")

	list := am.List()
	if len(list) != 2 {
		t.Errorf("Expected 2 aliases, got %d", len(list))
	}
	if list["k $1"] != "kill $1" {
		t.Errorf("Expected 'kill $1', got %q", list["k $1"])
	}
	if list["l"] != "look" {
		t.Errorf("Expected 'look', got %q", list["l"])
	}
}

func TestAliasManager_Expand_Simple(t *testing.T) {
	am := NewAliasManager()

	am.Set("k", "kill")
	am.Set("l", "look")

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple expansion",
			input:    "k rat",
			expected: []string{"kill rat"},
		},
		{
			name:     "alias only",
			input:    "k",
			expected: []string{"kill"},
		},
		{
			name:     "no alias",
			input:    "say hello",
			expected: []string{"say hello"},
		},
		{
			name:     "empty input",
			input:    "",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := am.Expand(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Expand(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAliasManager_Expand_Variables(t *testing.T) {
	am := NewAliasManager()

	am.Set("k $1", "kill $1; skin corpse")
	am.Set("hi $1", "say Hello $1; smile $1")

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "variable substitution with multi-command",
			input:    "k rat",
			expected: []string{"kill rat", "skin corpse"},
		},
		{
			name:     "multiple variable references",
			input:    "hi Bob",
			expected: []string{"say Hello Bob", "smile Bob"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := am.Expand(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Expand(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAliasManager_Expand_MultiCommand(t *testing.T) {
	am := NewAliasManager()

	am.Set("setup", "stand; wear all; look")

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "multi-command expansion",
			input:    "setup",
			expected: []string{"stand", "wear all", "look"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := am.Expand(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Expand(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAliasManager_Expand_LocalCommandInAlias(t *testing.T) {
	am := NewAliasManager()

	am.Set("bye", "say goodbye; /quit")

	result := am.Expand("bye")
	expected := []string{"say goodbye", "/quit"}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expand(bye) = %v, want %v", result, expected)
	}
}

func TestAliasManager_InfiniteLoopProtection(t *testing.T) {
	am := NewAliasManager()

	// Create a loop
	am.Set("a", "b")
	am.Set("b", "a")

	// Should not hang, should stop after MaxAliasExpansionDepth
	result := am.Expand("a")
	// Should return something (either "a" or "b" depending on depth)
	if len(result) == 0 {
		t.Error("Expected non-empty result for infinite loop protection")
	}
}

func TestAliasManager_RecursiveExpansion(t *testing.T) {
	am := NewAliasManager()

	// Chain of aliases
	am.Set("a", "b")
	am.Set("b", "c")
	am.Set("c", "final command")

	result := am.Expand("a test")
	expected := []string{"final command test"}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Recursive expansion: got %v, want %v", result, expected)
	}
}

func TestHandler_AdvancedAliasCommand(t *testing.T) {
	h := NewHandler()

	// Test new brace-delimited alias syntax
	results := h.HandleInput("/alias add {k $1} {kill $1; skin corpse}")
	if len(results) == 0 {
		t.Fatal("Expected /alias to return results")
	}
	result := results[0]
	if result.Command == nil {
		t.Errorf("Expected /alias to produce a command, got response: %s", result.Response)
	}

	// Use the alias
	results = h.HandleInput("k rat")
	if len(results) != 2 {
		t.Errorf("Expected 2 results from 'k rat', got %d: %v", len(results), results)
		return
	}

	if results[0].ServerText != "kill rat" {
		t.Errorf("Expected first command 'kill rat', got %q", results[0].ServerText)
	}
	if results[1].ServerText != "skin corpse" {
		t.Errorf("Expected second command 'skin corpse', got %q", results[1].ServerText)
	}
}

func TestHandler_AliasWithLocalCommand(t *testing.T) {
	h := NewHandler()

	// Set alias that includes a local command
	h.HandleInput("/alias add {bye} {say goodbye; /quit}")

	// Use the alias
	results := h.HandleInput("bye")
	if len(results) != 2 {
		t.Errorf("Expected 2 results from 'bye', got %d: %v", len(results), results)
		return
	}

	// First command is server command
	if results[0].Command != nil {
		t.Error("Expected first command to be server command")
	}
	if results[0].ServerText != "say goodbye" {
		t.Errorf("Expected 'say goodbye', got %q", results[0].ServerText)
	}

	// Second command is local command
	if results[1].Command == nil {
		t.Error("Expected second command to be local command")
	}
	if _, ok := results[1].Command.(*command.Quit); !ok {
		t.Errorf("Expected *command.Quit, got %T", results[1].Command)
	}
}

func TestHandler_AliasCommands(t *testing.T) {
	h := NewHandler()

	// Test /alias with brace syntax
	results := h.HandleInput("/alias add {hi $1} {say Hello $1; smile $1}")
	if len(results) == 0 || results[0].Command == nil {
		t.Error("Expected /alias add to produce a command")
	}

	// Test /alias list
	results = h.HandleInput("/alias list")
	if len(results) == 0 || results[0].Command == nil {
		t.Error("Expected /alias list to produce a command")
	}

	// Test /alias remove
	results = h.HandleInput("/alias remove hi")
	if len(results) == 0 || results[0].Command == nil {
		t.Error("Expected /alias remove to produce a command")
	}

	// Test removing non-existent alias
	results = h.HandleInput("/alias remove nonexistent")
	if len(results) == 0 || results[0].Command != nil {
		t.Error("Expected /alias remove of non-existent to not produce a command")
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		input    string
		expected map[string]string
	}{
		{
			name:    "simple variable",
			pattern: "k $1",
			input:   "k rat",
			expected: map[string]string{
				"$1": "rat",
				"$*": "",
			},
		},
		{
			name:    "multiple variables",
			pattern: "give $1 $2",
			input:   "give sword player",
			expected: map[string]string{
				"$1": "sword",
				"$2": "player",
				"$*": "",
			},
		},
		{
			name:    "no variables with extra args",
			pattern: "setup",
			input:   "setup extra args",
			expected: map[string]string{
				"$*": "extra args",
				"$1": "extra",
				"$2": "args",
			},
		},
		{
			name:     "no match - different trigger",
			pattern:  "k $1",
			input:    "kill rat",
			expected: nil,
		},
		{
			name:    "many positional args sets $10 correctly",
			pattern: "test",
			input:   "test a b c d e f g h i j",
			expected: map[string]string{
				"$*": "a b c d e f g h i j",
				"$1": "a",
				"$2": "b",
				"$9": "i",
				"$10": "j",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchPattern(tt.pattern, tt.input)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("matchPattern(%q, %q) = %v, want nil", tt.pattern, tt.input, result)
				}
				return
			}
			if result == nil {
				t.Errorf("matchPattern(%q, %q) = nil, want %v", tt.pattern, tt.input, tt.expected)
				return
			}
			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("matchPattern(%q, %q)[%q] = %q, want %q", tt.pattern, tt.input, k, result[k], v)
				}
			}
		})
	}
}

func TestSubstituteVariables(t *testing.T) {
	tests := []struct {
		name      string
		expansion string
		vars      map[string]string
		expected  string
	}{
		{
			name:      "single variable",
			expansion: "kill $1",
			vars:      map[string]string{"$1": "rat"},
			expected:  "kill rat",
		},
		{
			name:      "multiple same variable",
			expansion: "say Hello $1; smile $1",
			vars:      map[string]string{"$1": "Bob"},
			expected:  "say Hello Bob; smile Bob",
		},
		{
			name:      "multiple different variables",
			expansion: "give $1 to $2",
			vars:      map[string]string{"$1": "sword", "$2": "player"},
			expected:  "give sword to player",
		},
		{
			name:      "all remaining args",
			expansion: "say $*",
			vars:      map[string]string{"$*": "hello world"},
			expected:  "say hello world",
		},
		{
			name:      "double-digit variable $10",
			expansion: "say $10",
			vars:      map[string]string{"$10": "tenth"},
			expected:  "say tenth",
		},
		{
			name:      "$10 does not partially replace $1",
			expansion: "$1 and $10",
			vars:      map[string]string{"$1": "first", "$10": "tenth"},
			expected:  "first and tenth",
		},
		{
			name:      "$1 does not leave $10 as 0",
			expansion: "$10",
			vars:      map[string]string{"$1": "first", "$10": "tenth"},
			expected:  "tenth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := substituteVariables(tt.expansion, tt.vars)
			if result != tt.expected {
				t.Errorf("substituteVariables(%q, %v) = %q, want %q",
					tt.expansion, tt.vars, result, tt.expected)
			}
		})
	}
}
