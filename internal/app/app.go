package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ayder/gotin/internal/command"
	"github.com/ayder/gotin/internal/config"
	"github.com/ayder/gotin/internal/input"
	"github.com/ayder/gotin/internal/logic"
	"github.com/ayder/gotin/internal/mapper"
	"github.com/ayder/gotin/internal/mudproto/gmcp"
	"github.com/ayder/gotin/internal/mudproto/mccp2"
	"github.com/ayder/gotin/internal/mudproto/mxp"
	"github.com/ayder/gotin/internal/mudproto/protolog"
	"github.com/ayder/gotin/internal/network"
	"github.com/ayder/gotin/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

// Options holds the command-line flags passed to the application.
type Options struct {
	Host  string
	Port  int
	Debug bool
}

// gotinData represents the JSON structure used by /save and /load commands.
type gotinData struct {
	Aliases        map[string]string                `json:"aliases,omitempty"`
	Triggers       []config.TriggerConfig           `json:"triggers,omitempty"`
	Connections    map[string]input.ConnectionAlias `json:"connections,omitempty"`
	MappingOptions *mapper.MappingOptions           `json:"mapping_options,omitempty"`
	MUDProfiles    map[string]mapper.MUDProfile     `json:"mud_profiles,omitempty"`
}

const gotinDataFilename = "config.json"

// protoFactories is the canonical name → factory map.
var protoFactories = map[string]func() network.Protocol{
	"GMCP":  func() network.Protocol { return gmcp.New(nil) },
	"MCCP2": func() network.Protocol { return mccp2.New() },
	"MXP":   func() network.Protocol { return mxp.New() },
}

func defaultOn(name string) bool {
	switch name {
	case "GMCP", "MXP":
		return true
	}
	return false
}

func installProtocols(c *network.Client, cfg map[string]bool) {
	for name, factory := range protoFactories {
		enabled, explicit := cfg[name]
		if !explicit {
			enabled = defaultOn(name)
		}
		if !enabled {
			continue
		}
		if err := c.Install(factory()); err != nil {
			fmt.Printf("[proto] install %s failed: %v\n", name, err)
		}
	}
}

func findMXP(c *network.Client) *mxp.Protocol {
	if c == nil {
		return nil
	}
	for _, p := range c.Installed() {
		if m, ok := p.(*mxp.Protocol); ok {
			m.SetContext(c.Context())
			return m
		}
	}
	return nil
}

func findGMCP(c *network.Client) *gmcp.Protocol {
	if c == nil {
		return nil
	}
	for _, p := range c.Installed() {
		if g, ok := p.(*gmcp.Protocol); ok {
			return g
		}
	}
	return nil
}

