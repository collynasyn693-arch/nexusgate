package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"nexusgate/pkg/config"
	"nexusgate/pkg/tui"
)

// EngineStarter is a pluggable function that boots the full gateway engine.
var EngineStarter func(ctx context.Context, cfg *config.GatewayConfig, socketPath string) error

// RegisterStartCommand adds the 'start' command to the CLI app.
func RegisterStartCommand(a *App) {
	a.Register(&Command{
		Name:        "start",
		Description: "Start the NexusGate edge reverse proxy server in daemon/background mode",
		Usage:       "nexusgate start [-c <config>] [-s <socket>]",
		Run: func(ctx context.Context, args []string) error {
			flags, pos := ParseFlags(args)
			cfgPath := "nexusgate.yaml"
			if p, ok := flags["c"]; ok {
				cfgPath = p
			} else if p, ok := flags["config"]; ok {
				cfgPath = p
			} else if len(pos) > 0 {
				cfgPath = pos[0]
			}

			socketPath := tui.DefaultSocketPath()
			if s, ok := flags["s"]; ok {
				socketPath = s
			} else if s, ok := flags["socket"]; ok {
				socketPath = s
			}

			cfg, err := ValidateConfig(cfgPath)
			if err != nil {
				fmt.Fprintf(a.errOut, "[✗] Cannot start: configuration error: %v\n", err)
				return err
			}

			fmt.Fprintf(a.out, "Starting NexusGate on %s:%d (UDS IPC: %s)...\n",
				cfg.Listener.Host, cfg.Listener.Port, socketPath)

			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			sigChan := make(chan os.Signal, 2)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigChan
				fmt.Fprintf(a.out, "\nReceived shutdown signal, draining connections...\n")
				cancel()
			}()

			if EngineStarter != nil {
				return EngineStarter(ctx, cfg, socketPath)
			}

			fmt.Fprintf(a.out, "NexusGate gateway service running (press Ctrl+C to terminate)\n")
			<-ctx.Done()
			return nil
		},
	})
}
