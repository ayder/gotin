package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"dmud/internal/config"
	"dmud/internal/input"
	"dmud/internal/logic"
	"dmud/internal/network"
	"dmud/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// 1. Parse Flags
	hostFlag := flag.String("host", "", "MUD server hostname")
	portFlag := flag.Int("port", 0, "MUD server port")
	debug := flag.Bool("debug", false, "Enable debug logging for telnet negotiation")
	flag.Parse()

	// 2. Load Configuration
	cfgMgr, err := config.NewManager()
	if err != nil {
		log.Fatal("Failed to initialize config manager:", err)
	}

	cfg, err := cfgMgr.Load()
	if err != nil {
		log.Println("Error loading config:", err)
	}

	// 3. Setup Channels
	// UI -> Network (Text to send to server)
	sendChan := make(chan string)
	// UI -> Main (Local commands like /connect, /quit)
	localChan := make(chan input.CommandResult)

	// 4. Initialize UI Model
	model := ui.New(sendChan, localChan)
	// Load aliases from config into key handler
	model.SetAliases(cfg.Aliases)

	// 5. Initialize Bubble Tea Program
	p := tea.NewProgram(model, tea.WithAltScreen())

	// 6. Handle Wizard / First Run
	// If no config file exists (fresh run) AND no flags provided
	firstRun := !cfgMgr.Exists()
	if firstRun && *hostFlag == "" {
		// Inject welcome message
		p.Send(ui.NetworkDataMsg{
			Data: "Welcome to TermMud!\n" +
				"It looks like this is your first time here.\n" +
				"Type /connect <host> <port> to start playing.\n" +
				"Example: /connect t2tmud.org 9999\n",
		})
	}

	// 7. Network State
	var client *network.Client

	// Shared Logic Components (initialized once)
	sendToNet := func(msg string) {
		if client != nil {
			// Append CRLF as triggers likely don't include it
			if !strings.HasSuffix(msg, "\r\n") {
				msg += "\r\n"
			}
			client.Send([]byte(msg))
		}
	}
	te := logic.NewTriggerEngine(sendToNet)

	// Load triggers from config
	for _, t := range cfg.Triggers {
		te.AddTrigger(t.Pattern, t.Response)
	}

	// Helper to connect
	connect := func(h string, port int) {
		if client != nil {
			client.Close()
		}

		p.Send(ui.NetworkDataMsg{Data: fmt.Sprintf("Connecting to %s:%d...\n", h, port)})

		c, err := network.Connect(h, port)
		if err != nil {
			p.Send(ui.NetworkDataMsg{Data: fmt.Sprintf("Connection failed: %v\n", err)})
			return
		}

		client = c
		client.SetDebug(*debug)

		// --- Logic Layer Wiring ---
		// 1. Buffer
		lb := logic.NewLineBuffer()

		// 2. Trigger Engine (Already initialized)
		// 3. Processor
		proc := logic.NewProcessor(te)

		// Setup callbacks
		client.SetEchoCallback(func(enabled bool) {
			p.Send(ui.SetLocalEchoMsg{LocalEcho: enabled})
			// Do NOT touch stty here; BubbleTea handles terminal mode.
			// setLocalEcho(enabled) -> REMOVED
		})

		client.SetDataCallback(func(data string) {
			// 1. Feed buffer
			// 2. Process complete lines
			// 3. Handle Prompt (Wait I need to be careful here)
			// If I send the pending data, the UI will print it.
			// But if the next packet completes the line, I will print the line AGAIN.
			//
			// Approach:
			// Just send the Raw Data to the UI for display?
			// But Triggers need lines.
			//
			// Alternative: Send a "PromptMsg" to UI?
			// Or modify UI to handle raw stream?
			//
			// If I change main to send Raw Data directly to UI for display:
			//   p.Send(ui.NetworkDataMsg{Data: data})
			// AND feed it to Logic for triggers?
			// That decouples display from logic! This is probably better for a MUD client.
			//
			// Let's TRY that. Sending everything to UI immediately prevents prompt Lag.
			// Triggers run on the "Logic Buffer" side. When a trigger fires, it sends data back.
			//
			// Wait, M3T4 (ANSI Strip) logic was in Processor.ProcessLine.
			// If I bypass Processor for display, I bypass ANSI logic? No, UI handles ANSI (BubbleTea viewport supports it naturally).
			// The Processor ANSI strip was ONLY for Triggers (so regex matches ^You are hungry vs ^\x1b[31mYou are hungry).
			//
			// So:
			// 1. Send `data` directly to UI (Display Layer).
			// 2. Feed `data` to `lb` (Logic Layer).
			// 3. If `lb` emits lines, check Triggers.
			// 4. Triggers might send responses.
			// This matches standard MUD client architecture (Display Stream vs Logic Stream).

			p.Send(ui.NetworkDataMsg{Data: data})

			// Logic processing (Triggers)
			logicLines := lb.Feed([]byte(data))
			for _, line := range logicLines {
				proc.ProcessLine(line) // This checks triggers
			}
		})

		// Start reading in a goroutine
		go func() {
			defer c.Close()
			c.ReadLoop()
			// If ReadLoop exits, connection is closed
			p.Send(ui.NetworkDataMsg{Data: "\nConnection closed.\n"})
		}()

		p.Send(ui.NetworkDataMsg{Data: "Connected!\n"})

		// Update Config
		cfg.LastHost = h
		cfg.LastPort = port
		cfgMgr.Save(cfg)
	}

	// 8. Handle Local Commands (Goroutine)
	go func() {
		for cmd := range localChan {
			switch cmd.Action {
			case "quit":
				p.Quit()
				return
			case "connect":
				host := cmd.ActionArgs["host"]
				portStr := cmd.ActionArgs["port"]
				port, _ := strconv.Atoi(portStr)
				connect(host, port)

			case "trigger_add":
				pattern := cmd.ActionArgs["pattern"]
				response := cmd.ActionArgs["response"]
				err := te.AddTrigger(pattern, response)
				if err != nil {
					// We might want to send this error back to UI, but Response was already sent.
					// Ideally Handler should validate regex, but it doesn't have regexp pkg logic.
					// For now, assume it works or log.
					log.Println("Error adding trigger:", err)
				}

			case "trigger_remove":
				pattern := cmd.ActionArgs["pattern"]
				te.RemoveTrigger(pattern)

			case "trigger_list":
				// List triggers and show to user
				triggers := te.ListTriggers()
				if len(triggers) == 0 {
					p.Send(ui.NetworkDataMsg{Data: "No triggers defined.\n"})
				} else {
					var sb strings.Builder
					sb.WriteString("Active Triggers:\n")
					for _, t := range triggers {
						sb.WriteString(fmt.Sprintf("  '%s' -> '%s'\n", t.Pattern.String(), t.Response))
					}
					p.Send(ui.NetworkDataMsg{Data: sb.String()})
				}
			}

			// Also save config on changes
			if cmd.Handled {
				// Refresh aliases from model
				cfg.Aliases = model.GetAliases()

				// Refresh triggers from TE
				// We need to implement this sync
				triggers := te.ListTriggers()
				cfg.Triggers = make([]config.TriggerConfig, len(triggers))
				for i, t := range triggers {
					cfg.Triggers[i] = config.TriggerConfig{
						Pattern:  t.Pattern.String(),
						Response: t.Response,
					}
				}

				cfgMgr.Save(cfg)
			}
		}
	}()

	// 9. Handle Outgoing Data (Goroutine)
	go func() {
		for text := range sendChan {
			if client != nil {
				// Append \r\n if needed, standard Telnet requires CRLF
				if !strings.HasSuffix(text, "\r\n") {
					text += "\r\n"
				}
				client.Send([]byte(text))
			} else {
				p.Send(ui.NetworkDataMsg{Data: "Not connected. Type /connect <host> <port> to connect.\n"})
			}
		}
	}()

	// 10. Auto-Connect if flags provided
	if *hostFlag != "" && *portFlag != 0 {
		go connect(*hostFlag, *portFlag)
	} else if cfg.LastHost != "" && !firstRun {
		// Optional: Auto-reconnect to last host
		go func() {
			p.Send(ui.NetworkDataMsg{Data: fmt.Sprintf("Last connected to: %s:%d. Type /connect to reconnect.\n", cfg.LastHost, cfg.LastPort)})
		}()
	}

	// 11. Run UI
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}

	// Cleanup on exit
}
