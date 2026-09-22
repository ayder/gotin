package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/ayder/gotin/internal/app"
	"github.com/ayder/gotin/internal/version"
)

func main() {
	hostFlag := flag.String("host", "", "MUD server hostname")
	portFlag := flag.Int("port", 0, "MUD server port")
	debug := flag.Bool("debug", false, "Enable debug logging for telnet negotiation")
	versionFlag := flag.Bool("version", false, "Print the client version and exit")
	flag.Parse()
	if *versionFlag {
		fmt.Println("gotin " + version.Version)
		return
	}

	if err := app.Run(context.Background(), app.Options{
		Host:  *hostFlag,
		Port:  *portFlag,
		Debug: *debug,
	}); err != nil {
		log.Fatal(err)
	}
}
