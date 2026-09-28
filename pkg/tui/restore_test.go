package tui

import (
	"bytes"
	"strings"
	"testing"
)

func TestTerminalGuard_EnterExit(t *testing.T) {
	var out bytes.Buffer
	guard := NewTerminalGuard(0, &out)

	// Test Enter
	if err := guard.Enter(); err != nil {
		t.Fatalf("unexpected enter error: %v", err)
	}

	enterStr := out.String()
	if !strings.Contains(enterStr, EscSeqEnterAltScreen) {
		t.Fatalf("expected alt screen sequence in %q", enterStr)
	}
	if !strings.Contains(enterStr, EscSeqHideCursor) {
		t.Fatalf("expected hide cursor sequence in %q", enterStr)
	}

	// Test Exit
	out.Reset()
	if err := guard.Exit(); err != nil {
		t.Fatalf("unexpected exit error: %v", err)
	}

	exitStr := out.String()
	if !strings.Contains(exitStr, EscSeqExitAltScreen) {
		t.Fatalf("expected exit alt screen sequence in %q", exitStr)
	}
	if !strings.Contains(exitStr, EscSeqShowCursor) {
		t.Fatalf("expected show cursor sequence in %q", exitStr)
	}
}

func TestTerminalGuard_PanicRecovery(t *testing.T) {
	var out bytes.Buffer
	guard := NewTerminalGuard(0, &out)

	err := guard.WithRecovery(func() error {
		panic("simulated fatal crash in TUI render")
	})

	if err == nil {
		t.Fatalf("expected error from panic recovery, got nil")
	}
	if !strings.Contains(err.Error(), "simulated fatal crash") {
		t.Fatalf("unexpected error message: %v", err)
	}

	// Verify terminal restoration occurred
	exitStr := out.String()
	if !strings.Contains(exitStr, EscSeqExitAltScreen) {
		t.Fatalf("expected exit alt screen sequence on panic recovery")
	}
	if !strings.Contains(exitStr, EscSeqShowCursor) {
		t.Fatalf("expected show cursor sequence on panic recovery")
	}
}

func TestTerminalGuard_Idempotence(t *testing.T) {
	var out bytes.Buffer
	guard := NewTerminalGuard(0, &out)

	_ = guard.Enter()
	len1 := out.Len()
	_ = guard.Enter() // Duplicate enter
	if out.Len() != len1 {
		t.Fatalf("duplicate Enter() should not write duplicate escape sequences")
	}

	_ = guard.Exit()
	lenExit1 := out.Len()
	_ = guard.Exit() // Duplicate exit
	if out.Len() != lenExit1 {
		t.Fatalf("duplicate Exit() should not write duplicate escape sequences")
	}
}
