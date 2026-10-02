package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCLI_Version_Default(t *testing.T) {
	app := NewApp()
	RegisterAllCommands(app)

	var out, errOut bytes.Buffer
	app.SetOutput(&out, &errOut)

	err := app.Execute(context.Background(), []string{"version"})
	if err != nil {
		t.Fatalf("unexpected error executing version: %v", err)
	}

	outStr := out.String()
	if !strings.Contains(outStr, "NexusGate v") {
		t.Errorf("expected version banner, got: %q", outStr)
	}
	if !strings.Contains(outStr, "Git Commit:") {
		t.Errorf("expected Git Commit field, got: %q", outStr)
	}
	if !strings.Contains(outStr, "Target:     termux/arm64") {
		t.Errorf("expected target termux/arm64, got: %q", outStr)
	}
}

func TestCLI_Version_JSON(t *testing.T) {
	app := NewApp()
	RegisterAllCommands(app)

	var out, errOut bytes.Buffer
	app.SetOutput(&out, &errOut)

	err := app.Execute(context.Background(), []string{"version", "--json"})
	if err != nil {
		t.Fatalf("unexpected error executing version --json: %v", err)
	}

	var data map[string]string
	if err := json.Unmarshal(out.Bytes(), &data); err != nil {
		t.Fatalf("failed to unmarshal json version output: %v, raw: %q", err, out.String())
	}

	if data["version"] == "" {
		t.Errorf("version key is empty")
	}
	if data["platform"] == "" {
		t.Errorf("platform key is empty")
	}
	if data["target"] != "termux/arm64" {
		t.Errorf("expected target termux/arm64, got: %q", data["target"])
	}
}

func TestCLI_Version_Aliases(t *testing.T) {
	for _, alias := range []string{"-v", "--version"} {
		app := NewApp()
		RegisterAllCommands(app)

		var out, errOut bytes.Buffer
		app.SetOutput(&out, &errOut)

		err := app.Execute(context.Background(), []string{alias})
		if err != nil {
			t.Fatalf("unexpected error executing %s: %v", alias, err)
		}

		if !strings.Contains(out.String(), "NexusGate v") {
			t.Errorf("expected version output for alias %s, got: %q", alias, out.String())
		}
	}
}

func TestCLI_Help_AllCommands(t *testing.T) {
	cases := [][]string{
		{},
		{"help"},
		{"-h"},
		{"--help"},
	}

	for _, tc := range cases {
		app := NewApp()
		RegisterAllCommands(app)

		var out, errOut bytes.Buffer
		app.SetOutput(&out, &errOut)

		err := app.Execute(context.Background(), tc)
		if err != nil {
			t.Fatalf("unexpected error executing %v: %v", tc, err)
		}

		outStr := out.String()
		expected := []string{"start", "run", "attach", "validate", "reload", "drain", "version", "help"}
		for _, cmd := range expected {
			if !strings.Contains(outStr, cmd) {
				t.Errorf("for args %v, expected help to mention %q, got: %q", tc, cmd, outStr)
			}
		}
	}
}

func TestCLI_Help_Subcommands(t *testing.T) {
	app := NewApp()
	RegisterAllCommands(app)

	var out, errOut bytes.Buffer
	app.SetOutput(&out, &errOut)

	err := app.Execute(context.Background(), []string{"help", "validate"})
	if err != nil {
		t.Fatalf("unexpected error executing help validate: %v", err)
	}

	if !strings.Contains(out.String(), "Usage: nexusgate validate") {
		t.Errorf("expected validate usage, got: %q", out.String())
	}

	// Unknown subcommand help
	out.Reset()
	err = app.Execute(context.Background(), []string{"help", "nonexistent"})
	if err == nil {
		t.Errorf("expected error for help nonexistent")
	}
}

func TestCLI_UnknownCommand(t *testing.T) {
	app := NewApp()
	RegisterAllCommands(app)

	var out, errOut bytes.Buffer
	app.SetOutput(&out, &errOut)

	err := app.Execute(context.Background(), []string{"foobar123"})
	if err == nil {
		t.Fatalf("expected error executing unknown command")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("unexpected error message: %v", err)
	}
}
