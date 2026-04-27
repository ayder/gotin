package mapper

import (
	"fmt"
	"regexp"
)

// MUDProfile drives the structural block parser. Defaults are tuned for
// t2tmud.org (see docs/t2tmud_mxp.md). Override per-host via gotin.json
// mud_profiles.<host>; absent fields fall back to the in-code default.
type MUDProfile struct {
	Host             string   `json:"host"`
	BlockStartTag    string   `json:"block_start_tag"`
	BlockEndPattern  string   `json:"block_end_pattern"`
	ExitTags         []string `json:"exit_tags"`
	PresenceTags     []string `json:"presence_tags"`
	WeatherPatterns  []string `json:"weather_patterns"`
	DoorStatePattern string   `json:"door_state_pattern"`
}

// CompiledProfile holds compiled regex versions of a MUDProfile's patterns
// for hot-path use.
type CompiledProfile struct {
	Profile  MUDProfile
	EndRe    *regexp.Regexp
	WeatherR []*regexp.Regexp
	DoorR    *regexp.Regexp
}

// DefaultT2TMUDProfile is the in-code default profile.
func DefaultT2TMUDProfile() MUDProfile {
	return MUDProfile{
		Host:          "t2tmud.org",
		BlockStartTag: "expire",
		// ANSI-tolerant prompt: tolerates \r\n line endings and any number of
		// ANSI/MXP escapes between the newline and "HP:". [WA] or
		// [\x1b[1;32mWA\x1b[0m] both match.
		BlockEndPattern: `\r?\n(?:\x1b\[[0-9;]*[A-Za-z])*HP:\d+ EP:\d+ \[(?:\x1b\[[0-9;]*[A-Za-z])*\w+(?:\x1b\[[0-9;]*[A-Za-z])*\] > `,
		ExitTags:        []string{"x", "xx", "t", "w", "ww", "l", "ll", "y", "yy", "z"},
		PresenceTags:    []string{"i30", "i9"},
		WeatherPatterns: []string{
			`^The sky is .+\.$`,
			`^A .+ sky .+\.$`,
		},
		DoorStatePattern: `^The .+ (door|gate|portcullis) is (open|closed|locked|broken|sealed|barred)\.$`,
	}
}

// MergeProfile returns a new profile in which every field of over that is
// non-zero replaces the corresponding field of base. Slice fields are
// replaced wholesale when non-empty.
func MergeProfile(base, over MUDProfile) MUDProfile {
	out := base
	if over.Host != "" {
		out.Host = over.Host
	}
	if over.BlockStartTag != "" {
		out.BlockStartTag = over.BlockStartTag
	}
	if over.BlockEndPattern != "" {
		out.BlockEndPattern = over.BlockEndPattern
	}
	if len(over.ExitTags) > 0 {
		out.ExitTags = append([]string(nil), over.ExitTags...)
	}
	if len(over.PresenceTags) > 0 {
		out.PresenceTags = append([]string(nil), over.PresenceTags...)
	}
	if len(over.WeatherPatterns) > 0 {
		out.WeatherPatterns = append([]string(nil), over.WeatherPatterns...)
	}
	if over.DoorStatePattern != "" {
		out.DoorStatePattern = over.DoorStatePattern
	}
	return out
}

// Compile builds the regex companions; returns an error if any pattern is
// malformed.
func (p MUDProfile) Compile() (*CompiledProfile, error) {
	endRe, err := regexp.Compile(p.BlockEndPattern)
	if err != nil {
		return nil, fmt.Errorf("block_end_pattern: %w", err)
	}
	weatherR := make([]*regexp.Regexp, 0, len(p.WeatherPatterns))
	for i, pat := range p.WeatherPatterns {
		r, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("weather_patterns[%d]: %w", i, err)
		}
		weatherR = append(weatherR, r)
	}
	var doorR *regexp.Regexp
	if p.DoorStatePattern != "" {
		doorR, err = regexp.Compile(p.DoorStatePattern)
		if err != nil {
			return nil, fmt.Errorf("door_state_pattern: %w", err)
		}
	}
	return &CompiledProfile{
		Profile:  p,
		EndRe:    endRe,
		WeatherR: weatherR,
		DoorR:    doorR,
	}, nil
}
