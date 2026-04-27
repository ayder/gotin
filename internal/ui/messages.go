package ui

import "github.com/ayder/gotin/internal/mapper"

// MapPaneToggleMsg is sent by the app layer when the user issues
// `/map show`. The model flips visibility, recomputes split widths, and
// rejects the toggle if the terminal is too narrow.
type MapPaneToggleMsg struct{}

// MapPaneRecenterMsg is sent by the app layer after a mapper mutation
// (e.g. movement, dig) so the model can clear pan offset and snap the pane
// back to the (possibly new) current room.
type MapPaneRecenterMsg struct{}

// SetMapEngineSnapshotMsg installs a snapshot accessor on the model copy
// held by the running tea.Program. This must travel via tea.Program.Send
// rather than by calling Model.SetMapEngineSnapshot directly, because the
// program holds its own value-copy of the model.
type SetMapEngineSnapshotMsg struct {
	Fn func() (*mapper.Map, string)
}
