package tui

import (
	"bytes"
	"strings"
	"testing"
)

func TestScreenRenderer_InitialFlush(t *testing.T) {
	var out bytes.Buffer
	renderer := NewScreenRenderer(20, 5, &out)

	renderer.Buffer().SetString(0, 0, "NexusGate", ColorGreen, ColorDefault, StyleBold)
	n, err := renderer.Flush()
	if err != nil {
		t.Fatalf("unexpected flush error: %v", err)
	}
	if n == 0 {
		t.Fatalf("expected non-zero bytes on initial flush, got %d", n)
	}
	content := out.String()
	if !strings.Contains(content, "NexusGate") {
		t.Fatalf("expected output to contain 'NexusGate', got %q", content)
	}
	if !strings.Contains(content, EscSeqClearScreen) {
		t.Fatalf("expected initial flush to contain clear screen sequence")
	}
}

func TestScreenRenderer_ZeroDiffOnIdenticalFrames(t *testing.T) {
	var out bytes.Buffer
	renderer := NewScreenRenderer(20, 5, &out)

	renderer.Buffer().SetString(0, 0, "NexusGate", ColorGreen, ColorDefault, StyleBold)
	_, _ = renderer.Flush()

	// Second flush with no changes to Back buffer
	out.Reset()
	n, err := renderer.Flush()
	if err != nil {
		t.Fatalf("unexpected flush error: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected exactly 0 bytes written on identical frame, got %d bytes: %q", n, out.String())
	}
}

func TestScreenRenderer_SingleCellDiff(t *testing.T) {
	var out bytes.Buffer
	renderer := NewScreenRenderer(20, 5, &out)

	renderer.Buffer().SetString(0, 0, "NexusGate", ColorGreen, ColorDefault, StyleBold)
	_, _ = renderer.Flush()

	out.Reset()
	// Change only cell at (5, 0) from 'G' to 'X'
	renderer.Buffer().SetRune(5, 0, 'X', ColorRed, ColorDefault, StyleNone)

	n, err := renderer.Flush()
	if err != nil {
		t.Fatalf("flush error: %v", err)
	}
	if n == 0 {
		t.Fatalf("expected non-zero diff bytes")
	}

	content := out.String()
	if !strings.Contains(content, "X") {
		t.Fatalf("expected diff to contain 'X'")
	}
	// Verify it does NOT contain the full word "Nexus" again
	if strings.Contains(content, "Nexus") {
		t.Fatalf("diff should not redraw unchanged characters 'Nexus'")
	}
}

func TestScreenRenderer_ResizeAndClipping(t *testing.T) {
	var out bytes.Buffer
	renderer := NewScreenRenderer(10, 5, &out)

	// Fill region
	renderer.Buffer().SetString(0, 0, "0123456789", ColorWhite, ColorDefault, StyleNone)
	// Clipping check
	renderer.Buffer().SetString(8, 0, "EXCEEDING", ColorWhite, ColorDefault, StyleNone)

	c := renderer.Buffer().Get(9, 0)
	if c.R != 'X' {
		t.Fatalf("expected clipped char 'X' at index 9, got %c", c.R)
	}

	// Out of bounds get
	oob := renderer.Buffer().Get(15, 0)
	if oob.R != ' ' {
		t.Fatalf("expected empty space for OOB get, got %c", oob.R)
	}

	// Resize
	renderer.Resize(30, 10)
	if renderer.Width != 30 || renderer.Height != 10 {
		t.Fatalf("expected 30x10 dimensions after resize")
	}
}
