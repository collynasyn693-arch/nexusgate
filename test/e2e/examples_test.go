package e2e

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"nexusgate/pkg/cli"
	"nexusgate/pkg/config"
)

func TestExamples_AllConfigsValid(t *testing.T) {
	exampleFiles := []string{
		"../../nexusgate.example.yaml",
		"../../examples/microservices.yaml",
		"../../examples/mobile_dev_gateway.yaml",
		"../../examples/battery_saver.yaml",
	}

	for _, relPath := range exampleFiles {
		absPath, err := filepath.Abs(relPath)
		if err != nil {
			t.Fatalf("failed to resolve path %s: %v", relPath, err)
		}

		t.Run(filepath.Base(absPath), func(t *testing.T) {
			// 1. Semantic validator verification
			cfg, err := config.ParseFile(absPath)
			if err != nil {
				t.Fatalf("failed to load %s: %v", absPath, err)
			}

			if err := config.Validate(cfg); err != nil {
				t.Fatalf("validation failed for %s: %v", absPath, err)
			}

			// 2. CLI validate command verification
			app := cli.NewApp()
			cli.RegisterAllCommands(app)

			var out, errOut bytes.Buffer
			app.SetOutput(&out, &errOut)

			if err := app.Execute(context.Background(), []string{"validate", "-c", absPath}); err != nil {
				t.Fatalf("CLI validate command failed for %s: %v, stderr: %s", absPath, err, errOut.String())
			}

			if !strings.Contains(out.String(), "VALID") {
				t.Errorf("expected validation success message in stdout, got: %q", out.String())
			}
		})
	}
}