func protoList(c *network.Client) string {
	installed := map[string]bool{}
	if c != nil {
		for _, p := range c.Installed() {
			installed[p.Name()] = true
		}
	}
	names := make([]string, 0, len(protoFactories))
	for name := range protoFactories {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("Protocols:\n")
	for _, n := range names {
		state := "off"
		if installed[n] {
			state = "on"
		}
		fmt.Fprintf(&b, "  %-6s %s\n", n, state)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Run starts the application and blocks until it exits.
func Run(ctx context.Context, opts Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	s := &Session{opts: opts, ctx: ctx, cancel: cancel}

	// 1. Load Configuration
	cfgMgr, err := config.NewManager()
	if err != nil {
		return fmt.Errorf("failed to initialize config manager: %w", err)
	}
	s.cfgMgr = cfgMgr

	cfg, err := cfgMgr.Load()
	if err != nil {
		log.Println("Error loading config:", err)
	}
	s.cfg = cfg

	s.cfgSaver = config.NewDebouncedSaver(500*time.Millisecond, func(v any) error {
		c, ok := v.(config.Config)
		if !ok {
			return nil
		}
		return cfgMgr.Save(c)
	})
	defer s.cfgSaver.Flush()

	// 2. Setup Channels
	s.sendChan = make(chan string, 128)
	s.localChan = make(chan command.Command, 128)

	// 3. Initialize UI Model
	s.model = ui.New(s.sendChan, s.localChan)
	s.model.SetAliases(cfg.Aliases)

	// Load command history
	if historyPath, err := historyFilePath(); err == nil {
		if cmds, err := loadHistory(historyPath); err == nil {
			s.model.LoadHistory(cmds)
		}
	}

	// 4. Resize callback
	s.model.SetResizeCallback(func(w, h int) {
		s.termW, s.termH = w, h
		contentH := h - ui.InputHeight
		if c := s.client.Load(); c != nil {
			c.DebugLogf("[app] resize: raw=%dx%d content=%dx%d", w, h, w, contentH)
			c.SetWindowSize(w, contentH)
			c.SendNAWS()
		} else {
			log.Printf("[app] resize: raw=%dx%d content=%dx%d (no client)", w, h, w, contentH)
		}
	})

	// 5. Initialize Bubble Tea Program
	s.program = tea.NewProgram(s.model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	// 6. UI delivery pump channel
	s.uiMsgChan = make(chan tea.Msg, 1024)

	// 7. First run welcome
	firstRun := !cfgMgr.Exists()
	if firstRun && opts.Host == "" {
		s.trySendUI(ui.StatusMsg{
			Message: "Welcome to Gotin!\n" +
				"It looks like this is your first time here.\n" +
				"Type /connect <host> <port> to start playing.\n" +
				"Example: /connect t2tmud.org 9999\n",
		})
	}

	// 8. Shared logic components
	s.te = logic.NewTriggerEngine(s.sendToNet)
	s.mapEngine = mapper.NewEngine("")
	// The accessor must reach the program's value-copy of the model, so we
	// send it as a tea.Msg via the UI pump after the program is running.
	// Direct mutation of s.model would only update the local Session field.
	mapSnap := func() (*mapper.Map, string) { return s.mapEngine.Snapshot() }
	s.trySendUI(ui.SetMapEngineSnapshotMsg{Fn: mapSnap})
	defaultMappingOptions := mapper.DefaultMappingOptions()
	s.mappingOptionsTop = &defaultMappingOptions

	if opts.Debug {
		if f, err := os.OpenFile("gotin.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
			defer f.Close()
			s.protoLog = protolog.NewJSONLinesLogger(f, func() bool { return s.opts.Debug })
		} else {
			log.Printf("protolog: open gotin.log: %v", err)
		}
	}

	// Load triggers from config
	for _, t := range cfg.Triggers {
		s.te.AddTrigger(t.Pattern, t.Response)
	}

	// Auto-load project-level Gotin configuration.
	s.loadGotinData()

	// 9. Start goroutines
	var wg sync.WaitGroup
	s.handleLocalCommands(&wg)
	s.handleOutgoing(&wg)

	// 10. Auto-Connect
	if opts.Host != "" && opts.Port != 0 {
		go s.connect(opts.Host, opts.Port)
	} else if s.autoConnectFromAlias {
		go s.connect(s.autoConnectAlias.Host, s.autoConnectAlias.Port)
	} else if cfg.LastHost != "" && !firstRun {
		s.trySendUI(ui.StatusMsg{Message: fmt.Sprintf("Last connected to: %s:%d. Type /connect to reconnect.\n", cfg.LastHost, cfg.LastPort)})
	}

	// 11. Run UI
	progDone := make(chan error, 1)
	go func() {
		_, err := s.program.Run()
		progDone <- err
	}()

	// Start UI delivery pump now that Run() is active
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case msg := <-s.uiMsgChan:
				s.program.Send(msg)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for program to finish
	if err := <-progDone; err != nil {
		return fmt.Errorf("program error: %w", err)
	}

	// 12. Shutdown: cancel context, drain goroutines, save state
	cancel()
	wg.Wait()

	// Save map (best-effort)
	if s.mapEngine != nil {
		_ = s.mapEngine.Save()
	}

	// Cleanup: save history
	if historyPath, err := historyFilePath(); err == nil {
		_ = saveHistory(historyPath, s.model.GetHistory())
	}

	return nil
}

// Session holds all runtime state and callback bridges.
type Session struct {
	opts Options

	ctx    context.Context
	cancel context.CancelFunc

	cfgMgr   *config.Manager
	cfgSaver *config.DebouncedSaver
	cfg      config.Config

	model     ui.Model
	program   *tea.Program
	uiMsgChan chan tea.Msg
	uiMux     sync.Mutex

	client       atomic.Pointer[network.Client]
	termW, termH int

	te        *logic.TriggerEngine
	mapEngine *mapper.Engine
	protoLog  *protolog.JSONLinesLogger
	roomBuf   *mapper.RoomBlockBuffer

	sendChan  chan string
	localChan chan command.Command

	readLoopDone chan struct{} // closed when active ReadLoop exits

	autoConnectFromAlias bool
	autoConnectAlias     input.ConnectionAlias

	mappingOptionsScope string
	mappingOptionsAlias string
	mappingOptionsTop   *mapper.MappingOptions
	mudProfiles         map[string]mapper.MUDProfile

	currentHost string
	currentPort int
	pendingHost string
	pendingPort int
}

func (s *Session) trySendUI(msg tea.Msg) {
	s.uiMux.Lock()
	defer s.uiMux.Unlock()
	if data, ok := msg.(ui.NetworkDataMsg); ok {
		select {
		case s.uiMsgChan <- data:
			return
		default:
		}
		select {
		case prev := <-s.uiMsgChan:
			if pd, ok := prev.(ui.NetworkDataMsg); ok {
				combined := ui.NetworkDataMsg{Data: pd.Data + data.Data}
				select {
				case s.uiMsgChan <- combined:
				default:
				}
				return
			}
			select {
			case s.uiMsgChan <- prev:
			default:
			}
			return
		default:
			select {
			case s.uiMsgChan <- data:
			default:
			}
			return
		}
	}
	select {
	case s.uiMsgChan <- msg:
	default:
	}
}

func (s *Session) sendToNet(msg string) {
	if c := s.client.Load(); c != nil {
		c.Send([]byte(appendCRLF(msg)))
	}
}

func appendCRLF(msg string) string {
	if strings.HasSuffix(msg, "\r\n") {
		return msg
	}
	return msg + "\r\n"
}

func (s *Session) scheduleSave() {
	s.cfg.Aliases = s.model.GetAliases()
	triggers := s.te.ListTriggers()
	s.cfg.Triggers = make([]config.TriggerConfig, len(triggers))
	for i, t := range triggers {
		s.cfg.Triggers[i] = config.TriggerConfig{
			Pattern:  t.Pattern.String(),
			Response: t.Response,
		}
	}
	s.cfgSaver.Schedule(s.cfg)
}

func (s *Session) connect(h string, port int) {
	s.trySendUI(ui.StatusMsg{Message: fmt.Sprintf("Connecting to %s:%d...\n", h, port)})

	c, err := network.Connect(h, port)
	if err != nil {
		s.trySendUI(ui.StatusMsg{Message: fmt.Sprintf("Connection failed: %v\n", err)})
		return
	}

	c.SetDebug(s.opts.Debug)
	contentH := s.termH - ui.InputHeight
	c.DebugLogf("[app] connect initial size: raw=%dx%d content=%dx%d", s.termW, s.termH, s.termW, contentH)
	c.SetWindowSize(s.termW, contentH)
	c.SetProtocolStatusCallback(func(active []string) {
		s.trySendUI(ui.ProtocolsMsg{Active: active})
	})
	installProtocols(c, s.autoConnectAlias.Protocols)

	resolved, scope, aliasName := s.resolveMappingOptions(h, port)
	s.mappingOptionsScope = scope
	s.mappingOptionsAlias = aliasName
	s.mapEngine.SetMappingOptions(resolved)

	profile := s.resolveMUDProfile(h)
	s.mapEngine.SetMUDProfile(profile)
	s.mapEngine.ResetBlockStart()
	s.roomBuf = nil

	s.installProtocolLogger(c)

	if g := findGMCP(c); g != nil {
		g.SetCallback(func(pkg string, payload []byte) {
			s.onGMCPRoomInfo(pkg, payload)
		})
	}

	s.configureMXP(c)

	if old := s.client.Swap(c); old != nil {
		old.SetDisconnectCallback(nil)
		old.Close()
	}

	if s.termW > 0 && s.termH > 0 {
		contentH := s.termH - ui.InputHeight
		c.DebugLogf("[app] post-connect NAWS re-send: raw=%dx%d content=%dx%d", s.termW, s.termH, s.termW, contentH)
		c.SetWindowSize(s.termW, contentH)
		c.SendNAWS()
	} else {
		c.DebugLogf("[app] post-connect: term size unknown (%dx%d), skipping NAWS re-send", s.termW, s.termH)
	}

	lb := logic.NewLineBuffer()
	proc := logic.NewProcessor(s.te)

	c.SetEchoCallback(func(enabled bool) {
		s.trySendUI(ui.SetLocalEchoMsg{LocalEcho: enabled})
	})

	c.SetDataCallback(func(data string) {
		s.onData(c, lb, proc, data)
	})

	c.SetDisconnectCallback(func(reason error) {
		s.onDisconnect(reason)
	})

	// Wait for previous ReadLoop to finish before starting new one
	if s.readLoopDone != nil {
		select {
		case <-s.readLoopDone:
		case <-time.After(2 * time.Second):
		}
	}

	done := make(chan struct{})
	s.readLoopDone = done

	go func() {
		defer close(done)
		defer c.Close()
		c.ReadLoop()
	}()

	s.trySendUI(ui.StatusMsg{Message: "Connected!\n"})
	s.cfg.LastHost = h
	s.cfg.LastPort = port
	s.currentHost = h
	s.currentPort = port
	s.pendingHost = ""
	s.pendingPort = 0
	s.scheduleSave()
}

func (s *Session) onData(c *network.Client, lb *logic.LineBuffer, proc *logic.Processor, data string) {
	if m := findMXP(c); m != nil {
		data = m.Filter(data)
	}
	s.trySendUI(ui.NetworkDataMsg{Data: data})

	logicLines := lb.Feed([]byte(data))
	for _, line := range logicLines {
		proc.ProcessLine(line)
	}
}

func (s *Session) onDisconnect(reason error) {
	var msg string
	var ne net.Error
	if reason == nil {
		msg = "\nConnection closed (server disconnected).\n"
	} else if errors.As(reason, &ne) && ne.Timeout() {
		msg = "\nConnection closed (read timeout; server may be unresponsive).\n"
	} else {
		msg = fmt.Sprintf("\nConnection closed: %v\n", reason)
	}
	s.trySendUI(ui.StatusMsg{Message: msg})
	s.trySendUI(ui.ProtocolsMsg{Active: nil})
	s.trySendUI(ui.RoomNameMsg{})
	s.mapEngine.ResetBlockStart()
	if s.roomBuf != nil {
		s.roomBuf.Reset()
	}
	s.currentHost = ""
	s.currentPort = 0
	s.pendingHost = ""
	s.pendingPort = 0
}

func (s *Session) onGMCPRoomInfo(pkg string, payload []byte) {
	if pkg != "Room.Info" {
		return
	}
	room, err := gmcp.ParseRoomInfo(payload)
	if err != nil {
		return
	}
	if s.protoLog != nil && s.protoLog.Enabled() {
		s.protoLog.Log(protolog.Entry{
			Source: "gmcp",
			Dir:    "rx",
			Event:  "room_info",
			UTF8:   protolog.EncodeUTF8(payload),
			Hex:    protolog.EncodeHex(payload),
			Parsed: map[string]any{
				"vnum":     room.Vnum,
				"name":     room.Name,
				"area":     room.Area,
				"exits":    room.Exits,
				"desc_len": len(room.Description),
			},
		})
	}
	if room.Name != "" {
		s.trySendUI(ui.RoomNameMsg{Name: room.Name})
	}
}

func (s *Session) onRoomBlock(b mapper.RoomBlock) {
	if b.Name != "" {
		s.trySendUI(ui.RoomNameMsg{Name: b.Name})
	}
	processed, roomName, loopDetected, err := s.mapEngine.HandleRoomBlock(b)
	if !processed {
		return
	}
	if err != nil {
		s.trySendUI(ui.StatusMsg{Message: fmt.Sprintf("[Map] Error: %v\n", err)})
	} else if loopDetected {
		s.trySendUI(ui.StatusMsg{Message: fmt.Sprintf("[Map] Loop detected! Linked to existing room: %s\n", roomName)})
	} else {
		s.trySendUI(ui.StatusMsg{Message: fmt.Sprintf("[Map] New room created: %s\n", roomName)})
	}
}

func (s *Session) handleLocalCommands(wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case cmd := <-s.localChan:
				mutates, quit := s.dispatch(cmd)
				if quit {
					return
				}
				if mutates {
					s.scheduleSave()
				}
			case <-s.ctx.Done():
				return
			}
		}
	}()
}

func (s *Session) handleOutgoing(wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case text := <-s.sendChan:
				if s.mapEngine.IsAutoMapping() {
					processed, roomName, err := s.mapEngine.ProcessMovement(strings.TrimSpace(text))
					if processed {
						if err != nil {
							s.trySendUI(ui.StatusMsg{Message: fmt.Sprintf("[Map] Error: %v\n", err)})
						} else if roomName == "[pending room data]" {
							s.trySendUI(ui.StatusMsg{Message: "[Map] Moving... (awaiting room data)\n"})
						} else {
							s.trySendUI(ui.StatusMsg{Message: fmt.Sprintf("[Map] Moved to: %s\n", roomName)})
						}
					}
				}

				if c := s.client.Load(); c != nil {
					c.Send([]byte(appendCRLF(text)))
				} else {
					s.trySendUI(ui.StatusMsg{Message: "Not connected. Type /connect <host> <port> to connect.\n"})
				}
			case <-s.ctx.Done():
				return
			}
		}
	}()
}

