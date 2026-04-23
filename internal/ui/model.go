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

// StatusMsg is used to update the single-line command status in the UI border.
type StatusMsg struct {
	Message string
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
	statusMsg      string   // stores current status for border title
	showHelp       bool     // whether the help widget is visible

	// Channels for command routing
	SendChan  chan<- string              // Channel to send commands to server
	LocalChan chan<- input.CommandResult // Channel for local command actions

	// Callbacks
	resizeCallback func(width, height int) // called when terminal is resized
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
		if m.showHelp {
			if msg.Type == tea.KeyCtrlC {
				return m, tea.Quit
			}
			m.showHelp = false
			return m, nil
		}
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
					if result.Action == "show_help" {
						m.showHelp = true
						continue
					}
					if result.Response != "" {
						trimmed := strings.TrimSpace(result.Response)
						if strings.Contains(trimmed, "\n") {
							m.appendContent(result.Response + "\n")
						} else {
							m.statusMsg = trimmed
						}
					}
					// Send action to local command channel (non-blocking)
					if m.LocalChan != nil && result.Action != "" {
						select {
						case m.LocalChan <- result:
						default:
							// Buffer full, drop or log (should not happen with 128 buffer)
						}
					}
				} else if result.ServerText != "" || value == "" {
					// Server command - send to server (or empty if value was "")
					if m.SendChan != nil {
						// For empty value, send empty string to signal an "enter"
						textToSend := result.ServerText
						if value == "" {
							textToSend = ""
						}
						select {
						case m.SendChan <- textToSend:
						default:
							// Buffer full
						}
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

	case StatusMsg:
		trimmed := strings.TrimSpace(msg.Message)
		if strings.Contains(trimmed, "\n") {
			m.appendContent(msg.Message)
		} else {
			m.statusMsg = trimmed
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
		m.textinput.Width = m.width - 5

		// Notify resize callback so NAWS can update the server
		if m.resizeCallback != nil {
			m.resizeCallback(m.width, m.height)
		}
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
	distFromBottom := m.viewport.TotalLineCount() - (m.viewport.YOffset + m.viewport.Height)
	shouldAutoScroll := distFromBottom <= 1

	// Sanitize input: remove all carriage returns (CR / \r)
	text = strings.ReplaceAll(text, "\r", "")

	// If m.content is empty, start it.
	if len(m.content) == 0 {
		m.content = []string{""}
	}

	parts := strings.Split(text, "\n")

	// Update the current last line with the first part of new text
	lastIdx := len(m.content) - 1
	m.content[lastIdx] += parts[0]

	// Add any subsequent parts as new lines
	for i := 1; i < len(parts); i++ {
		m.content = append(m.content, parts[i])
	}

	// Optimization: limit total history to prevent performance degradation over time
	const maxHistory = 5000
	if len(m.content) > maxHistory {
		m.content = m.content[len(m.content)-maxHistory:]
	}

	// Update viewport content - this is still O(N) where N is history size,
	// but we've optimized the string manipulation before this point.
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

	if m.showHelp {
		return m.renderHelpWidget()
	}

	// Style for the viewport (no border)
	viewportStyle := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height - inputHeight)

	// Style for the input border
	border := lipgloss.RoundedBorder()
	inputStyle := lipgloss.NewStyle().
		Border(border).
		BorderTop(false).
		BorderForeground(lipgloss.Color("62")).
		Width(m.width - 2)

	topBorderWidth := m.width
	var topBorder strings.Builder
	topBorder.WriteString(border.TopLeft)

	titleStr := ""
	if m.statusMsg != "" {
		titleStr = " " + m.statusMsg + " "
	}

	titleWidth := lipgloss.Width(titleStr)
	available := topBorderWidth - 2 - 2 // Space for left/right corners + at least 2 dashes

	if titleWidth > available && available > 0 {
		if len(titleStr) > available {
			titleStr = " ..." + titleStr[len(titleStr)-(available-4):]
			titleWidth = lipgloss.Width(titleStr)
		}
	}

	if len(titleStr) > 0 && titleWidth <= available {
		remaining := topBorderWidth - 2 - titleWidth - 2
		if remaining > 0 {
			topBorder.WriteString(strings.Repeat(border.Top, remaining))
		}
		topBorder.WriteString(titleStr)
		topBorder.WriteString(strings.Repeat(border.Top, 2))
	} else {
		if topBorderWidth-2 > 0 {
			topBorder.WriteString(strings.Repeat(border.Top, topBorderWidth-2))
		}
	}
	topBorder.WriteString(border.TopRight)

	topBorderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("62"))

	// Build the view
	var b strings.Builder
	b.WriteString(viewportStyle.Render(m.viewport.View()))
	b.WriteString("\n")
	b.WriteString(topBorderStyle.Render(topBorder.String()))
	b.WriteString("\n")
	b.WriteString(inputStyle.Render(m.textinput.View()))

	return b.String()
}

// renderHelpWidget renders a centered help widget.
func (m Model) renderHelpWidget() string {
	helpWidth := m.width - 4
	if helpWidth > 78 {
		helpWidth = 78
	}

	border := lipgloss.RoundedBorder()
	widgetStyle := lipgloss.NewStyle().
		Border(border).
		BorderForeground(lipgloss.Color("62")).
		Background(lipgloss.Color("235")).
		Padding(1, 2).
		Width(helpWidth)

	helpContent := m.commandHandler.HelpText()
	widget := widgetStyle.Render(helpContent)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, widget)
}

// GetAliases returns the current aliases from the command handler.
func (m Model) GetAliases() map[string]string {
	return m.commandHandler.GetAliases()
}

// SetAliases loads aliases into the command handler.
func (m *Model) SetAliases(aliases map[string]string) {
	m.commandHandler.SetAliases(aliases)
}

// ClearAliases removes all aliases from the command handler.
func (m *Model) ClearAliases() {
	m.commandHandler.ClearAliases()
}

// GetHistory returns the current command history.
func (m Model) GetHistory() []string {
	return m.history.Commands()
}

// LoadHistory loads commands into the history.
func (m *Model) LoadHistory(commands []string) {
	m.history.SetCommands(commands)
}

// GetConnections returns the current connection aliases from the command handler.
func (m Model) GetConnections() map[string]input.ConnectionAlias {
	return m.commandHandler.GetConnections()
}

// SetConnections loads connection aliases into the command handler.
func (m *Model) SetConnections(connections map[string]input.ConnectionAlias) {
	m.commandHandler.SetConnections(connections)
}

// ClearConnections removes all connection aliases from the command handler.
func (m *Model) ClearConnections() {
	m.commandHandler.ClearConnections()
}

// SetResizeCallback sets the callback invoked when the terminal is resized.
func (m *Model) SetResizeCallback(callback func(width, height int)) {
	m.resizeCallback = callback
}

// Width returns the current terminal width.
func (m Model) Width() int {
	return m.width
}

// Height returns the current terminal height.
func (m Model) Height() int {
	return m.height
}
