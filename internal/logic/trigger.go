package logic

import (
	"regexp"
	"sync"
	"time"
)

// MinTriggerInterval is the minimum time between consecutive firings of the same trigger.
// This prevents infinite loops where a trigger's response triggers itself.
const MinTriggerInterval = 200 * time.Millisecond

// Trigger represents a pattern-response pair.
// When the Pattern matches incoming text, Response is sent to the server.
type Trigger struct {
	Pattern  *regexp.Regexp
	Response string
}

// TriggerEngine manages a list of triggers and checks incoming lines against them.
type TriggerEngine struct {
	triggers     []Trigger
	sendFunc     func(string) // Callback to send responses to the network
	lastFired    map[string]time.Time
	lastFiredMux sync.Mutex
}

// NewTriggerEngine creates a new TriggerEngine with the given send function.
// The sendFunc is called whenever a trigger matches and needs to send a response.
func NewTriggerEngine(sendFunc func(string)) *TriggerEngine {
	return &TriggerEngine{
		triggers:  make([]Trigger, 0),
		sendFunc:  sendFunc,
		lastFired: make(map[string]time.Time),
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
// Implements loop prevention: each trigger has a minimum 200ms cooldown between firings.
func (te *TriggerEngine) CheckLine(line string) []string {
	var triggered []string
	now := time.Now()

	for _, t := range te.triggers {
		patternKey := t.Pattern.String()

		// Check cooldown for loop prevention
		te.lastFiredMux.Lock()
		lastTime, exists := te.lastFired[patternKey]
		if exists && now.Sub(lastTime) < MinTriggerInterval {
			// Trigger is on cooldown, skip it
			te.lastFiredMux.Unlock()
			continue
		}
		te.lastFiredMux.Unlock()

		// Find match indices for expansion
		loc := t.Pattern.FindStringSubmatchIndex(line)
		if loc != nil {
			// Expand the response template with captured groups
			// ExpandString appends to the first arg, so we pass nil to start fresh
			expanded := t.Pattern.ExpandString(nil, t.Response, line, loc)
			response := string(expanded)

			// Update last fired time
			te.lastFiredMux.Lock()
			te.lastFired[patternKey] = now
			te.lastFiredMux.Unlock()

			triggered = append(triggered, response)
			if te.sendFunc != nil {
				te.sendFunc(response)
			}
		}
	}
	return triggered
}

// RemoveTrigger removes a trigger by its exact pattern string.
func (te *TriggerEngine) RemoveTrigger(pattern string) bool {
	for i, t := range te.triggers {
		if t.Pattern.String() == pattern {
			// Remove element
			te.triggers = append(te.triggers[:i], te.triggers[i+1:]...)
			return true
		}
	}
	return false
}

// ListTriggers returns all registered triggers.
func (te *TriggerEngine) ListTriggers() []Trigger {
	// Return a copy to avoid mutation
	list := make([]Trigger, len(te.triggers))
	copy(list, te.triggers)
	return list
}

// TriggerCount returns the number of registered triggers.
func (te *TriggerEngine) TriggerCount() int {
	return len(te.triggers)
}
