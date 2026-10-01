package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"nexusgate/pkg/cli"
	"nexusgate/pkg/config"
	"nexusgate/pkg/engine"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	cli.EngineStarter = func(ctx context.Context, cfg *config.GatewayConfig, socketPath string) error {
		sup, err := engine.NewSupervisor(cfg, socketPath)
		if err != nil {
			return err
		}
		return sup.Serve(ctx)
	}

	app := cli.NewApp()
	cli.RegisterAllCommands(app)

	if err := app.Execute(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
