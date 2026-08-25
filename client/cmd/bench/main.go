package main

import (
	"flag"
	"fmt"
	"os"

	"hy_client/pkg/client"
)

func main() {
	configPath := flag.String("config", "bench.yaml", "path to config file")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	client.RegisterMessages()

	mgr := NewBotManager(cfg)
	mgr.Run()
}
