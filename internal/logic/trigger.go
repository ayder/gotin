package logic

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

// containsFast provides a fast check if a literal string is present in the line.
func containsFast(line, literal string) bool {
	return strings.Contains(line, literal)
}

// MinTriggerInterval is the minimum time between consecutive firings of the same trigger.
// This prevents infinite loops where a trigger's response triggers itself.
const MinTriggerInterval = 200 * time.Millisecond

// Trigger represents a pattern-response pair.
// When the Pattern matches incoming text, Response is sent to the server.
type Trigger struct {
	Pattern       *regexp.Regexp
	Response      string
	LiteralPrefix string // Optimization: literal prefix of the regex
}

// TriggerEngine manages a list of triggers and checks incoming lines against them.
type TriggerEngine struct {
	triggers     []Trigger
	triggersMux  sync.RWMutex
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
	literal, _ := re.LiteralPrefix()

	te.triggersMux.Lock()
	te.triggers = append(te.triggers, Trigger{
		Pattern:       re,
		Response:      response,
		LiteralPrefix: literal,
	})
	te.triggersMux.Unlock()
	return nil
}

// CheckLine checks a line against all triggers.
// If a trigger matches, its response is sent via the sendFunc.
// Returns a list of responses that were triggered (for logging/testing).
// Implements loop prevention: each trigger has a minimum 200ms cooldown between firings.
func (te *TriggerEngine) CheckLine(line string) []string {
	var triggered []string
	now := time.Now()

	te.triggersMux.RLock()
	snapshot := make([]Trigger, len(te.triggers))
	copy(snapshot, te.triggers)
	te.triggersMux.RUnlock()

	for _, t := range snapshot {
		if t.LiteralPrefix != "" && !containsFast(line, t.LiteralPrefix) {
			continue
		}
		patternKey := t.Pattern.String()

		te.lastFiredMux.Lock()
		lastTime, exists := te.lastFired[patternKey]
		if exists && now.Sub(lastTime) < MinTriggerInterval {
			te.lastFiredMux.Unlock()
			continue
		}
		te.lastFiredMux.Unlock()

		loc := t.Pattern.FindStringSubmatchIndex(line)
		if loc != nil {
			expanded := t.Pattern.ExpandString(nil, t.Response, line, loc)
			response := string(expanded)

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
	te.triggersMux.Lock()
	defer te.triggersMux.Unlock()
	for i, t := range te.triggers {
		if t.Pattern.String() == pattern {
			te.triggers = append(te.triggers[:i], te.triggers[i+1:]...)
			return true
		}
	}
	return false
}

// ListTriggers returns all registered triggers.
func (te *TriggerEngine) ListTriggers() []Trigger {
	te.triggersMux.RLock()
	defer te.triggersMux.RUnlock()
	list := make([]Trigger, len(te.triggers))
	copy(list, te.triggers)
	return list
}

// TriggerCount returns the number of registered triggers.
func (te *TriggerEngine) TriggerCount() int {
	te.triggersMux.RLock()
	defer te.triggersMux.RUnlock()
	return len(te.triggers)
}

// ClearTriggers removes all registered triggers.
func (te *TriggerEngine) ClearTriggers() {
	te.triggersMux.Lock()
	te.triggers = make([]Trigger, 0)
	te.triggersMux.Unlock()

	te.lastFiredMux.Lock()
	te.lastFired = make(map[string]time.Time)
	te.lastFiredMux.Unlock()
}
