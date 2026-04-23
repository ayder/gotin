package network

// optState is the per-side state for one Telnet option.
// This is a simplified subset of RFC 1143's six-state Q Method, sufficient
// for a receive-side-only client that never initiates new offers.
type optState byte

const (
	optNo      optState = iota // option is not in effect
	optYes                     // option is in effect
	optWantNo                  // we asked to disable; awaiting ack
	optWantYes                 // we asked to enable; awaiting ack
)

// optionTable tracks him/us state for each option byte we care about.
type optionTable struct {
	him map[byte]optState // server-side state
	us  map[byte]optState // our-side state
}

func newOptionTable() *optionTable {
	return &optionTable{
		him: make(map[byte]optState),
		us:  make(map[byte]optState),
	}
}

func (ot *optionTable) getHim(opt byte) optState { return ot.him[opt] }
func (ot *optionTable) getUs(opt byte) optState  { return ot.us[opt] }
func (ot *optionTable) setHim(opt byte, s optState) {
	if s == optNo {
		delete(ot.him, opt)
		return
	}
	ot.him[opt] = s
}
func (ot *optionTable) setUs(opt byte, s optState) {
	if s == optNo {
		delete(ot.us, opt)
		return
	}
	ot.us[opt] = s
}
