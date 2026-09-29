package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"nexusgate/pkg/tui"
)

// RegisterRunCommand adds the 'run' command to the CLI app.
func RegisterRunCommand(a *App) {
	a.Register(&Command{
		Name:        "run",
		Aliases:     []string{"all-in-one", "dev"},
		Description: "Run the gateway engine in the background with an embedded interactive TUI dashboard",
		Usage:       "nexusgate run [-c <config>] [-s <socket>]",
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
				fmt.Fprintf(a.errOut, "[✗] Cannot run: configuration error: %v\n", err)
				return err
			}

			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			engineErrChan := make(chan error, 1)
			if EngineStarter != nil {
				go func() {
					engineErrChan <- EngineStarter(ctx, cfg, socketPath)
				}()
			}

			// Wait a short moment for socket initialization
			time.Sleep(100 * time.Millisecond)

			w, h := tui.GetTerminalDimensions(os.Stdout.Fd())
			client := tui.NewIPCClient(socketPath)
			app := tui.NewApp(w, h, os.Stdout, os.Stdin, client)

			guard := tui.NewTerminalGuard(os.Stdin.Fd(), os.Stdout)
			tuiErr := guard.WithRecovery(func() error {
				tui.WatchWindowSize(ctx, os.Stdout.Fd(), func(nw, nh int) {
					app.Renderer.Resize(nw, nh)
				})
				return app.Run(ctx)
			})

			// Cancel engine upon TUI exit
			cancel()

			select {
			case err := <-engineErrChan:
				if err != nil && err != context.Canceled {
					return err
				}
			default:
			}

			return tuiErr
		},
	})
}
