package tui

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestDecodeKeySequence_RunesAndControls(t *testing.T) {
	// 'q' for quit
	ev, n := DecodeKeySequence([]byte("q"))
	if ev.Type != KeyRune || ev.Rune != 'q' || n != 1 {
		t.Fatalf("expected KeyRune('q'), got %v, consumed %d", ev, n)
	}

	// 'r' for reload
	ev, n = DecodeKeySequence([]byte("r"))
	if ev.Type != KeyRune || ev.Rune != 'r' || n != 1 {
		t.Fatalf("expected KeyRune('r'), got %v, consumed %d", ev, n)
	}

	// 'd' for drain
	ev, n = DecodeKeySequence([]byte("d"))
	if ev.Type != KeyRune || ev.Rune != 'd' || n != 1 {
		t.Fatalf("expected KeyRune('d'), got %v, consumed %d", ev, n)
	}

	// 'm' for mute
	ev, n = DecodeKeySequence([]byte("m"))
	if ev.Type != KeyRune || ev.Rune != 'm' || n != 1 {
		t.Fatalf("expected KeyRune('m'), got %v, consumed %d", ev, n)
	}

	// Ctrl-C
	ev, n = DecodeKeySequence([]byte{0x03})
	if ev.Type != KeyCtrlC || n != 1 {
		t.Fatalf("expected KeyCtrlC, got %v, consumed %d", ev, n)
	}

	// Enter
	ev, n = DecodeKeySequence([]byte{0x0d})
	if ev.Type != KeyEnter || n != 1 {
		t.Fatalf("expected KeyEnter, got %v, consumed %d", ev, n)
	}
}

func TestDecodeKeySequence_EscapeSequences(t *testing.T) {
	// Standalone Esc
	ev, n := DecodeKeySequence([]byte{0x1b})
	if ev.Type != KeyEsc || n != 1 {
		t.Fatalf("expected KeyEsc, got %v, consumed %d", ev, n)
	}

	// KeyUp \033[A
	ev, n = DecodeKeySequence([]byte("\033[A"))
	if ev.Type != KeyUp || n != 3 {
		t.Fatalf("expected KeyUp, got %v, consumed %d", ev, n)
	}

	// KeyDown \033[B
	ev, n = DecodeKeySequence([]byte("\033[B"))
	if ev.Type != KeyDown || n != 3 {
		t.Fatalf("expected KeyDown, got %v, consumed %d", ev, n)
	}

	// KeyPageUp \033[5~
	ev, n = DecodeKeySequence([]byte("\033[5~"))
	if ev.Type != KeyPageUp || n != 4 {
		t.Fatalf("expected KeyPageUp, got %v, consumed %d", ev, n)
	}
}

func TestInputReader_Stream(t *testing.T) {
	inputBuf := bytes.NewBuffer([]byte("q\033[Ar"))
	ir := NewInputReader(inputBuf)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ir.Start(ctx)

	expectedKeys := []KeyType{KeyRune, KeyUp, KeyRune}
	for _, exp := range expectedKeys {
		select {
		case ev := <-ir.Events():
			if ev.Type != exp {
				t.Fatalf("expected key %v, got %v", exp, ev.Type)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("timed out waiting for key event")
		}
	}
}
