package tui

import (
	"bytes"
	"fmt"
	"strconv"
)

// Standard ANSI control escape sequences.
const (
	EscSeqReset          = "\033[0m"
	EscSeqHideCursor     = "\033[?25l"
	EscSeqShowCursor     = "\033[?25h"
	EscSeqEnterAltScreen = "\033[?1049h"
	EscSeqExitAltScreen  = "\033[?1049l"
	EscSeqClearScreen    = "\033[2J"
	EscSeqClearLine      = "\033[2K"
	EscSeqCursorHome     = "\033[H"
)

// MoveCursor returns the escape sequence to move cursor to 0-indexed (x, y).
// Terminal coordinates are 1-indexed.
func MoveCursor(x, y int) string {
	return fmt.Sprintf("\033[%d;%dH", y+1, x+1)
}

// AppendMoveCursor appends the cursor movement sequence to a byte slice buffer without heap allocs.
func AppendMoveCursor(buf *bytes.Buffer, x, y int) {
	buf.WriteString("\033[")
	buf.WriteString(strconv.Itoa(y + 1))
	buf.WriteByte(';')
	buf.WriteString(strconv.Itoa(x + 1))
	buf.WriteByte('H')
}

// AppendStyle appends SGR style attributes to the buffer.
func AppendStyle(buf *bytes.Buffer, s Style) {
	if s == StyleNone {
		return
	}
	if s&StyleBold != 0 {
		buf.WriteString("\033[1m")
	}
	if s&StyleDim != 0 {
		buf.WriteString("\033[2m")
	}
	if s&StyleItalic != 0 {
		buf.WriteString("\033[3m")
	}
	if s&StyleUnderline != 0 {
		buf.WriteString("\033[4m")
	}
	if s&StyleReverse != 0 {
		buf.WriteString("\033[7m")
	}
}

// AppendFgColor appends foreground color SGR escape sequences.
func AppendFgColor(buf *bytes.Buffer, c Color) {
	if c.IsRGB {
		buf.WriteString("\033[38;2;")
		buf.WriteString(strconv.Itoa(int(c.R)))
		buf.WriteByte(';')
		buf.WriteString(strconv.Itoa(int(c.G)))
		buf.WriteByte(';')
		buf.WriteString(strconv.Itoa(int(c.B)))
		buf.WriteByte('m')
	} else if c.IsANSI {
		if c.ANSI < 8 {
			buf.WriteString("\033[3")
			buf.WriteByte('0' + c.ANSI)
			buf.WriteByte('m')
		} else if c.ANSI < 16 {
			buf.WriteString("\033[9")
			buf.WriteByte('0' + (c.ANSI - 8))
			buf.WriteByte('m')
		} else {
			buf.WriteString("\033[38;5;")
			buf.WriteString(strconv.Itoa(int(c.ANSI)))
			buf.WriteByte('m')
		}
	}
}

// AppendBgColor appends background color SGR escape sequences.
func AppendBgColor(buf *bytes.Buffer, c Color) {
	if c.IsRGB {
		buf.WriteString("\033[48;2;")
		buf.WriteString(strconv.Itoa(int(c.R)))
		buf.WriteByte(';')
		buf.WriteString(strconv.Itoa(int(c.G)))
		buf.WriteByte(';')
		buf.WriteString(strconv.Itoa(int(c.B)))
		buf.WriteByte('m')
	} else if c.IsANSI {
		if c.ANSI < 8 {
			buf.WriteString("\033[4")
			buf.WriteByte('0' + c.ANSI)
			buf.WriteByte('m')
		} else if c.ANSI < 16 {
			buf.WriteString("\033[10")
			buf.WriteByte('0' + (c.ANSI - 8))
			buf.WriteByte('m')
		} else {
			buf.WriteString("\033[48;5;")
			buf.WriteString(strconv.Itoa(int(c.ANSI)))
			buf.WriteByte('m')
		}
	}
}

// FormatCellAttributes emits full styling escape codes for the cell to the buffer.
func FormatCellAttributes(buf *bytes.Buffer, c Cell) {
	buf.WriteString(EscSeqReset)
	AppendStyle(buf, c.Style)
	AppendFgColor(buf, c.Fg)
	AppendBgColor(buf, c.Bg)
}
