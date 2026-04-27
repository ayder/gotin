package main

import (
	"context"
	"flag"
	"log"

	"github.com/ayder/gotin/internal/app"
)

func main() {
	hostFlag := flag.String("host", "", "MUD server hostname")
	portFlag := flag.Int("port", 0, "MUD server port")
	debug := flag.Bool("debug", false, "Enable debug logging for telnet negotiation")
	flag.Parse()

	if err := app.Run(context.Background(), app.Options{
		Host:  *hostFlag,
		Port:  *portFlag,
		Debug: *debug,
	}); err != nil {
		log.Fatal(err)
	}
}
