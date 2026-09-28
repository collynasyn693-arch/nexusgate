package tui

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// TerminalGuard handles safe entry and exit from raw alternate terminal mode,
// guaranteeing clean terminal restoration even under panics or unexpected signals.
type TerminalGuard struct {
	fd          uintptr
	out         io.Writer
	origTermios *syscall.Termios
	mu          sync.Mutex
	active      bool
}

// NewTerminalGuard creates a guard for the specified standard file descriptors.
func NewTerminalGuard(fd uintptr, out io.Writer) *TerminalGuard {
	return &TerminalGuard{
		fd:  fd,
		out: out,
	}
}

// Enter enables the alternate screen buffer, hides the cursor, and configures raw mode.
func (g *TerminalGuard) Enter() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.active {
		return nil
	}

	// 1. Enter alternate screen buffer & hide cursor
	enterSeq := EscSeqEnterAltScreen + EscSeqHideCursor + EscSeqClearScreen + EscSeqCursorHome
	if _, err := io.WriteString(g.out, enterSeq); err != nil {
		return err
	}

	// 2. Put terminal into raw mode if valid tty fd
	if g.fd != 0 {
		orig, err := MakeRaw(g.fd)
		if err == nil {
			g.origTermios = orig
		}
	}

	g.active = true
	return nil
}

// Exit restores the main screen buffer, unhides the cursor, and resets termios.
func (g *TerminalGuard) Exit() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.active {
		return nil
	}

	// 1. Restore terminal mode
	if g.origTermios != nil && g.fd != 0 {
		_ = RestoreTerminal(g.fd, g.origTermios)
		g.origTermios = nil
	}

	// 2. Exit alternate screen, restore cursor, reset colors
	exitSeq := EscSeqReset + EscSeqShowCursor + EscSeqExitAltScreen
	_, err := io.WriteString(g.out, exitSeq)

	g.active = false
	return err
}

// WithRecovery executes fn while ensuring the terminal state is restored on exit or panic.
func (g *TerminalGuard) WithRecovery(fn func() error) (err error) {
	if err := g.Enter(); err != nil {
		return err
	}

	// Catch emergency interrupt signals
	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sigChan
		_ = g.Exit()
		os.Exit(1)
	}()
	defer signal.Stop(sigChan)

	defer func() {
		restoreErr := g.Exit()
		if r := recover(); r != nil {
			err = fmt.Errorf("tui panic recovered: %v", r)
		} else if restoreErr != nil && err == nil {
			err = restoreErr
		}
	}()

	return fn()
}