func (s *Session) dispatch(cmd command.Command) (mutates, quit bool) {
	switch c := cmd.(type) {
	case *command.Quit:
		s.program.Quit()
		s.cancel()
		return false, true

	case *command.Connect:
		host, port := c.Host, c.Port
		if host == "" {
			if s.cfg.LastHost == "" {
				s.program.Send(ui.StatusMsg{Message: "No previous connection. Type /connect <host> <port> to connect.\n"})
				return false, false
			}
			host, port = s.cfg.LastHost, s.cfg.LastPort
		}
		if s.client.Load() != nil {
			s.pendingHost = host
			s.pendingPort = port
			s.program.Send(ui.ConfirmConnectMsg{
				CurrentHost: s.currentHost,
				CurrentPort: s.currentPort,
				Host:        host,
				Port:        port,
			})
			return false, false
		}
		s.connect(host, port)

	case *command.ConfirmConnect:
		if s.pendingHost == "" {
			s.program.Send(ui.StatusMsg{Message: "Nothing to confirm.\n"})
			return false, false
		}
		host, port := s.pendingHost, s.pendingPort
		s.pendingHost = ""
		s.pendingPort = 0
		if old := s.client.Load(); old != nil {
			old.SetDisconnectCallback(nil)
			old.Close()
		}
		s.connect(host, port)

	case *command.CancelConnect:
		if s.pendingHost == "" {
			s.program.Send(ui.StatusMsg{Message: "Nothing to cancel.\n"})
			return false, false
		}
		s.pendingHost = ""
		s.pendingPort = 0
		s.program.Send(ui.StatusMsg{Message: "Connection cancelled.\n"})

	case *command.TriggerAdd:
		if err := s.te.AddTrigger(c.Pattern, c.Response); err != nil {
			log.Println("Error adding trigger:", err)
		}

	case *command.TriggerRemove:
		s.te.RemoveTrigger(c.Pattern)

	case *command.TriggerList:
		triggers := s.te.ListTriggers()
		if len(triggers) == 0 {
			s.program.Send(ui.StatusMsg{Message: "No triggers defined.\n"})
		} else {
			var b strings.Builder
			b.WriteString("Active Triggers:\n")
			for _, t := range triggers {
				b.WriteString(fmt.Sprintf("  '%s' -> '%s'\n", t.Pattern.String(), t.Response))
			}
			s.program.Send(ui.StatusMsg{Message: b.String()})
		}
		return false, false

	case *command.ProtoOn:
		cl := s.client.Load()
		if cl == nil {
			s.program.Send(ui.StatusMsg{Message: "Not connected.\n"})
			return false, false
		}
		factory, ok := protoFactories[c.Name]
		if !ok {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Unknown protocol: %s\n", c.Name)})
			return false, false
		}
		if err := cl.Install(factory()); err != nil {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("%s install failed: %v\n", c.Name, err)})
		} else {
			s.installProtocolLogger(cl)
			if strings.EqualFold(c.Name, "MXP") {
				s.configureMXP(cl)
			}
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("[proto] %s enabled\n", c.Name)})
		}

	case *command.ProtoOff:
		cl := s.client.Load()
		if cl == nil {
			s.program.Send(ui.StatusMsg{Message: "Not connected.\n"})
			return false, false
		}
		if _, ok := protoFactories[c.Name]; !ok {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Unknown protocol: %s\n", c.Name)})
			return false, false
		}
		if err := cl.Uninstall(c.Name); err != nil {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("%s uninstall failed: %v\n", c.Name, err)})
		} else {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("[proto] %s disabled\n", c.Name)})
		}

	case *command.ProtoList:
		s.program.Send(ui.StatusMsg{Message: protoList(s.client.Load()) + "\n"})
		return false, false

	case *command.Save:
		s.saveGotinData(c.Filename)

	case *command.Load:
		s.loadGotinDataFile(c.Filename)

	// --- Mapper Commands ---
	case *command.MapCreate:
		filename := c.Filename
		if filename == "" {
			filename = fmt.Sprintf("map_%s.map", time.Now().Format("20060102-150405"))
		}
		if err := s.mapEngine.Create(filename); err != nil {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Map Init Error: %v\n", err)})
		} else {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Map initialized: %s\n", filename)})
		}

	case *command.MapPaths:
		if c.Directions == "" {
			paths := s.mapEngine.GetPaths()
			var names []string
			for _, d := range paths {
				names = append(names, string(d))
			}
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Configured paths: %s\n", strings.Join(names, ", "))})
			return false, false
		} else {
			dirList := strings.Split(c.Directions, ",")
			var newPaths []mapper.Direction
			var invalid []string
			for _, d := range dirList {
				if dir, ok := mapper.ParseDirection(d); ok {
					newPaths = append(newPaths, dir)
				} else {
					invalid = append(invalid, d)
				}
			}
			if len(invalid) > 0 {
				s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Invalid directions ignored: %s\n", strings.Join(invalid, ", "))})
			}
			if len(newPaths) > 0 {
				s.mapEngine.SetPaths(newPaths)
				var names []string
				for _, d := range newPaths {
					names = append(names, string(d))
				}
				s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Paths set to: %s\n", strings.Join(names, ", "))})
			} else {
				s.program.Send(ui.StatusMsg{Message: "No valid directions provided.\n"})
			}
		}

	case *command.MapDig:
		dir, ok := mapper.ParseDirection(c.Direction)
		if !ok {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Invalid direction: %s\n", c.Direction)})
			return false, false
		}
		if err := s.mapEngine.Dig(dir, "New Room"); err != nil {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Dig Error: %v\n", err)})
		} else {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Dug %s. Created new room.\n", dir)})
			if c.Action != "" {
				s.mapEngine.SetDescription("Reached via: " + c.Action)
			}
		}

	case *command.MapUndo:
		if err := s.mapEngine.Undo(); err != nil {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Undo Error: %v\n", err)})
		} else {
			s.program.Send(ui.StatusMsg{Message: "Undo successful.\n"})
		}

	case *command.MapDelete:
		if err := s.mapEngine.DeleteByNameOrID(c.Query); err != nil {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Delete Error: %v\n", err)})
		} else {
			s.program.Send(ui.StatusMsg{Message: "Room deleted.\n"})
		}

	case *command.MapGoto:
		if err := s.mapEngine.Goto(c.Query); err != nil {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Goto Error: %v\n", err)})
		} else {
			r := s.mapEngine.GetCurrent()
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Teleported to: %s\n", r.Name)})
		}

	case *command.MapLink:
		dir, ok := mapper.ParseDirection(c.Direction)
		if !ok {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Invalid direction: %s\n", c.Direction)})
			return false, false
		}
		if err := s.mapEngine.Link(dir, c.Target); err != nil {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Link Error: %v\n", err)})
		} else {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Linked %s to %s.\n", c.Direction, c.Target)})
		}

	case *command.MapStart:
		if !s.mapEngine.IsCreated() {
			s.program.Send(ui.StatusMsg{Message: "[Map] No map loaded. Run `/map create <filename>` first to create or load one before /map start.\n"})
			return false, false
		}
		if c.Query != "" {
			if err := s.mapEngine.Goto(c.Query); err != nil {
				s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Goto Error: %v\n", err)})
			}
		}
		s.mapEngine.StartAutoMapping()
		if !s.mapEngine.HasMXPSeen() {
			s.program.Send(ui.StatusMsg{Message: "[Map] Auto-mapping enabled, but MXP is not active on this connection. /map dig still works manually; auto-rooms will not appear until you reconnect with MXP enabled.\n"})
		} else {
			r := s.mapEngine.GetCurrent()
			if r != nil {
				s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Auto-mapping started at: %s\n", r.Name)})
			} else {
				s.program.Send(ui.StatusMsg{Message: "Auto-mapping started.\n"})
			}
		}

	case *command.MapStop:
		s.mapEngine.StopAutoMapping()
		s.program.Send(ui.StatusMsg{Message: "Auto-mapping stopped.\n"})

	case *command.MapName:
		s.mapEngine.SetName(c.Name)
		s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Room renamed to '%s'.\n", c.Name)})

	case *command.MapSearch:
		results := s.mapEngine.Search(c.Query)
		if len(results) == 0 {
			s.program.Send(ui.StatusMsg{Message: "No matches found.\n"})
		} else {
			s.program.Send(ui.StatusMsg{Message: "Search Results:\n" + strings.Join(results, "\n") + "\n"})
		}
		return false, false

	case *command.MapMermaid:
		radius := 3
		if c.Scope == "all" {
			radius = -1
		} else if c.Scope != "" {
			if val, err := strconv.Atoi(c.Scope); err == nil {
				radius = val
			}
		}
		s.program.Send(ui.StatusMsg{Message: s.mapEngine.Show(radius) + "\n"})
		return false, false

	case *command.MapShow:
		s.program.Send(ui.MapPaneToggleMsg{})
		return false, false

	case *command.MapRefresh:
		if err := s.mapEngine.Refresh(); err != nil {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("[Map] Refresh error: %v\n", err)})
		} else {
			s.program.Send(ui.StatusMsg{Message: "[Map] Layout refreshed.\n"})
		}
		return false, false

	case *command.MapInfo:
		r := s.mapEngine.GetCurrent()
		if r != nil {
			info := fmt.Sprintf("ID: %s\nName: %s\nDescHash: %s\nExits: %v\n", r.ID, r.Name, r.DescriptionHash, r.Exits)
			s.program.Send(ui.StatusMsg{Message: info})
		} else {
			s.program.Send(ui.StatusMsg{Message: "No current room.\n"})
		}
		return false, false

	case *command.MapExit:
		s.mapEngine.StopAutoMapping()
		if err := s.mapEngine.Save(); err != nil {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Save Error: %v\n", err)})
		} else {
			s.program.Send(ui.StatusMsg{Message: "Map saved. Auto-mapping stopped.\n"})
		}

	case *command.MapOption:
		opts := s.mapEngine.GetMappingOptions()
		if c.Print {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("mapping options: vnum=%s, hash=%s\n", onOff(opts.Vnum), onOff(opts.Hash))})
			return false, false
		}
		switch c.Strategy {
		case "vnum":
			opts.Vnum = c.Enable
		case "hash":
			opts.Hash = c.Enable
		case "none":
			opts.Vnum = false
			opts.Hash = false
		default:
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Unknown mapping strategy: %s\n", c.Strategy)})
			return false, false
		}
		s.mapEngine.SetMappingOptions(opts)
		s.scheduleMapOptionsSave(opts)
		s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("mapping options: vnum=%s, hash=%s\n", onOff(opts.Vnum), onOff(opts.Hash))})
		return false, false

	case *command.AliasAdd, *command.AliasRemove,
		*command.ConnectionAliasAdd, *command.ConnectionAliasRemove:
		// Handler already mutated state; just schedule save

	case *command.AliasList, *command.AliasListConnections:
		return false, false
	}

	return true, false
}

