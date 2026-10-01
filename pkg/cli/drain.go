package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"nexusgate/pkg/tui"
)

// RegisterDrainCommand adds the 'drain' command to the CLI app.
func RegisterDrainCommand(a *App) {
	a.Register(&Command{
		Name:        "drain",
		Description: "Mark an upstream backend target as draining to gracefully bleed active connections",
		Usage:       "nexusgate drain <target-url> [--undo] [-s <socket>]",
		Run: func(ctx context.Context, args []string) error {
			flags, pos := ParseFlags(args)

			if len(pos) == 0 {
				return fmt.Errorf("missing target URL argument. Usage: %s", "nexusgate drain <target-url> [--undo]")
			}

			targetURL := pos[0]
			undo := false
			if u, ok := flags["undo"]; ok && u == "true" {
				undo = true
			}

			socketPath := tui.DefaultSocketPath()
			if s, ok := flags["s"]; ok {
				socketPath = s
			} else if s, ok := flags["socket"]; ok {
				socketPath = s
			}

			// Verify socket file exists
			if _, err := os.Stat(socketPath); err != nil {
				return fmt.Errorf("cannot connect to gateway: socket %q does not exist", socketPath)
			}

			cmd := fmt.Sprintf("DRAIN %s", targetURL)
			if undo {
				cmd = fmt.Sprintf("UNDRAIN %s", targetURL)
			}

			reply, err := SendIPCCommand(socketPath, cmd)
			if err != nil {
				fmt.Fprintf(a.errOut, "[✗] Drain failed: %v\n", err)
				return err
			}

			if strings.HasPrefix(reply, "OK") {
				action := "DRAINING"
				if undo {
					action = "ACTIVE (undrained)"
				}
				fmt.Fprintf(a.out, "[✓] Target backend %s is now %s (%s)\n", targetURL, action, reply)
				return nil
			}

			fmt.Fprintf(a.errOut, "[✗] Drain command rejected: %s\n", reply)
			return fmt.Errorf("gateway drain error: %s", reply)
		},
	})
}
