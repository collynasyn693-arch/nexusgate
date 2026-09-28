package tui

import (
	"context"
	"io"
	"syscall"
	"unicode/utf8"
	"unsafe"
)

// MakeRaw puts the terminal file descriptor into non-blocking raw mode,
// returning the original Termios state for later restoration.
func MakeRaw(fd uintptr) (*syscall.Termios, error) {
	var orig syscall.Termios
	_, _, err := syscall.Syscall(
		syscall.SYS_IOCTL,
		fd,
		uintptr(syscall.TCGETS),
		uintptr(unsafe.Pointer(&orig)),
	)
	if err != 0 {
		return nil, err
	}

	raw := orig
	// Clear ECHO, ICANON (canonical mode), ISIG (signals), IEXTEN
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	// Clear IXON (software flow control), ICRNL (map CR to NL)
	raw.Iflag &^= syscall.IXON | syscall.ICRNL
	// Minimum 1 byte, 0 timeout
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0

	_, _, err = syscall.Syscall(
		syscall.SYS_IOCTL,
		fd,
		uintptr(syscall.TCSETS),
		uintptr(unsafe.Pointer(&raw)),
	)
	if err != 0 {
		return nil, err
	}

	return &orig, nil
}

// RestoreTerminal restores the original terminal mode.
func RestoreTerminal(fd uintptr, orig *syscall.Termios) error {
	if orig == nil {
		return nil
	}
	_, _, err := syscall.Syscall(
		syscall.SYS_IOCTL,
		fd,
		uintptr(syscall.TCSETS),
		uintptr(unsafe.Pointer(orig)),
	)
	if err != 0 {
		return err
	}
	return nil
}

// DecodeKeySequence decodes a byte slice into a KeyEvent and returns consumed byte count.
func DecodeKeySequence(b []byte) (KeyEvent, int) {
	if len(b) == 0 {
		return KeyEvent{Type: KeyUnknown}, 0
	}

	// Escape sequences
	if b[0] == 0x1b {
		if len(b) == 1 {
			return KeyEvent{Type: KeyEsc}, 1
		}

		if b[1] == '[' {
			if len(b) >= 3 {
				switch b[2] {
				case 'A':
					return KeyEvent{Type: KeyUp}, 3
				case 'B':
					return KeyEvent{Type: KeyDown}, 3
				case 'C':
					return KeyEvent{Type: KeyRight}, 3
				case 'D':
					return KeyEvent{Type: KeyLeft}, 3
				case 'H':
					return KeyEvent{Type: KeyHome}, 3
				case 'F':
					return KeyEvent{Type: KeyEnd}, 3
				case '1', '2', '3', '4', '5', '6':
					if len(b) >= 4 && b[3] == '~' {
						switch b[2] {
						case '5':
							return KeyEvent{Type: KeyPageUp}, 4
						case '6':
							return KeyEvent{Type: KeyPageDown}, 4
						}
					}
				}
			}
		}
		return KeyEvent{Type: KeyEsc}, 1
	}

	// Control keys
	switch b[0] {
	case 0x01: // Ctrl-A
		return KeyEvent{Type: KeyRune, Rune: 'a', Mod: ModCtrl}, 1
	case 0x03: // Ctrl-C
		return KeyEvent{Type: KeyCtrlC, Mod: ModCtrl}, 1
	case 0x04: // Ctrl-D
		return KeyEvent{Type: KeyCtrlD, Mod: ModCtrl}, 1
	case 0x0c: // Ctrl-L
		return KeyEvent{Type: KeyCtrlL, Mod: ModCtrl}, 1
	case 0x12: // Ctrl-R
		return KeyEvent{Type: KeyCtrlR, Mod: ModCtrl}, 1
	case 0x0d, 0x0a: // Enter
		return KeyEvent{Type: KeyEnter}, 1
	case 0x09: // Tab
		return KeyEvent{Type: KeyTab}, 1
	case 0x7f, 0x08: // Backspace
		return KeyEvent{Type: KeyBackspace}, 1
	}

	// Standard UTF-8 rune decoding
	r, size := utf8.DecodeRune(b)
	if r != utf8.RuneError {
		return KeyEvent{Type: KeyRune, Rune: r}, size
	}

	return KeyEvent{Type: KeyUnknown}, 1
}

// InputReader listens for terminal input and publishes decoded KeyEvents.
type InputReader struct {
	reader    io.Reader
	eventChan chan KeyEvent
}

// NewInputReader creates a new InputReader.
func NewInputReader(r io.Reader) *InputReader {
	return &InputReader{
		reader:    r,
		eventChan: make(chan KeyEvent, 32),
	}
}

// Events returns the receive-only channel of KeyEvents.
func (ir *InputReader) Events() <-chan KeyEvent {
	return ir.eventChan
}

// Start begins the background input reading loop until the context is canceled.
func (ir *InputReader) Start(ctx context.Context) {
	go func() {
		defer close(ir.eventChan)
		buf := make([]byte, 64)

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			n, err := ir.reader.Read(buf)
			if err != nil {
				return
			}
			if n <= 0 {
				continue
			}

			offset := 0
			for offset < n {
				ev, consumed := DecodeKeySequence(buf[offset:n])
				if consumed <= 0 {
					consumed = 1
				}
				offset += consumed

				if ev.Type != KeyUnknown {
					select {
					case ir.eventChan <- ev:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
}