func (s *Session) saveGotinData(filename string) {
	aliases := s.model.GetAliases()
	triggers := s.te.ListTriggers()
	connections := s.model.GetConnections()

	td := gotinData{
		Aliases:        aliases,
		Triggers:       make([]config.TriggerConfig, len(triggers)),
		Connections:    connections,
		MappingOptions: s.mappingOptionsTop,
		MUDProfiles:    s.mudProfiles,
	}
	for i, t := range triggers {
		td.Triggers[i] = config.TriggerConfig{
			Pattern:  t.Pattern.String(),
			Response: t.Response,
		}
	}

	fileData, err := json.MarshalIndent(td, "", "  ")
	if err != nil {
		s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Save error: %v\n", err)})
		return
	}
	tmpFile := filename + ".tmp"
	if err := os.WriteFile(tmpFile, fileData, 0644); err != nil {
		s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Save error: %v\n", err)})
		return
	}
	if err := os.Rename(tmpFile, filename); err != nil {
		s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Save error: %v\n", err)})
		return
	}
	s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Saved %d aliases, %d triggers and %d connections to %s\n", len(aliases), len(triggers), len(connections), filename)})
}

func (s *Session) loadGotinDataFile(filename string) {
	fileData, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("File not found: %s\n", filename)})
		} else {
			s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Load error: %v\n", err)})
		}
		return
	}
	var td gotinData
	if err := json.Unmarshal(fileData, &td); err != nil {
		s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Load error: %v\n", err)})
		return
	}
	s.model.ClearAliases()
	s.te.ClearTriggers()
	s.model.ClearConnections()
	if td.Aliases != nil {
		s.model.SetAliases(td.Aliases)
	}
	for _, t := range td.Triggers {
		s.te.AddTrigger(t.Pattern, t.Response)
	}
	if td.Connections != nil {
		s.model.SetConnections(td.Connections)
	}
	if td.MappingOptions != nil {
		s.mappingOptionsTop = td.MappingOptions
	} else {
		defaultMappingOptions := mapper.DefaultMappingOptions()
		s.mappingOptionsTop = &defaultMappingOptions
	}
	if td.MUDProfiles != nil {
		s.mudProfiles = td.MUDProfiles
	}
	s.program.Send(ui.StatusMsg{Message: fmt.Sprintf("Loaded %d aliases, %d triggers and %d connections from %s\n", len(td.Aliases), len(td.Triggers), len(td.Connections), filename)})
}

