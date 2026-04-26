package ui

// MapPaneToggleMsg is sent by the app layer when the user issues
// `/map show`. The model flips visibility, recomputes split widths, and
// rejects the toggle if the terminal is too narrow.
type MapPaneToggleMsg struct{}

// MapPaneRecenterMsg is sent by the app layer after a mapper mutation
// (e.g. movement, dig) so the model can clear pan offset and snap the pane
// back to the (possibly new) current room.
type MapPaneRecenterMsg struct{}
