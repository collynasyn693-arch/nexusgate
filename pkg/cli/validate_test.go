package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_Validate_ValidConfig(t *testing.T) {
	app := NewApp()
	RegisterAllCommands(app)

	var out bytes.Buffer
	var errOut bytes.Buffer
	app.SetOutput(&out, &errOut)

	err := app.Execute(context.Background(), []string{"validate", "-c", "../../nexusgate.example.yaml"})
	if err != nil {
		// Try local relative path if working directory is root
		err = app.Execute(context.Background(), []string{"validate", "-c", "nexusgate.example.yaml"})
	}

	if err != nil {
		t.Fatalf("expected valid config to pass validation, got: %v", err)
	}

	outStr := out.String()
	if !strings.Contains(outStr, "[✓]") || !strings.Contains(outStr, "VALID") {
		t.Fatalf("expected validation success message, got: %q", outStr)
	}
}

func TestCLI_Validate_NonExistentFile(t *testing.T) {
	app := NewApp()
	RegisterAllCommands(app)

	var out bytes.Buffer
	var errOut bytes.Buffer
	app.SetOutput(&out, &errOut)

	err := app.Execute(context.Background(), []string{"validate", "-c", "/path/to/nonexistent/file.yaml"})
	if err == nil {
		t.Fatalf("expected validation error for non-existent file")
	}

	errStr := errOut.String()
	if !strings.Contains(errStr, "[✗] FAILED") {
		t.Fatalf("expected failure message in stderr, got: %q", errStr)
	}
}

func TestCLI_Validate_InvalidSyntax(t *testing.T) {
	tempDir := t.TempDir()
	badFile := filepath.Join(tempDir, "bad.json")
	_ = os.WriteFile(badFile, []byte("{ invalid json syntax }"), 0644)

	app := NewApp()
	RegisterAllCommands(app)

	var out bytes.Buffer
	var errOut bytes.Buffer
	app.SetOutput(&out, &errOut)

	err := app.Execute(context.Background(), []string{"validate", "-c", badFile})
	if err == nil {
		t.Fatalf("expected validation error for bad YAML syntax")
	}
}

func TestCLI_Validate_PrivilegedPort(t *testing.T) {
	tempDir := t.TempDir()
	privPortFile := filepath.Join(tempDir, "priv.yaml")
	content := `version: "1.0"
listener:
  host: "0.0.0.0"
  port: 80
`
	_ = os.WriteFile(privPortFile, []byte(content), 0644)

	app := NewApp()
	RegisterAllCommands(app)

	var out bytes.Buffer
	var errOut bytes.Buffer
	app.SetOutput(&out, &errOut)

	err := app.Execute(context.Background(), []string{"validate", "-c", privPortFile})
	if err == nil {
		t.Fatalf("expected validation error for privileged port 80 in userland Termux")
	}
}
