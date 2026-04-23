package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"dmud/internal/config"
	"dmud/internal/input"
	"dmud/internal/logic"
	"dmud/internal/mapper"
	"dmud/internal/network"
	"dmud/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

// termudData represents the JSON structure used by /save and /load commands.
type termudData struct {
	Aliases     map[string]string                `json:"aliases,omitempty"`
	Triggers    []config.TriggerConfig           `json:"triggers,omitempty"`
	Connections map[string]input.ConnectionAlias `json:"connections,omitempty"`
}

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
	sendChan := make(chan string, 128)
	// UI -> Main (Local commands like /connect, /quit)
	localChan := make(chan input.CommandResult, 128)

	// 4. Initialize UI Model
	model := ui.New(sendChan, localChan)
	// Load aliases from config into key handler
	model.SetAliases(cfg.Aliases)

	// Load command history from ~/.termud_history if it exists
	historyPath, _ := historyFilePath()
	if cmds, err := loadHistory(historyPath); err == nil {
		model.LoadHistory(cmds)
	}

	// 5. Initialize Bubble Tea Program
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	// Non-blocking UI delivery pump
	uiMsgChan := make(chan tea.Msg, 1024)
	go func() {
		for msg := range uiMsgChan {
			p.Send(msg)
		}
	}()

	// 6. Handle Wizard / First Run
	// If no config file exists (fresh run) AND no flags provided
	firstRun := !cfgMgr.Exists()
	if firstRun && *hostFlag == "" {
		// Inject welcome message
		p.Send(ui.StatusMsg{
			Message: "Welcome to TermMud!\n" +
				"It looks like this is your first time here.\n" +
				"Type /connect <host> <port> to start playing.\n" +
				"Example: /connect t2tmud.org 9999\n",
		})
	}

	// 7. Network State
	var client *network.Client

	// Resize callback: update NAWS when terminal size changes
	model.SetResizeCallback(func(w, h int) {
		if client != nil {
			client.SetWindowSize(w, h)
			client.SendNAWS()
		}
	})

	// Shared Logic Components
	sendToNet := func(msg string) {
		if client != nil {
			if !strings.HasSuffix(msg, "\r\n") {
				msg += "\r\n"
			}
			client.Send([]byte(msg))
		}
	}
	te := logic.NewTriggerEngine(sendToNet)
	// Initialize Mapper
	mapEngine := mapper.NewEngine("") // Path set on create

	// Load triggers from config
	for _, t := range cfg.Triggers {
		te.AddTrigger(t.Pattern, t.Response)
	}

	// Auto-load termud.json from current directory if it exists
	autoConnectFromAlias := false
	var autoConnectAlias input.ConnectionAlias
	if _, err := os.Stat("termud.json"); err == nil {
		data, err := os.ReadFile("termud.json")
		if err == nil {
			var td termudData
			if err := json.Unmarshal(data, &td); err == nil {
				model.ClearAliases()
				te.ClearTriggers()
				model.ClearConnections()
				if td.Aliases != nil {
					model.SetAliases(td.Aliases)
				}
				for _, t := range td.Triggers {
					te.AddTrigger(t.Pattern, t.Response)
				}
				if td.Connections != nil {
					model.SetConnections(td.Connections)
					for _, ca := range td.Connections {
						if ca.Auto {
							autoConnectFromAlias = true
							autoConnectAlias = ca
							break
						}
					}
				}
			} else {
				log.Printf("Error parsing termud.json: %v\n", err)
			}
		}
	}

	// Helper to connect
	connect := func(h string, port int) {
		if client != nil {
			client.Close()
		}

		p.Send(ui.StatusMsg{Message: fmt.Sprintf("Connecting to %s:%d...\n", h, port)})

		c, err := network.Connect(h, port)
		if err != nil {
			p.Send(ui.StatusMsg{Message: fmt.Sprintf("Connection failed: %v\n", err)})
			return
		}

		client = c
		client.SetDebug(*debug)
		client.SetWindowSize(model.Width(), model.Height())

		// --- Logic Layer Wiring ---
		// 1. Buffer
		lb := logic.NewLineBuffer()

		// 2. Trigger Engine (Already initialized)
		// 3. Processor
		proc := logic.NewProcessor(te)

		// Setup callbacks
		client.SetEchoCallback(func(enabled bool) {
			uiMsgChan <- ui.SetLocalEchoMsg{LocalEcho: enabled}
		})

		client.SetDataCallback(func(data string) {
			// Decoupled UI delivery
			uiMsgChan <- ui.NetworkDataMsg{Data: data}

			// Logic processing (Triggers)
			logicLines := lb.Feed([]byte(data))
			for _, line := range logicLines {
				proc.ProcessLine(line) // This checks triggers
			}

			// Smart auto-mapping: process room data when we have a pending movement
			if mapEngine.HasPendingMovement() {
				processed, roomName, loopDetected, err := mapEngine.ProcessRoomData(data)
				if processed {
					if err != nil {
						uiMsgChan <- ui.StatusMsg{Message: fmt.Sprintf("[Map] Error: %v\n", err)}
					} else if loopDetected {
						uiMsgChan <- ui.StatusMsg{Message: fmt.Sprintf("[Map] Loop detected! Linked to existing room: %s\n", roomName)}
					} else {
						uiMsgChan <- ui.StatusMsg{Message: fmt.Sprintf("[Map] New room created: %s\n", roomName)}
					}
				}
			}
		})

		// Start reading in a goroutine
		go func() {
			defer c.Close()
			c.ReadLoop()
			// If ReadLoop exits, connection is closed
			p.Send(ui.StatusMsg{Message: "\nConnection closed.\n"})
		}()

		p.Send(ui.StatusMsg{Message: "Connected!\n"})
		client.SendNAWS() // Send initial NAWS on connect

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
				port, err := strconv.Atoi(portStr)
				if err != nil || port <= 0 || port > 65535 {
					p.Send(ui.StatusMsg{Message: fmt.Sprintf("Invalid port: %s\n", portStr)})
					return
				}
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
				triggers := te.ListTriggers()
				if len(triggers) == 0 {
					p.Send(ui.StatusMsg{Message: "No triggers defined.\n"})
				} else {
					var sb strings.Builder
					sb.WriteString("Active Triggers:\n")
					for _, t := range triggers {
						sb.WriteString(fmt.Sprintf("  '%s' -> '%s'\n", t.Pattern.String(), t.Response))
					}
					p.Send(ui.StatusMsg{Message: sb.String()})
				}

			case "save":
				filename := cmd.ActionArgs["filename"]
				aliases := model.GetAliases()
				triggers := te.ListTriggers()
				connections := model.GetConnections()

				td := termudData{
					Aliases:     aliases,
					Triggers:    make([]config.TriggerConfig, len(triggers)),
					Connections: connections,
				}
				for i, t := range triggers {
					td.Triggers[i] = config.TriggerConfig{
						Pattern:  t.Pattern.String(),
						Response: t.Response,
					}
				}

				fileData, err := json.MarshalIndent(td, "", "  ")
				if err != nil {
					p.Send(ui.StatusMsg{Message: fmt.Sprintf("Save error: %v\n", err)})
				} else {
					tmpFile := filename + ".tmp"
					err = os.WriteFile(tmpFile, fileData, 0644)
					if err != nil {
						p.Send(ui.StatusMsg{Message: fmt.Sprintf("Save error: %v\n", err)})
					} else {
						err = os.Rename(tmpFile, filename)
						if err != nil {
							p.Send(ui.StatusMsg{Message: fmt.Sprintf("Save error: %v\n", err)})
						} else {
							p.Send(ui.StatusMsg{Message: fmt.Sprintf("Saved %d aliases, %d triggers and %d connections to %s\n", len(aliases), len(triggers), len(connections), filename)})
						}
					}
				}

			case "load":
				filename := cmd.ActionArgs["filename"]
				fileData, err := os.ReadFile(filename)
				if err != nil {
					if os.IsNotExist(err) {
						p.Send(ui.StatusMsg{Message: fmt.Sprintf("File not found: %s\n", filename)})
					} else {
						p.Send(ui.StatusMsg{Message: fmt.Sprintf("Load error: %v\n", err)})
					}
				} else {
					var td termudData
					err = json.Unmarshal(fileData, &td)
					if err != nil {
						p.Send(ui.StatusMsg{Message: fmt.Sprintf("Load error: %v\n", err)})
					} else {
						model.ClearAliases()
						te.ClearTriggers()
						model.ClearConnections()
						if td.Aliases != nil {
							model.SetAliases(td.Aliases)
						}
						for _, t := range td.Triggers {
							te.AddTrigger(t.Pattern, t.Response)
						}
						if td.Connections != nil {
							model.SetConnections(td.Connections)
						}
						p.Send(ui.StatusMsg{Message: fmt.Sprintf("Loaded %d aliases, %d triggers and %d connections from %s\n", len(td.Aliases), len(td.Triggers), len(td.Connections), filename)})
					}
				}

			// --- Mapper Commands ---
			case "map_create":
				filename := cmd.ActionArgs["filename"]
				err := mapEngine.Create(filename)
				if err != nil {
					p.Send(ui.StatusMsg{Message: fmt.Sprintf("Map Init Error: %v\n", err)})
				} else {
					p.Send(ui.StatusMsg{Message: "Map initialized.\n"})
				}

			case "map_paths":
				dirsStr := cmd.ActionArgs["directions"]
				if dirsStr == "" {
					// Show current paths
					paths := mapEngine.GetPaths()
					var pathNames []string
					for _, d := range paths {
						pathNames = append(pathNames, string(d))
					}
					p.Send(ui.StatusMsg{Message: fmt.Sprintf("Configured paths: %s\n", strings.Join(pathNames, ", "))})
				} else {
					// Set paths
					dirList := strings.Split(dirsStr, ",")
					var newPaths []mapper.Direction
					var invalid []string
					for _, d := range dirList {
						dir, ok := mapper.ParseDirection(d)
						if ok {
							newPaths = append(newPaths, dir)
						} else {
							invalid = append(invalid, d)
						}
					}
					if len(invalid) > 0 {
						p.Send(ui.StatusMsg{Message: fmt.Sprintf("Invalid directions ignored: %s\n", strings.Join(invalid, ", "))})
					}
					if len(newPaths) > 0 {
						mapEngine.SetPaths(newPaths)
						var pathNames []string
						for _, d := range newPaths {
							pathNames = append(pathNames, string(d))
						}
						p.Send(ui.StatusMsg{Message: fmt.Sprintf("Paths set to: %s\n", strings.Join(pathNames, ", "))})
					} else {
						p.Send(ui.StatusMsg{Message: "No valid directions provided.\n"})
					}
				}

			case "map_dig":
				dir := mapper.Direction(cmd.ActionArgs["direction"])
				action := cmd.ActionArgs["action"] // Metadata or actual command?
				// For now just dig.
				err := mapEngine.Dig(dir, "New Room")
				if err != nil {
					p.Send(ui.StatusMsg{Message: fmt.Sprintf("Dig Error: %v\n", err)})
				} else {
					p.Send(ui.StatusMsg{Message: fmt.Sprintf("Dug %s. Created new room.\n", dir)})
					// Optionally send the action to the server?
					// "If map dig n open door" -> Dig North, then send "open door"?
					// User said: "Create a new room... based on the action".
					// Probably intended to just map it.
					// If action is provided, maybe store it?
					if action != "" {
						// For now, we don't store action in Room struct as per requirements,
						// but maybe description?
						mapEngine.SetDescription("Reached via: " + action)
					}
				}

			case "map_undo":
				err := mapEngine.Undo()
				if err != nil {
					p.Send(ui.StatusMsg{Message: fmt.Sprintf("Undo Error: %v\n", err)})
				} else {
					p.Send(ui.StatusMsg{Message: "Undo successful.\n"})
				}

			case "map_delete":
				query := cmd.ActionArgs["query"]
				err := mapEngine.DeleteByNameOrID(query)
				if err != nil {
					p.Send(ui.StatusMsg{Message: fmt.Sprintf("Delete Error: %v\n", err)})
				} else {
					p.Send(ui.StatusMsg{Message: "Room deleted.\n"})
				}

			case "map_goto":
				query := cmd.ActionArgs["query"]
				err := mapEngine.Goto(query)
				if err != nil {
					p.Send(ui.StatusMsg{Message: fmt.Sprintf("Goto Error: %v\n", err)})
				} else {
					r := mapEngine.GetCurrent()
					p.Send(ui.StatusMsg{Message: fmt.Sprintf("Teleported to: %s\n", r.Name)})
				}

			case "map_link":
				dirStr := cmd.ActionArgs["direction"]
				target := cmd.ActionArgs["target"]
				dir, ok := mapper.ParseDirection(dirStr)
				if !ok {
					p.Send(ui.StatusMsg{Message: fmt.Sprintf("Invalid direction: %s\n", dirStr)})
				} else {
					err := mapEngine.Link(dir, target)
					if err != nil {
						p.Send(ui.StatusMsg{Message: fmt.Sprintf("Link Error: %v\n", err)})
					} else {
						p.Send(ui.StatusMsg{Message: fmt.Sprintf("Linked %s to %s.\n", dirStr, target)})
					}
				}

			case "map_start":
				query := cmd.ActionArgs["query"]
				if query != "" {
					// Goto specified room first
					if err := mapEngine.Goto(query); err != nil {
						p.Send(ui.StatusMsg{Message: fmt.Sprintf("Goto Error: %v\n", err)})
					}
				}
				mapEngine.StartAutoMapping()
				r := mapEngine.GetCurrent()
				if r != nil {
					p.Send(ui.StatusMsg{Message: fmt.Sprintf("Auto-mapping started at: %s\n", r.Name)})
				} else {
					p.Send(ui.StatusMsg{Message: "Auto-mapping started.\n"})
				}

			case "map_stop":
				mapEngine.StopAutoMapping()
				p.Send(ui.StatusMsg{Message: "Auto-mapping stopped.\n"})

			case "map_name":
				name := cmd.ActionArgs["name"]
				mapEngine.SetName(name)
				p.Send(ui.StatusMsg{Message: fmt.Sprintf("Room renamed to '%s'.\n", name)})

			case "map_search":
				query := cmd.ActionArgs["query"]
				results := mapEngine.Search(query)
				if len(results) == 0 {
					p.Send(ui.StatusMsg{Message: "No matches found.\n"})
				} else {
					p.Send(ui.StatusMsg{Message: "Search Results:\n" + strings.Join(results, "\n") + "\n"})
				}

			case "map_show":
				scope := cmd.ActionArgs["scope"]
				radius := 3 // default
				if scope == "all" {
					radius = -1
				} else if scope != "" {
					if val, err := strconv.Atoi(scope); err == nil {
						radius = val
					}
				}
				p.Send(ui.StatusMsg{Message: mapEngine.Show(radius) + "\n"})

			case "map_info":
				r := mapEngine.GetCurrent()
				if r != nil {
					info := fmt.Sprintf("ID: %s\nName: %s\nDescHash: %s\nExits: %v\n", r.ID, r.Name, r.DescriptionHash, r.Exits)
					p.Send(ui.StatusMsg{Message: info})
				} else {
					p.Send(ui.StatusMsg{Message: "No current room.\n"})
				}

			case "map_exit":
				mapEngine.StopAutoMapping()
				err := mapEngine.Save()
				if err != nil {
					p.Send(ui.StatusMsg{Message: fmt.Sprintf("Save Error: %v\n", err)})
				} else {
					p.Send(ui.StatusMsg{Message: "Map saved. Auto-mapping stopped.\n"})
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
			// Check auto-mapping before sending to server
			if mapEngine.IsAutoMapping() {
				processed, roomName, err := mapEngine.ProcessMovement(strings.TrimSpace(text))
				if processed {
					if err != nil {
						p.Send(ui.StatusMsg{Message: fmt.Sprintf("[Map] Error: %v\n", err)})
					} else if roomName == "[pending room data]" {
						// Smart auto-mapping: waiting for room description
						p.Send(ui.StatusMsg{Message: "[Map] Moving... (awaiting room data)\n"})
					} else {
						// Moved to known room (existing exit)
						p.Send(ui.StatusMsg{Message: fmt.Sprintf("[Map] Moved to: %s\n", roomName)})
					}
				}
			}

			if client != nil {
				// Append \r\n if needed, standard Telnet requires CRLF
				if !strings.HasSuffix(text, "\r\n") {
					text += "\r\n"
				}
				client.Send([]byte(text))
			} else {
				p.Send(ui.StatusMsg{Message: "Not connected. Type /connect <host> <port> to connect.\n"})
			}
		}
	}()

	// 10. Auto-Connect if flags provided
	if *hostFlag != "" && *portFlag != 0 {
		go connect(*hostFlag, *portFlag)
	} else if autoConnectFromAlias {
		go connect(autoConnectAlias.Host, autoConnectAlias.Port)
	} else if cfg.LastHost != "" && !firstRun {
		// Optional: Auto-reconnect to last host
		go func() {
			p.Send(ui.StatusMsg{Message: fmt.Sprintf("Last connected to: %s:%d. Type /connect to reconnect.\n", cfg.LastHost, cfg.LastPort)})
		}()
	}

	// 11. Run UI
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}

	// Cleanup on exit
	// Save command history to ~/.termud_history
	if historyPath, err := historyFilePath(); err == nil {
		_ = saveHistory(historyPath, model.GetHistory())
	}
}

// historyFilePath returns the path to the history file (~/.termud_history).
func historyFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".termud_history"), nil
}

// loadHistory reads command history from a file, one command per line.
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

// saveHistory writes command history to a file, one command per line.
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
