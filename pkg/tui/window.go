package tui

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"unsafe"
)

type winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

// GetTerminalDimensions queries the terminal window size using ioctl TIOCGWINSZ,
// falling back to environment variables (COLUMNS/LINES) or default (80x24).
func GetTerminalDimensions(fd uintptr) (int, int) {
	var ws winsize
	_, _, err := syscall.Syscall(
		syscall.SYS_IOCTL,
		fd,
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&ws)),
	)
	if err == 0 && ws.Col > 0 && ws.Row > 0 {
		return int(ws.Col), int(ws.Row)
	}

	// Fallback to environment variables
	cols := 80
	rows := 24
	if c := os.Getenv("COLUMNS"); c != "" {
		if v, err := strconv.Atoi(c); err == nil && v > 0 {
			cols = v
		}
	}
	if r := os.Getenv("LINES"); r != "" {
		if v, err := strconv.Atoi(r); err == nil && v > 0 {
			rows = v
		}
	}
	return cols, rows
}

// WatchWindowSize listens for SIGWINCH signals and invokes onResize with the updated terminal dimensions.
func WatchWindowSize(ctx context.Context, fd uintptr, onResize func(w, h int)) {
	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, syscall.SIGWINCH)

	go func() {
		defer signal.Stop(sigChan)
		for {
			select {
			case <-ctx.Done():
				return
			case <-sigChan:
				w, h := GetTerminalDimensions(fd)
				if onResize != nil {
					onResize(w, h)
				}
			}
		}
	}()
}
