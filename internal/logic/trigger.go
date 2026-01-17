package logic

import "regexp"

// Trigger represents a pattern-response pair.
// When the Pattern matches incoming text, Response is sent to the server.
type Trigger struct {
	Pattern  *regexp.Regexp
	Response string
}

// TriggerEngine manages a list of triggers and checks incoming lines against them.
type TriggerEngine struct {
	triggers []Trigger
	sendFunc func(string) // Callback to send responses to the network
}

// NewTriggerEngine creates a new TriggerEngine with the given send function.
// The sendFunc is called whenever a trigger matches and needs to send a response.
func NewTriggerEngine(sendFunc func(string)) *TriggerEngine {
	return &TriggerEngine{
		triggers: make([]Trigger, 0),
		sendFunc: sendFunc,
	}
}

// AddTrigger adds a new trigger to the engine.
// Returns an error if the pattern is invalid.
func (te *TriggerEngine) AddTrigger(pattern string, response string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	te.triggers = append(te.triggers, Trigger{
		Pattern:  re,
		Response: response,
	})
	return nil
}

// CheckLine checks a line against all triggers.
// If a trigger matches, its response is sent via the sendFunc.
// Returns a list of responses that were triggered (for logging/testing).
func (te *TriggerEngine) CheckLine(line string) []string {
	var triggered []string
	for _, t := range te.triggers {
		if t.Pattern.MatchString(line) {
			triggered = append(triggered, t.Response)
			if te.sendFunc != nil {
				te.sendFunc(t.Response)
			}
		}
	}
	return triggered
}

// TriggerCount returns the number of registered triggers.
func (te *TriggerEngine) TriggerCount() int {
	return len(te.triggers)
}
