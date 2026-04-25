package parser

import (
	"reflect"
	"testing"
)

func TestParseBraceArgs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "two brace-delimited args",
			input:    "{k $1} {kill $1; skin corpse}",
			expected: []string{"k $1", "kill $1; skin corpse"},
		},
		{
			name:     "nested braces",
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
			name:     "complex pattern with semicolons",
			input:    "{setup} {stand; wear all; look}",
			expected: []string{"setup", "stand; wear all; look"},
		},
		{
			name:     "pattern with variables",
			input:    "{hi $1} {say Hello $1; smile $1}",
			expected: []string{"hi $1", "say Hello $1; smile $1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseBraceArgs(tt.input)
			if err != nil {
				t.Errorf("ParseBraceArgs(%q) returned error: %v", tt.input, err)
				return
			}
			if len(result) == 0 && len(tt.expected) == 0 {
				return // Both empty, pass
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("ParseBraceArgs(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSplitCommands(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple split",
			input:    "kill rat; skin corpse",
			expected: []string{"kill rat", "skin corpse"},
		},
		{
			name:     "three commands",
			input:    "stand; wear all; look",
			expected: []string{"stand", "wear all", "look"},
		},
		{
			name:     "no semicolon",
			input:    "kill rat",
			expected: []string{"kill rat"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
		{
			name:     "semicolon in quotes",
			input:    `say "hello; goodbye"; smile`,
			expected: []string{`say "hello; goodbye"`, "smile"},
		},
		{
			name:     "trailing semicolon",
			input:    "kill rat; skin corpse;",
			expected: []string{"kill rat", "skin corpse"},
		},
		{
			name:     "leading semicolon",
			input:    "; kill rat; skin corpse",
			expected: []string{"kill rat", "skin corpse"},
		},
		{
			name:     "multiple spaces around semicolon",
			input:    "kill rat  ;  skin corpse",
			expected: []string{"kill rat", "skin corpse"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SplitCommands(tt.input)
			if len(result) == 0 && len(tt.expected) == 0 {
				return // Both empty, pass
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("SplitCommands(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
