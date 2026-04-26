package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/ayder/gotin/internal/command"
	"github.com/ayder/gotin/internal/input"
	"github.com/ayder/gotin/internal/mapper"
	"github.com/ayder/gotin/internal/ui/mappane"

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

// ProtocolsMsg updates the badges row under the input box with the names of
// the currently-negotiated MUD protocols (e.g. {"GMCP", "MXP"}). An empty or
// nil slice clears the row.
type ProtocolsMsg struct {
	Active []string
}

// RoomNameMsg updates the room-name badge under the input box.
// Sent when GMCP Room.Info is received. An empty string clears the badge.
type RoomNameMsg struct {
	Name string
}

// renderTickMsg schedules a coalesced viewport render.
type renderTickMsg struct{}

// ConfirmConnectMsg is sent by the app layer when a /connect command is
// issued while already connected. The UI shows a confirmation widget.
type ConfirmConnectMsg struct {
	CurrentHost string
	CurrentPort int
	Host        string
	Port        int
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
	content          []string // stores all lines displayed in viewport
	statusMsg        string   // stores current status for border title
	activeProtocols  []string // negotiated MUD protocols, rendered as badges under the input box
	currentRoomName  string   // last room name received from GMCP Room.Info
	showHelp       bool     // whether the help widget is visible
	helpViewport   viewport.Model // scrollable help content
	helpReady      bool     // helpViewport sized at least once
	showConfirmConnect bool   // whether the connection confirmation widget is visible
	confirmHost    string   // pending connection host for confirmation widget
	confirmPort    int      // pending connection port for confirmation widget
	confirmCurrentHost string // current connection host for confirmation widget
	confirmCurrentPort int    // current connection port for confirmation widget
	contentDirty   bool     // set when appendContent modifies m.content but viewport hasn't been updated yet
	pendingTick    bool     // true while a renderTickMsg is queued

	// Map pane (side-by-side renderer for /map show).
	mapPaneVisible    bool
	mapPaneWidth      int
	mapPanOffset      mappane.Point
	mapPaneLayerKey   string
	mapPaneLastCurrID string
	mapEngineSnapshot func() (*mapper.Map, string)

	// Channels for command routing
	SendChan  chan<- string         // Channel to send commands to server
	LocalChan chan<- command.Command // Channel for local command actions

	// Callbacks
	resizeCallback func(width, height int) // called when terminal is resized
}

// InputHeight is the fixed height for the input area (input line + borders)
// plus the protocol badges row rendered below it. Exported so the network
// layer can report the content height (terminal height - chrome) via NAWS.
const InputHeight = 4

