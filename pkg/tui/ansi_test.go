package tui

import (
	"bytes"
	"strings"
	"testing"
)

func TestANSI_CursorMovements(t *testing.T) {
	seq := MoveCursor(0, 0)
	if seq != "\033[1;1H" {
		t.Fatalf("expected \\033[1;1H, got %q", seq)
	}

	seq2 := MoveCursor(15, 7)
	if seq2 != "\033[8;16H" {
		t.Fatalf("expected \\033[8;16H, got %q", seq2)
	}

	var buf bytes.Buffer
	AppendMoveCursor(&buf, 4, 9)
	if buf.String() != "\033[10;5H" {
		t.Fatalf("expected \\033[10;5H, got %q", buf.String())
	}
}

func TestANSI_Styles(t *testing.T) {
	var buf bytes.Buffer
	AppendStyle(&buf, StyleBold|StyleUnderline)
	out := buf.String()
	if !strings.Contains(out, "\033[1m") {
		t.Fatalf("expected bold code in %q", out)
	}
	if !strings.Contains(out, "\033[4m") {
		t.Fatalf("expected underline code in %q", out)
	}
}

func TestANSI_Colors(t *testing.T) {
	// Standard ANSI 16
	var buf bytes.Buffer
	AppendFgColor(&buf, ColorRed)
	if buf.String() != "\033[31m" {
		t.Fatalf("expected \\033[31m, got %q", buf.String())
	}

	buf.Reset()
	AppendBgColor(&buf, ColorBrightGreen)
	if buf.String() != "\033[102m" {
		t.Fatalf("expected \\033[102m, got %q", buf.String())
	}

	// 24-bit TrueColor
	buf.Reset()
	rgb := RGB(100, 150, 200)
	AppendFgColor(&buf, rgb)
	if buf.String() != "\033[38;2;100;150;200m" {
		t.Fatalf("expected RGB escape, got %q", buf.String())
	}

	buf.Reset()
	AppendBgColor(&buf, rgb)
	if buf.String() != "\033[48;2;100;150;200m" {
		t.Fatalf("expected RGB bg escape, got %q", buf.String())
	}

	// 256 Color
	buf.Reset()
	c256 := ANSI256(196)
	AppendFgColor(&buf, c256)
	if buf.String() != "\033[38;5;196m" {
		t.Fatalf("expected 256 color escape, got %q", buf.String())
	}
}

func TestANSI_FormatCellAttributes(t *testing.T) {
	var buf bytes.Buffer
	cell := Cell{
		R:     'X',
		Fg:    ColorGreen,
		Bg:    ColorBlack,
		Style: StyleBold,
	}
	FormatCellAttributes(&buf, cell)
	out := buf.String()

	if !strings.HasPrefix(out, EscSeqReset) {
		t.Fatalf("expected prefix reset, got %q", out)
	}
	if !strings.Contains(out, "\033[1m") {
		t.Fatalf("expected bold style, got %q", out)
	}
	if !strings.Contains(out, "\033[32m") {
		t.Fatalf("expected green fg, got %q", out)
	}
	if !strings.Contains(out, "\033[40m") {
		t.Fatalf("expected black bg, got %q", out)
	}
}
