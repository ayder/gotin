package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"dmud/internal/network"
)

// setLocalEcho enables or disables local terminal echo using stty.
func setLocalEcho(enabled bool) {
	var cmd *exec.Cmd
	if enabled {
		cmd = exec.Command("stty", "echo")
	} else {
		cmd = exec.Command("stty", "-echo")
	}
	cmd.Stdin = os.Stdin
	cmd.Run()
}

func main() {
	host := flag.String("host", "t2tmud.org", "MUD server hostname")
	port := flag.Int("port", 9999, "MUD server port")
	debug := flag.Bool("debug", false, "Enable debug logging for telnet negotiation")
	flag.Parse()

	fmt.Printf("Connecting to %s:%d...\n", *host, *port)
	client, err := network.Connect(*host, *port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Connection failed: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	// Ensure echo is restored on exit
	defer setLocalEcho(true)

	client.SetDebug(*debug)
	client.SetEchoCallback(setLocalEcho)

	fmt.Println("Connected! Reading data...")
	client.ReadLoop()
}
