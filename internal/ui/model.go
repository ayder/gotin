package ui

import (
	"strings"

	"dmud/internal/input"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SetLocalEchoMsg is sent by the network layer to toggle password mode.
// When LocalEcho is true, input is displayed normally.
// When LocalEcho is false, input is masked (password mode).
type SetLocalEchoMsg struct {
	LocalEcho bool
}

// NetworkDataMsg is sent when data arrives from the network.
type NetworkDataMsg struct {
	Data string
}

// SendCommandMsg is used to request sending a command to the server.
type SendCommandMsg struct {
	Command string
}

// LocalCommandMsg is sent when a local command is processed.
type LocalCommandMsg struct {
	Result input.CommandResult
}

// Model represents the TUI state for the MUD client.
type Model struct {
	viewport       viewport.Model
	textinput      textinput.Model
	history        *input.History
	commandHandler *input.Handler
	ready          bool
	width          int
	height         int
	content        []string // stores all lines displayed in viewport

	// Channels for command routing
	SendChan  chan<- string              // Channel to send commands to server
	LocalChan chan<- input.CommandResult // Channel for local command actions
}

// inputHeight is the fixed height for the input area (input line + border).
const inputHeight = 3

// New creates and returns a new Model with initialized components.
// sendChan is used to send commands to the server.
// localChan is used to send local command results for processing (quit, connect, etc.).
func New(sendChan chan<- string, localChan chan<- input.CommandResult) Model {
	ti := textinput.New()
	ti.Placeholder = "Enter command..."
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 80

	return Model{
		textinput:      ti,
		history:        input.NewHistory(),
		commandHandler: input.NewHandler(),
		SendChan:       sendChan,
		LocalChan:      localChan,
	}
}

// Init returns the initial command for the TUI.
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles incoming messages and updates the model state.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			value := m.textinput.Value()
			// Process even if value is empty
			m.history.Add(value)
			// Process input through command handler
			// HandleInput returns []CommandResult for multi-command alias support
			results := m.commandHandler.HandleInput(value)

			// Echo the original input first (for server commands)
			hasServerCommand := false
			for _, r := range results {
				if !r.IsLocal {
					hasServerCommand = true
					break
				}
			}

			if hasServerCommand {
				if m.textinput.EchoMode == textinput.EchoNormal {
					m.appendContent(value + "\n")
				} else {
					m.appendContent("\n")
				}
			}

			m.textinput.Reset()

			// Process each result
			for _, result := range results {
				if result.IsLocal {
					// Local command - display response and send action via channel
					if result.Response != "" {
						m.appendContent(result.Response + "\n")
					}
					// Send action to local command channel (non-blocking)
					if m.LocalChan != nil && result.Action != "" {
						go func(r input.CommandResult) {
							m.LocalChan <- r
						}(result)
					}
				} else if result.ServerText != "" || value == "" {
					// Server command - send to server (or empty if value was "")
					if m.SendChan != nil {
						// For empty value, send empty string to signal an "enter"
						textToSend := result.ServerText
						if value == "" {
							textToSend = ""
						}
						go func(cmd string) {
							m.SendChan <- cmd
						}(textToSend)
					}
				}
			}
			return m, nil
		case tea.KeyPgUp:
			m.viewport.ViewUp()
			return m, nil
		case tea.KeyPgDown:
			m.viewport.ViewDown()
			return m, nil
		case tea.KeyUp:
			if cmd, ok := m.history.Previous(); ok {
				m.textinput.SetValue(cmd)
				m.textinput.CursorEnd()
			}
			return m, nil
		case tea.KeyDown:
			if cmd, ok := m.history.Next(); ok {
				m.textinput.SetValue(cmd)
				m.textinput.CursorEnd()
			} else {
				// Past the end of history, clear input
				m.textinput.SetValue("")
			}
			return m, nil
		case tea.KeyHome:
			m.viewport.GotoTop()
			return m, nil
		case tea.KeyEnd:
			m.viewport.GotoBottom()
			return m, nil
		}

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.viewport.LineUp(3)
			return m, nil
		case tea.MouseButtonWheelDown:
			m.viewport.LineDown(3)
			return m, nil
		}

	case SetLocalEchoMsg:
		if msg.LocalEcho {
			m.textinput.EchoMode = textinput.EchoNormal
		} else {
			m.textinput.EchoMode = textinput.EchoPassword
		}
		return m, nil

	case NetworkDataMsg:
		m.appendContent(msg.Data)
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		viewportHeight := m.height - inputHeight

		if !m.ready {
			// First time receiving window size - initialize viewport
			m.viewport = viewport.New(m.width, viewportHeight)
			m.viewport.SetContent("")
			m.ready = true
		} else {
			// Resize existing viewport
			m.viewport.Width = m.width
			m.viewport.Height = viewportHeight
		}

		// Update text input width
		m.textinput.Width = m.width - 2
	}

	// Handle viewport updates
	// We block KeyMsg because we don't want vim bindings (j/k etc) to scroll
	// while typing. We only want Mouse and WindowSize for the viewport.
	switch msg.(type) {
	case tea.KeyMsg:
		// Do nothing for keys, textinput handles them
	default:
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Handle text input updates
	m.textinput, cmd = m.textinput.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// appendContent adds incoming text to the viewport content.
// It handles partial lines by appending to the last line if needed.
func (m *Model) appendContent(text string) {
	// Calculate distance from bottom before update
	// Standard AtBottom() can be strict. We allow a small buffer (1 line)
	// to account for partial updates or off-by-one rendering issues.
	distFromBottom := m.viewport.TotalLineCount() - (m.viewport.YOffset + m.viewport.Height)
	shouldAutoScroll := distFromBottom <= 1

	// Sanitize input: remove all carriage returns (CR / \r)
	text = strings.ReplaceAll(text, "\r", "")

	// If m.content is empty, start it.
	if len(m.content) == 0 {
		m.content = []string{""}
	}

	parts := strings.Split(text, "\n")

	// Pending part
	lastIdx := len(m.content) - 1
	m.content[lastIdx] += parts[0]

	for i := 1; i < len(parts); i++ {
		m.content = append(m.content, parts[i])
	}

	m.viewport.SetContent(strings.Join(m.content, "\n"))

	// Auto-scroll if we were near the bottom
	if shouldAutoScroll {
		m.viewport.GotoBottom()
	}
}

// View renders the TUI.
func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	// Style for the viewport (no border)
	viewportStyle := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height - inputHeight)

	// Style for the input border
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(m.width - 2)

	// Build the view
	var b strings.Builder
	b.WriteString(viewportStyle.Render(m.viewport.View()))
	b.WriteString("\n")
	b.WriteString(inputStyle.Render(m.textinput.View()))

	return b.String()
}

// GetAliases returns the current aliases from the command handler.
func (m Model) GetAliases() map[string]string {
	return m.commandHandler.GetAliases()
}

// SetAliases loads aliases into the command handler.
func (m *Model) SetAliases(aliases map[string]string) {
	m.commandHandler.SetAliases(aliases)
}
