package tui

import (
	"fmt"
)

// Color represents ANSI 16, 256, or 24-bit TrueColor.
type Color struct {
	R, G, B byte
	IsRGB   bool
	ANSI    uint8
	IsANSI  bool
}

// Predefined ANSI 16 colors.
var (
	ColorDefault       = Color{}
	ColorBlack         = Color{ANSI: 0, IsANSI: true}
	ColorRed           = Color{ANSI: 1, IsANSI: true}
	ColorGreen         = Color{ANSI: 2, IsANSI: true}
	ColorYellow        = Color{ANSI: 3, IsANSI: true}
	ColorBlue          = Color{ANSI: 4, IsANSI: true}
	ColorMagenta       = Color{ANSI: 5, IsANSI: true}
	ColorCyan          = Color{ANSI: 6, IsANSI: true}
	ColorWhite         = Color{ANSI: 7, IsANSI: true}
	ColorBrightBlack   = Color{ANSI: 8, IsANSI: true}
	ColorBrightRed     = Color{ANSI: 9, IsANSI: true}
	ColorBrightGreen   = Color{ANSI: 10, IsANSI: true}
	ColorBrightYellow  = Color{ANSI: 11, IsANSI: true}
	ColorBrightBlue    = Color{ANSI: 12, IsANSI: true}
	ColorBrightMagenta = Color{ANSI: 13, IsANSI: true}
	ColorBrightCyan    = Color{ANSI: 14, IsANSI: true}
	ColorBrightWhite   = Color{ANSI: 15, IsANSI: true}
)

// RGB creates a 24-bit TrueColor.
func RGB(r, g, b byte) Color {
	return Color{R: r, G: g, B: b, IsRGB: true}
}

// ANSI256 creates an 8-bit ANSI 256 color.
func ANSI256(code uint8) Color {
	return Color{ANSI: code, IsANSI: true}
}

// Style bitmask flags for text decoration.
type Style uint8

const (
	StyleNone      Style = 0
	StyleBold      Style = 1 << 0
	StyleDim       Style = 1 << 1
	StyleItalic    Style = 1 << 2
	StyleUnderline Style = 1 << 3
	StyleReverse   Style = 1 << 4
)

// Cell represents a single character cell in the terminal matrix.
type Cell struct {
	R     rune
	Fg    Color
	Bg    Color
	Style Style
}

// Equal returns true if two cells have identical rune, style, and colors.
func (c Cell) Equal(other Cell) bool {
	return c.R == other.R &&
		c.Style == other.Style &&
		c.Fg == other.Fg &&
		c.Bg == other.Bg
}

// ScreenBuffer represents an in-memory 2D matrix of terminal cells.
type ScreenBuffer struct {
	Width  int
	Height int
	Cells  []Cell
}

// NewScreenBuffer allocates a screen buffer of dimensions (w, h).
func NewScreenBuffer(w, h int) *ScreenBuffer {
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	sb := &ScreenBuffer{
		Width:  w,
		Height: h,
		Cells:  make([]Cell, w*h),
	}
	sb.Clear()
	return sb
}

// Clear resets all cells in the buffer to empty space with default attributes.
func (sb *ScreenBuffer) Clear() {
	empty := Cell{R: ' ', Fg: ColorDefault, Bg: ColorDefault, Style: StyleNone}
	for i := range sb.Cells {
		sb.Cells[i] = empty
	}
}

// Resize resizes the buffer, keeping overlapping content if desired or clearing.
func (sb *ScreenBuffer) Resize(w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	if sb.Width == w && sb.Height == h {
		return
	}
	newCells := make([]Cell, w*h)
	empty := Cell{R: ' ', Fg: ColorDefault, Bg: ColorDefault, Style: StyleNone}
	for i := range newCells {
		newCells[i] = empty
	}

	minW := min(sb.Width, w)
	minH := min(sb.Height, h)
	for y := 0; y < minH; y++ {
		for x := 0; x < minW; x++ {
			newCells[y*w+x] = sb.Cells[y*sb.Width+x]
		}
	}

	sb.Width = w
	sb.Height = h
	sb.Cells = newCells
}

