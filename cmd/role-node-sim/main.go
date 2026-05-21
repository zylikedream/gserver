package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"gserver/core/gxyregistery"
	"gserver/internal/rolenodesim"
)

func main() {
	var (
		count       = flag.Int("count", 1000, "number of simulated role nodes")
		concurrency = flag.Int("concurrency", 32, "parallel registration workers")
		duration    = flag.Duration("duration", 0, "how long to keep the simulated nodes registered; 0 waits for interrupt")
		serviceName = flag.String("service-name", "role", "registry service name")
		nodePrefix  = flag.String("node-prefix", "role-sim", "node name prefix")
		hostPrefix  = flag.String("host-prefix", "127.0.0.1", "node host prefix")
		startPort   = flag.Int("start-port", 19000, "starting port used to generate unique node hosts")
		version     = flag.String("version", "sim", "service version")
		weight      = flag.Int("weight", 1, "service weight")
	)
	flag.Parse()

	registry, err := gxyregistery.NewRegistery()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "init registry failed: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := rolenodesim.Options{
		Count:       *count,
		ServiceName: *serviceName,
		NodePrefix:  *nodePrefix,
		HostPrefix:  *hostPrefix,
		StartPort:   *startPort,
		Version:     *version,
		Weight:      *weight,
		Concurrency: *concurrency,
		Duration:    *duration,
	}

	if err := rolenodesim.Run(ctx, registry, opts, os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "role node simulator failed: %v\n", err)
		os.Exit(1)
	}
}