func (s *Session) loadGotinData() {
	if _, err := os.Stat(gotinDataFilename); err != nil {
		return
	}
	data, err := os.ReadFile(gotinDataFilename)
	if err != nil {
		return
	}
	var td gotinData
	if err := json.Unmarshal(data, &td); err != nil {
		log.Printf("Error parsing %s: %v\n", gotinDataFilename, err)
		return
	}
	s.model.ClearAliases()
	s.te.ClearTriggers()
	s.model.ClearConnections()
	if td.Aliases != nil {
		s.model.SetAliases(td.Aliases)
	}
	for _, t := range td.Triggers {
		s.te.AddTrigger(t.Pattern, t.Response)
	}
	if td.Connections != nil {
		s.model.SetConnections(td.Connections)
		for _, ca := range td.Connections {
			if ca.Auto {
				s.autoConnectFromAlias = true
				s.autoConnectAlias = ca
				break
			}
		}
	}
	if td.MappingOptions != nil {
		s.mappingOptionsTop = td.MappingOptions
	}
	if td.MUDProfiles != nil {
		s.mudProfiles = td.MUDProfiles
	}
}

func (s *Session) resolveMappingOptions(host string, port int) (mapper.MappingOptions, string, string) {
	conns := s.model.GetConnections()
	names := make([]string, 0, len(conns))
	for name := range conns {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		ca := conns[name]
		if ca.Host == host && ca.Port == port {
			if ca.MappingOptions != nil {
				return *ca.MappingOptions, "alias", name
			}
			if s.mappingOptionsTop != nil {
				return *s.mappingOptionsTop, "alias", name
			}
			return mapper.DefaultMappingOptions(), "alias", name
		}
	}
	if s.mappingOptionsTop != nil {
		return *s.mappingOptionsTop, "global", ""
	}
	return mapper.DefaultMappingOptions(), "global", ""
}

