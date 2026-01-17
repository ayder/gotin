package logic

// Processor handles incoming data from the network.
// It applies triggers and other transformations before passing data to the UI.
type Processor struct {
	triggerEngine *TriggerEngine
}

// NewProcessor creates a new Processor with the given TriggerEngine.
func NewProcessor(te *TriggerEngine) *Processor {
	return &Processor{
		triggerEngine: te,
	}
}

// ProcessLine processes a single line of incoming text.
// It checks triggers and returns the (potentially modified) line for display.
func (p *Processor) ProcessLine(line string) string {
	// Check triggers for this line
	if p.triggerEngine != nil {
		p.triggerEngine.CheckLine(line)
	}

	// Currently returns line unchanged; future: ANSI processing, gag triggers, etc.
	return line
}

// ProcessIncoming processes data received from the network before displaying.
// This is a legacy function maintained for compatibility.
// Deprecated: Use Processor.ProcessLine instead.
func ProcessIncoming(data []byte) []byte {
	return data
}