// New creates and returns a new Model with initialized components.
// sendChan is used to send commands to the server.
// localChan is used to send local command results for processing (quit, connect, etc.).
func New(sendChan chan<- string, localChan chan<- command.Command) Model {
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

// SetMapEngineSnapshot wires a snapshot accessor into the model. The
// accessor must be safe for concurrent use; it is invoked from the model's
// View method and also from MapPaneRecenterMsg handling.
func (m *Model) SetMapEngineSnapshot(fn func() (*mapper.Map, string)) {
	m.mapEngineSnapshot = fn
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
		if m.showConfirmConnect {
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			case tea.KeyEnter, tea.KeySpace:
				m.showConfirmConnect = false
				if m.LocalChan != nil {
					select {
					case m.LocalChan <- &command.ConfirmConnect{}:
					default:
					}
				}
				return m, nil
			case tea.KeyEsc:
				m.showConfirmConnect = false
				if m.LocalChan != nil {
					select {
					case m.LocalChan <- &command.CancelConnect{}:
					default:
					}
				}
				return m, nil
			default:
				switch msg.String() {
				case "y", "Y":
					m.showConfirmConnect = false
					if m.LocalChan != nil {
						select {
						case m.LocalChan <- &command.ConfirmConnect{}:
						default:
						}
					}
					return m, nil
				case "n", "N":
					m.showConfirmConnect = false
					if m.LocalChan != nil {
						select {
						case m.LocalChan <- &command.CancelConnect{}:
						default:
						}
					}
					return m, nil
				}
			}
			return m, nil
		}
		if m.showHelp {
			if msg.Type == tea.KeyCtrlC {
				return m, tea.Quit
			}
			switch msg.Type {
			case tea.KeyEsc:
				m.showHelp = false
				return m, nil
			}
			switch msg.String() {
			case "q", "?":
				m.showHelp = false
				return m, nil
			}
			// Anything else: route to the help viewport so the user can scroll
			// (up/down/pgup/pgdn/home/end/j/k all handled by viewport).
			var hcmd tea.Cmd
			m.helpViewport, hcmd = m.helpViewport.Update(msg)
			return m, hcmd
		}
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyCtrlH:
			m.showHelp = !m.showHelp
			if m.showHelp {
				m.ensureHelpViewport()
			}
			return m, nil
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
				if r.Command == nil && !r.ShowHelp {
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

			// Schedule a tick if appendContent made content dirty
			var tickCmd tea.Cmd
			if m.contentDirty && !m.pendingTick {
				m.pendingTick = true
				tickCmd = scheduleRenderTick()
			}
			if tickCmd != nil {
				cmds = append(cmds, tickCmd)
			}

			// Process each result
			for _, result := range results {
				if result.ShowHelp {
					m.showHelp = true
					m.ensureHelpViewport()
					continue
				}
				if result.Command != nil {
					// Local command - display response and send command via channel
					if result.Response != "" {
						trimmed := strings.TrimSpace(result.Response)
						if strings.Contains(trimmed, "\n") {
							m.appendContent(result.Response + "\n")
							// Schedule a tick for this appendContent call
							if !m.pendingTick {
								m.pendingTick = true
								cmds = append(cmds, scheduleRenderTick())
							}
						} else {
							m.statusMsg = trimmed
						}
					}
					// Send command to local command channel (non-blocking)
					if m.LocalChan != nil {
						select {
						case m.LocalChan <- result.Command:
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
			return m, tea.Batch(cmds...)

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

	case ConfirmConnectMsg:
		m.showConfirmConnect = true
		m.confirmCurrentHost = msg.CurrentHost
		m.confirmCurrentPort = msg.CurrentPort
		m.confirmHost = msg.Host
		m.confirmPort = msg.Port
		return m, nil

	case MapPaneToggleMsg:
		if !m.mapPaneVisible {
			pw := computeMapPaneWidth(m.width)
			if pw == 0 {
				m.statusMsg = "terminal too narrow for /map show"
				return m, nil
			}
			m.mapPaneVisible = true
			m.mapPaneWidth = pw
			m.viewport.Width = m.width - pw - 1
			m.mapPanOffset = mappane.Point{}
			m.mapPaneLayerKey = ""
		} else {
			m.mapPaneVisible = false
			m.mapPaneWidth = 0
			m.viewport.Width = m.width
		}
		return m, nil

	case MapPaneRecenterMsg:
		m.mapPanOffset = mappane.Point{}
		m.mapPaneLayerKey = ""
		return m, nil

	case SetLocalEchoMsg:
		if msg.LocalEcho {
			m.textinput.EchoMode = textinput.EchoNormal
		} else {
			m.textinput.EchoMode = textinput.EchoPassword
		}
		return m, nil

	case renderTickMsg:
		m.flushViewport()
		m.pendingTick = false
		if m.contentDirty {
			return m, scheduleRenderTick()
		}
		return m, nil

	case StatusMsg:
		trimmed := strings.TrimSpace(msg.Message)
		if strings.Contains(trimmed, "\n") {
			m.appendContent(msg.Message)
			var tickCmd tea.Cmd
			if !m.pendingTick {
				m.pendingTick = true
				tickCmd = scheduleRenderTick()
			}
			return m, tickCmd
		}
		m.statusMsg = trimmed
		return m, nil

	case ProtocolsMsg:
		m.activeProtocols = append(m.activeProtocols[:0], msg.Active...)
		return m, nil

	case RoomNameMsg:
		m.currentRoomName = msg.Name
		return m, nil

	case NetworkDataMsg:
		m.appendContent(msg.Data)
		var tickCmd tea.Cmd
		if !m.pendingTick {
			m.pendingTick = true
			tickCmd = scheduleRenderTick()
		}
		return m, tickCmd

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		viewportHeight := m.height - InputHeight
		viewportW := m.width
		if m.mapPaneVisible {
			pw := computeMapPaneWidth(m.width)
			if pw == 0 {
				m.mapPaneVisible = false
				m.mapPaneWidth = 0
			} else {
				m.mapPaneWidth = pw
				viewportW = m.width - pw - 1
			}
		}

		if !m.ready {
			// First time receiving window size - initialize viewport
			m.viewport = viewport.New(viewportW, viewportHeight)
			m.viewport.SetContent("")
			m.ready = true
		} else {
			// Resize existing viewport
			m.viewport.Width = viewportW
			m.viewport.Height = viewportHeight
		}

		// Resize the help widget's viewport too if it has been initialised.
		if m.helpReady {
			m.helpViewport.Width = helpInnerWidth
			m.helpViewport.Height = helpBodyHeight(m.height)
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

// appendContent mutates m.content with incoming text. It does NOT touch the
// viewport directly; a renderTickMsg coalesces the actual SetContent call.
func (m *Model) appendContent(text string) {
	if len(m.content) == 0 {
		m.content = []string{""}
	}
	parts := strings.Split(text, "\n")
	lastIdx := len(m.content) - 1
	m.content[lastIdx] += parts[0]
	for i := 1; i < len(parts); i++ {
		m.content = append(m.content, parts[i])
	}
	const maxHistory = 5000
	if len(m.content) > maxHistory {
		m.content = m.content[len(m.content)-maxHistory:]
	}
	m.contentDirty = true
}

// flushViewport pushes m.content into the viewport in one pass and
// auto-scrolls if we were near the bottom. Safe to call when nothing has
// changed (no-op on !contentDirty).
func (m *Model) flushViewport() {
	if !m.contentDirty {
		return
	}
	distFromBottom := m.viewport.TotalLineCount() - (m.viewport.YOffset + m.viewport.Height)
	shouldAutoScroll := distFromBottom <= 1

	m.viewport.SetContent(strings.Join(m.content, "\n"))
	if shouldAutoScroll {
		m.viewport.GotoBottom()
	}
	m.contentDirty = false
}

// scheduleRenderTick returns a Cmd that fires a renderTickMsg after ~16ms.
func scheduleRenderTick() tea.Cmd {
	return tea.Tick(16*time.Millisecond, func(time.Time) tea.Msg {
		return renderTickMsg{}
	})
}

// View renders the TUI.
func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}
	// Value receiver: take a local copy and flush it so we render the
	// current state even between render ticks.
	mm := m
	mm.flushViewport()

	if mm.showHelp {
		return mm.renderHelpWidget()
	}

	// Style for the viewport (no border)
	viewportStyle := lipgloss.NewStyle().
		Width(mm.width).
		Height(mm.height - InputHeight)

	// Style for the input border
	border := lipgloss.RoundedBorder()
	inputStyle := lipgloss.NewStyle().
		Border(border).
		BorderTop(false).
		BorderForeground(lipgloss.Color("62")).
		Width(mm.width - 2)

	topBorderWidth := mm.width
	var topBorder strings.Builder
	topBorder.WriteString(border.TopLeft)

	titleStr := ""
	if mm.statusMsg != "" {
		titleStr = " " + mm.statusMsg + " "
	}

	titleWidth := lipgloss.Width(titleStr)
	available := topBorderWidth - 2 - 2 // Space for left/right corners + at least 2 dashes

	if titleWidth > available && available > 0 {
		if len(titleStr) > available {
			titleStr = titleStr[:available-4] + "... "
			titleWidth = lipgloss.Width(titleStr)
		}
	}

	if len(titleStr) > 0 && titleWidth <= available {
		remaining := topBorderWidth - 2 - titleWidth - 2
		topBorder.WriteString(strings.Repeat(border.Top, 2))
		topBorder.WriteString(titleStr)
		if remaining > 0 {
			topBorder.WriteString(strings.Repeat(border.Top, remaining))
		}
	} else {
		if topBorderWidth-2 > 0 {
			topBorder.WriteString(strings.Repeat(border.Top, topBorderWidth-2))
		}
	}
	topBorder.WriteString(border.TopRight)

	topBorderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("62"))

	// Build the view
	var b strings.Builder
	b.WriteString(viewportStyle.Render(mm.viewport.View()))
	b.WriteString("\n")
	b.WriteString(topBorderStyle.Render(topBorder.String()))
	b.WriteString("\n")
	b.WriteString(inputStyle.Render(mm.textinput.View()))
	b.WriteString("\n")
	b.WriteString(mm.renderProtocolBadges())

	view := b.String()

	if mm.mapPaneVisible {
		view = mm.composeMapSplit(view)
	}

	if mm.showConfirmConnect {
		return mm.renderConfirmConnectWidget(view)
	}

	return view
}

// composeMapSplit joins the existing left view with a freshly rendered map
// pane on the right. The left block is split into its viewport area and the
// status/input/badges block; only the viewport gets compressed horizontally.
func (mm Model) composeMapSplit(leftView string) string {
	if mm.mapEngineSnapshot == nil {
		return leftView
	}
	snap, currID := mm.mapEngineSnapshot()
	paneW := mm.mapPaneWidth
	paneH := mm.height - InputHeight
	pane := mappane.Render(mappane.View{
		PaneWidth:  paneW,
		PaneHeight: paneH,
		Map:        snap,
		CurrentID:  currID,
		PanOffset:  mm.mapPanOffset,
		LayerKey:   mm.mapPaneLayerKey,
	})

	// Split leftView into the viewport block (top, paneH lines) and the rest.
	lines := strings.SplitN(leftView, "\n", paneH+1)
	if len(lines) < paneH+1 {
		// Not enough lines to cleanly compose; fall back to leftView.
		return leftView
	}
	top := strings.Join(lines[:paneH], "\n")
	rest := lines[paneH]

	separator := strings.Repeat("│\n", paneH-1) + "│"
	joined := lipgloss.JoinHorizontal(lipgloss.Top, top, separator, pane)
	return joined + "\n" + rest
}

// renderProtocolBadges renders a single line of [NAME] badges for each
// currently-negotiated MUD protocol. Returns an empty line (spaces) when
// nothing is active so the layout stays stable.
func (m Model) renderProtocolBadges() string {
	sep := " "
	var parts []string
	var plainParts []string

	// Yellow room-name badge (from GMCP Room.Info)
	if m.currentRoomName != "" {
		roomBadge := lipgloss.NewStyle().
			Foreground(lipgloss.Color("220")).
			Bold(true).
			Render("[Room: " + m.currentRoomName + "]")
		parts = append(parts, roomBadge)
		plainParts = append(plainParts, "[Room: "+m.currentRoomName+"]")
	}

	// Green protocol badges
	if len(m.activeProtocols) > 0 {
		protoStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("82")).
			Bold(true)
		for _, name := range m.activeProtocols {
			parts = append(parts, protoStyle.Render("["+name+"]"))
			plainParts = append(plainParts, "["+name+"]")
		}
	}

	if len(parts) == 0 {
		return strings.Repeat(" ", m.width)
	}

	line := strings.Join(parts, sep)
	plain := strings.Join(plainParts, sep)
	if pad := m.width - lipgloss.Width(plain); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return line
}

// helpWidgetWidth is the fixed visible width of the help widget.
const helpWidgetWidth = 80

// helpInnerWidth is the column width used for the help body and footer
// (helpWidgetWidth minus the 2-cell padding on each side and 2 border cells).
const helpInnerWidth = helpWidgetWidth - 2*2 - 2

// helpBodyHeight returns the vertical space available for the scrollable
// help body, given a terminal height. Reserves room for borders, padding,
// the footer hint, and a screen margin.
func helpBodyHeight(termHeight int) int {
	maxBody := termHeight - 8
	if maxBody < 6 {
		maxBody = 6
	}
	body := maxBody - 2 // footer + spacer
	if body < 4 {
		body = 4
	}
	return body
}

// renderHelpWidget renders a centered, scrollable, single-column help widget
// fixed at 80 visible columns. Sizing and content of m.helpViewport are
// established in Update (resize / showHelp toggle); this function only
// composes the frame around the current viewport state.
func (m Model) renderHelpWidget() string {
	footer := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Width(helpInnerWidth).
		Render("↑/↓ PgUp/PgDn  Home/End  Esc/q to close")

	content := lipgloss.JoinVertical(lipgloss.Left, m.helpViewport.View(), "", footer)

	widgetStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Background(lipgloss.Color("235")).
		Padding(1, 2).
		Width(helpWidgetWidth)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, widgetStyle.Render(content))
}

// ensureHelpViewport (re)sizes m.helpViewport to match the current terminal
// dimensions and reloads its content from the command handler. Idempotent.
func (m *Model) ensureHelpViewport() {
	bodyHeight := helpBodyHeight(m.height)
	if !m.helpReady {
		m.helpViewport = viewport.New(helpInnerWidth, bodyHeight)
		m.helpReady = true
	} else {
		m.helpViewport.Width = helpInnerWidth
		m.helpViewport.Height = bodyHeight
	}
	m.helpViewport.SetContent(m.commandHandler.HelpText())
	m.helpViewport.GotoTop()
}

// renderConfirmConnectWidget renders a centered confirmation dialog over the
// normal view when the user tries to /connect while already connected.
func (m Model) renderConfirmConnectWidget(background string) string {
	confirmWidth := m.width - 12
	if confirmWidth > 80 {
		confirmWidth = 80
	}
	if confirmWidth < 40 {
		confirmWidth = 40
	}

	paddingX := 2

	var lines []string
	lines = append(lines, "Already connected to "+m.confirmCurrentHost+":"+strconv.Itoa(m.confirmCurrentPort))
	lines = append(lines, "")
	lines = append(lines, "Disconnect and connect to "+m.confirmHost+":"+strconv.Itoa(m.confirmPort)+"?")
	lines = append(lines, "")
	lines = append(lines, "[Enter/Y] Confirm    [Esc/N] Cancel")

	content := strings.Join(lines, "\n")

	border := lipgloss.RoundedBorder()
	widgetStyle := lipgloss.NewStyle().
		Border(border).
		BorderForeground(lipgloss.Color("208")).
		Background(lipgloss.Color("235")).
		Padding(1, paddingX).
		Width(confirmWidth)

	widget := widgetStyle.Render(content)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, widget, lipgloss.WithWhitespaceChars(" "))
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

// computeMapPaneWidth returns the requested pane width given a terminal
// width, applying the spec's clamp and minimum-chat-width rule. Returns 0
// when the pane should not be shown.
func computeMapPaneWidth(termW int) int {
	if termW < 65 {
		return 0
	}
	w := (termW + 2) / 3 // ceil(termW / 3)
	if w < 24 {
		w = 24
	}
	if w > 48 {
		w = 48
	}
	return w
}
