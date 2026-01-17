package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"dmud/internal/input"
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
	SendChan  chan<- string             // Channel to send commands to server
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
			if value != "" {
				m.history.Add(value)
				// Process input through command handler
				result := m.commandHandler.HandleInput(value)
				if result.IsLocal {
					// Local command - display response and send action via channel
					if result.Response != "" {
						m.appendContent(result.Response)
					}
					m.textinput.Reset()
					// Send action to local command channel (non-blocking)
					if m.LocalChan != nil && result.Action != "" {
						go func(r input.CommandResult) {
							m.LocalChan <- r
						}(result)
					}
					return m, nil
				}
				// Server command - echo input and send expanded text to server via channel
				m.appendContent(value)
				m.textinput.Reset()
				if m.SendChan != nil {
					// Send the alias-expanded text to server
					serverText := result.ServerText
					go func(cmd string) {
						m.SendChan <- cmd
					}(serverText)
				}
				return m, nil
			}
			m.textinput.Reset()
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
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	// Handle text input updates
	m.textinput, cmd = m.textinput.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// appendContent adds a line to the viewport content.
// Only auto-scrolls to bottom if the user was already at the bottom.
func (m *Model) appendContent(line string) {
	// Check if user is at the bottom before adding content
	atBottom := m.viewport.AtBottom()

	m.content = append(m.content, line)
	m.viewport.SetContent(strings.Join(m.content, "\n"))

	// Only scroll to bottom if user was already there
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// View renders the TUI.
func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	// Style for the viewport border
	viewportStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(m.width - 2).
		Height(m.height - inputHeight - 2)

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