func (s *Session) installProtocolLogger(c *network.Client) {
	if s.protoLog == nil || c == nil {
		return
	}
	if g := findGMCP(c); g != nil {
		g.SetLogger(s.protoLog)
	}
	if m := findMXP(c); m != nil {
		m.SetLogger(s.protoLog)
	}
	c.SetProtocolLogger(s.protoLog)
}

func (s *Session) configureMXP(c *network.Client) {
	m := findMXP(c)
	if m == nil {
		s.roomBuf = nil
		return
	}
	profile := s.mapEngine.GetMUDProfile()
	if s.roomBuf == nil {
		cp, err := profile.Compile()
		if err != nil {
			s.trySendUI(ui.StatusMsg{Message: fmt.Sprintf("[Map] profile compile error: %v\n", err)})
		} else {
			s.roomBuf = mapper.NewRoomBlockBuffer(cp, s.onRoomBlock)
		}
	}
	var inner mxp.Sink
	if s.roomBuf != nil {
		inner = s.roomBuf
	} else {
		inner = noopSink{}
	}
	m.SetSink(sentinelSink{
		inner:         inner,
		engine:        s.mapEngine,
		blockStartTag: profile.BlockStartTag,
	})
	m.SetSentinelResetCallback(func() {
		s.mapEngine.ResetBlockStart()
		if s.roomBuf != nil {
			s.roomBuf.Reset()
		}
	})
	m.SetRoomNameCallback(func(name string) {
		if name == "" {
			return
		}
		s.mapEngine.SetIncomingRoomName(name)
		s.trySendUI(ui.RoomNameMsg{Name: name})
	})
	if s.protoLog != nil {
		m.SetLogger(s.protoLog)
	}
}