// Get returns the cell at (x, y). Out-of-bounds coordinates return an empty cell.
func (sb *ScreenBuffer) Get(x, y int) Cell {
	if x < 0 || x >= sb.Width || y < 0 || y >= sb.Height {
		return Cell{R: ' '}
	}
	return sb.Cells[y*sb.Width+x]
}

// Set stores a cell at coordinate (x, y) if within bounds.
func (sb *ScreenBuffer) Set(x, y int, c Cell) {
	if x < 0 || x >= sb.Width || y < 0 || y >= sb.Height {
		return
	}
	if c.R == 0 {
		c.R = ' '
	}
	sb.Cells[y*sb.Width+x] = c
}

// SetRune stores a rune with styling attributes at (x, y).
func (sb *ScreenBuffer) SetRune(x, y int, r rune, fg, bg Color, s Style) {
	sb.Set(x, y, Cell{R: r, Fg: fg, Bg: bg, Style: s})
}

// SetString writes a horizontal string at (x, y) with specified styling.
func (sb *ScreenBuffer) SetString(x, y int, str string, fg, bg Color, s Style) {
	if y < 0 || y >= sb.Height {
		return
	}
	runes := []rune(str)
	for i, r := range runes {
		px := x + i
		if px < 0 {
			continue
		}
		if px >= sb.Width {
			break
		}
		sb.Set(px, y, Cell{R: r, Fg: fg, Bg: bg, Style: s})
	}
}

// FillRect fills a rectangular region with the specified rune and style.
func (sb *ScreenBuffer) FillRect(x, y, w, h int, r rune, fg, bg Color, s Style) {
	for cy := y; cy < y+h; cy++ {
		for cx := x; cx < x+w; cx++ {
			sb.Set(cx, cy, Cell{R: r, Fg: fg, Bg: bg, Style: s})
		}
	}
}

// KeyType identifies keyboard input codes.
type KeyType uint16

const (
	KeyUnknown KeyType = iota
	KeyRune
	KeyEnter
	KeyEsc
	KeyBackspace
	KeyTab
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyCtrlC
	KeyCtrlD
	KeyCtrlL
	KeyCtrlR
)

// Modifier flags for modifier keys.
type Modifier uint8

const (
	ModNone  Modifier = 0
	ModCtrl  Modifier = 1 << 0
	ModAlt   Modifier = 1 << 1
	ModShift Modifier = 2 << 1
)

// KeyEvent describes an incoming keyboard event.
type KeyEvent struct {
	Type KeyType
	Rune rune
	Mod  Modifier
}

// String returns a readable representation of the key event.
func (k KeyEvent) String() string {
	switch k.Type {
	case KeyRune:
		return fmt.Sprintf("KeyRune(%c)", k.Rune)
	case KeyEnter:
		return "KeyEnter"
	case KeyEsc:
		return "KeyEsc"
	case KeyBackspace:
		return "KeyBackspace"
	case KeyTab:
		return "KeyTab"
	case KeyUp:
		return "KeyUp"
	case KeyDown:
		return "KeyDown"
	case KeyLeft:
		return "KeyLeft"
	case KeyRight:
		return "KeyRight"
	case KeyCtrlC:
		return "KeyCtrlC"
	case KeyCtrlD:
		return "KeyCtrlD"
	case KeyCtrlL:
		return "KeyCtrlL"
	case KeyCtrlR:
		return "KeyCtrlR"
	default:
		return "KeyUnknown"
	}
}

// Dashboard defines the interface for the terminal observatory HUD.
type Dashboard interface {
	Render(buf *ScreenBuffer)
	HandleKey(event KeyEvent) bool
	Resize(width, height int)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
