package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ThorstenRhau/Python_REST_Tracing_Demo/internal/otelbackend"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:4317", "OTLP gRPC listen address")
	dbPath := flag.String("db", "", "optional SQLite database path")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store := otelbackend.NewStore()
	var persistence *otelbackend.Persistence
	if *dbPath != "" {
		var err error
		persistence, err = otelbackend.OpenPersistence(ctx, *dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "open sqlite: %v\n", err)
			os.Exit(1)
		}
		defer persistence.Close()
	}

	server, err := otelbackend.StartGRPC(*listen, otelbackend.NewReceiver(store, persistence))
	if err != nil {
		fmt.Fprintf(os.Stderr, "start grpc: %v\n", err)
		os.Exit(1)
	}
	defer server.Stop()

	go func() {
		<-ctx.Done()
		server.Stop()
	}()

	if err := otelbackend.NewTUI(store).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "run tui: %v\n", err)
		os.Exit(1)
	}
}
