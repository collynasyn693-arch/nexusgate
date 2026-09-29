package cli

import (
	"context"
	"fmt"
	"os"

	"nexusgate/pkg/tui"
)

// RegisterAttachCommand adds the 'attach' command to the CLI app.
func RegisterAttachCommand(a *App) {
	a.Register(&Command{
		Name:        "attach",
		Aliases:     []string{"top", "hud", "tui"},
		Description: "Attach an interactive Terminal Observatory HUD to a running NexusGate instance",
		Usage:       "nexusgate attach [-s <socket>]",
		Run: func(ctx context.Context, args []string) error {
			flags, pos := ParseFlags(args)
			socketPath := tui.DefaultSocketPath()
			if s, ok := flags["s"]; ok {
				socketPath = s
			} else if s, ok := flags["socket"]; ok {
				socketPath = s
			} else if len(pos) > 0 {
				socketPath = pos[0]
			}

			// Check socket exists
			if fi, err := os.Stat(socketPath); err != nil || fi.Mode()&os.ModeSocket == 0 {
				return fmt.Errorf("cannot connect to NexusGate: socket %q not found or not active", socketPath)
			}

			w, h := tui.GetTerminalDimensions(os.Stdout.Fd())
			client := tui.NewIPCClient(socketPath)
			app := tui.NewApp(w, h, os.Stdout, os.Stdin, client)

			guard := tui.NewTerminalGuard(os.Stdin.Fd(), os.Stdout)
			return guard.WithRecovery(func() error {
				tui.WatchWindowSize(ctx, os.Stdout.Fd(), func(nw, nh int) {
					app.Renderer.Resize(nw, nh)
				})
				return app.Run(ctx)
			})
		},
	})
}