func (s *Session) resolveMUDProfile(host string) mapper.MUDProfile {
	base := mapper.DefaultT2TMUDProfile()
	if s.mudProfiles == nil {
		return base
	}
	if over, ok := lookupProfile(s.mudProfiles, host); ok {
		return mapper.MergeProfile(base, over)
	}
	return base
}

// lookupProfile finds a MUDProfile by host (case-insensitive).
func lookupProfile(m map[string]mapper.MUDProfile, host string) (mapper.MUDProfile, bool) {
	for k, v := range m {
		if strings.EqualFold(k, host) {
			return v, true
		}
	}
	return mapper.MUDProfile{}, false
}

// sentinelSink wraps a sink so every observed MXP tag updates mapper sentinels.
type sentinelSink struct {
	inner         mxp.Sink
	engine        *mapper.Engine
	blockStartTag string
}

func (s sentinelSink) OnText(t string) { s.inner.OnText(t) }

func (s sentinelSink) OnTag(name, body string) {
	s.engine.MarkMXPSeen()
	if name == s.blockStartTag {
		s.engine.MarkBlockStartSeen()
	}
	s.inner.OnTag(name, body)
}

type noopSink struct{}

func (noopSink) OnText(string)        {}
func (noopSink) OnTag(string, string) {}

func (s *Session) scheduleMapOptionsSave(opts mapper.MappingOptions) {
	if s.mappingOptionsScope == "alias" && s.mappingOptionsAlias != "" {
		conns := s.model.GetConnections()
		if ca, ok := conns[s.mappingOptionsAlias]; ok {
			o := opts
			ca.MappingOptions = &o
			conns[s.mappingOptionsAlias] = ca
			s.model.SetConnections(conns)
		}
	} else {
		o := opts
		s.mappingOptionsTop = &o
	}
	s.persistGotinData()
}

