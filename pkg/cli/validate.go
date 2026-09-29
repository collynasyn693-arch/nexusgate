package cli

import (
	"context"
	"fmt"
	"os"

	"nexusgate/pkg/config"
)

// ValidateConfig reads, parses, defaults, and validates a configuration file.
func ValidateConfig(path string) (*config.GatewayConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read configuration file: %w", err)
	}

	cfg, err := config.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("configuration syntax error: %w", err)
	}

	config.ApplyDefaults(cfg)

	if err := config.Validate(cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
}

// RegisterValidateCommand adds the 'validate' command to the CLI app.
func RegisterValidateCommand(a *App) {
	a.Register(&Command{
		Name:        "validate",
		Aliases:     []string{"check"},
		Description: "Validate syntax and semantics of a NexusGate configuration file",
		Usage:       "nexusgate validate [-c <path>]",
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

			cfg, err := ValidateConfig(cfgPath)
			if err != nil {
				fmt.Fprintf(a.errOut, "[✗] FAILED: %v\n", err)
				return err
			}

			fmt.Fprintf(a.out, "[✓] Configuration %q is VALID\n", cfgPath)
			fmt.Fprintf(a.out, "    Listener: %s:%d (Version %s)\n", cfg.Listener.Host, cfg.Listener.Port, cfg.Version)
			fmt.Fprintf(a.out, "    Routes: %d route(s) registered\n", len(cfg.Routes))
			totalTargets := 0
			for _, u := range cfg.Upstreams {
				totalTargets += len(u.Targets)
			}
			fmt.Fprintf(a.out, "    Upstreams: %d pool(s) with %d target endpoint(s)\n", len(cfg.Upstreams), totalTargets)
			return nil
		},
	})
}
