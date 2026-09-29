package cli

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"nexusgate/pkg/tui"
)

// SendIPCCommand dials the gateway socket, writes a line command, and reads the response.
func SendIPCCommand(socketPath, command string) (string, error) {
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return "", fmt.Errorf("failed to connect to gateway socket %q: %w", socketPath, err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	// Send command with newline
	if !strings.HasSuffix(command, "\n") {
		command += "\n"
	}
	if _, err := conn.Write([]byte(command)); err != nil {
		return "", fmt.Errorf("failed to send command to socket: %w", err)
	}

	// Read reply
	reader := bufio.NewReader(conn)
	reply, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read response from gateway: %w", err)
	}

	return strings.TrimSpace(reply), nil
}

// RegisterReloadCommand adds the 'reload' command to the CLI app.
func RegisterReloadCommand(a *App) {
	a.Register(&Command{
		Name:        "reload",
		Description: "Trigger a zero-downtime atomic configuration reload on the running gateway",
		Usage:       "nexusgate reload [-c <config>] [-s <socket>]",
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

			// Validate config file before sending reload request
			if _, err := ValidateConfig(cfgPath); err != nil {
				fmt.Fprintf(a.errOut, "[✗] Rejecting reload: configuration is invalid: %v\n", err)
				return err
			}

			// Verify socket file exists
			if _, err := os.Stat(socketPath); err != nil {
				return fmt.Errorf("cannot connect to gateway: socket %q does not exist", socketPath)
			}

			reply, err := SendIPCCommand(socketPath, fmt.Sprintf("RELOAD %s", cfgPath))
			if err != nil {
				fmt.Fprintf(a.errOut, "[✗] Reload failed: %v\n", err)
				return err
			}

			if strings.HasPrefix(reply, "OK") {
				fmt.Fprintf(a.out, "[✓] Configuration reload successful: %s\n", reply)
				return nil
			}

			fmt.Fprintf(a.errOut, "[✗] Reload rejected by gateway: %s\n", reply)
			return fmt.Errorf("gateway reload error: %s", reply)
		},
	})
}