func (s *Session) persistGotinData() {
	aliases := s.model.GetAliases()
	triggers := s.te.ListTriggers()
	connections := s.model.GetConnections()

	td := gotinData{
		Aliases:        aliases,
		Triggers:       make([]config.TriggerConfig, len(triggers)),
		Connections:    connections,
		MappingOptions: s.mappingOptionsTop,
		MUDProfiles:    s.mudProfiles,
	}
	for i, t := range triggers {
		td.Triggers[i] = config.TriggerConfig{
			Pattern:  t.Pattern.String(),
			Response: t.Response,
		}
	}

	fileData, err := json.MarshalIndent(td, "", "  ")
	if err != nil {
		return
	}
	tmp := gotinDataFilename + ".tmp"
	if err := os.WriteFile(tmp, fileData, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, gotinDataFilename)
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func historyFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	gotinDir, err := config.DefaultConfigDir()
	if err != nil {
		return "", err
	}
	newPath := filepath.Join(gotinDir, "history")

	// Migration: if new path doesn't exist but old path does, migrate
	oldPath := filepath.Join(home, ".gotin_history")
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		if info, err := os.Stat(oldPath); err == nil && !info.IsDir() {
			_ = os.MkdirAll(gotinDir, 0755)
			_ = os.Rename(oldPath, newPath)
		}
	}

	// Ensure directory exists
	_ = os.MkdirAll(gotinDir, 0755)
	return newPath, nil
}

func loadHistory(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var commands []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			commands = append(commands, line)
		}
	}
	return commands, scanner.Err()
}

func saveHistory(path string, commands []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	writer := bufio.NewWriter(f)
	for _, cmd := range commands {
		_, _ = writer.WriteString(cmd)
		_ = writer.WriteByte('\n')
	}
	return writer.Flush()
}
