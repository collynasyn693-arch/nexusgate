package tui

import (
	"bytes"
	"io"
)

// ScreenRenderer manages double-buffered terminal rendering with minimal differential ANSI output.
type ScreenRenderer struct {
	Width       int
	Height      int
	Front       *ScreenBuffer
	Back        *ScreenBuffer
	out         io.Writer
	diffBuf     bytes.Buffer
	forceRedraw bool
}

// NewScreenRenderer creates a double-buffered differential renderer.
func NewScreenRenderer(w, h int, out io.Writer) *ScreenRenderer {
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	return &ScreenRenderer{
		Width:       w,
		Height:      h,
		Front:       NewScreenBuffer(w, h),
		Back:        NewScreenBuffer(w, h),
		out:         out,
		forceRedraw: true,
	}
}

// Resize updates the buffer dimensions and marks the screen for a full differential refresh.
func (sr *ScreenRenderer) Resize(w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	if sr.Width == w && sr.Height == h {
		return
	}
	sr.Width = w
	sr.Height = h
	sr.Front.Resize(w, h)
	sr.Back.Resize(w, h)
	sr.forceRedraw = true
}

// Invalidate forces the next Flush() call to emit all cells regardless of diff.
func (sr *ScreenRenderer) Invalidate() {
	sr.forceRedraw = true
}

// Buffer returns the back buffer for rendering.
func (sr *ScreenRenderer) Buffer() *ScreenBuffer {
	return sr.Back
}

// Flush calculates the differential between the Back buffer and the Front buffer,
// writing only modified cells to the output writer. Returns total bytes written.
func (sr *ScreenRenderer) Flush() (int, error) {
	sr.diffBuf.Reset()

	if sr.forceRedraw {
		sr.diffBuf.WriteString(EscSeqClearScreen)
		sr.diffBuf.WriteString(EscSeqCursorHome)
	}

	curX := -1
	curY := -1
	var lastCell Cell
	lastCellValid := false

	w := sr.Width
	h := sr.Height

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			backCell := sr.Back.Get(x, y)
			frontCell := sr.Front.Get(x, y)

			if !sr.forceRedraw && backCell.Equal(frontCell) {
				continue
			}

			// Reposition cursor if not sequentially next cell
			if curX != x || curY != y {
				AppendMoveCursor(&sr.diffBuf, x, y)
				curX = x
				curY = y
			}

			// Apply color and style attributes if changed
			if !lastCellValid || backCell.Fg != lastCell.Fg || backCell.Bg != lastCell.Bg || backCell.Style != lastCell.Style {
				FormatCellAttributes(&sr.diffBuf, backCell)
				lastCell = backCell
				lastCellValid = true
			}

			// Emit character
			r := backCell.R
			if r == 0 {
				r = ' '
			}
			sr.diffBuf.WriteRune(r)

			curX++
		}
	}

	// Synchronize front buffer with back buffer
	copy(sr.Front.Cells, sr.Back.Cells)
	sr.forceRedraw = false

	if sr.diffBuf.Len() == 0 {
		return 0, nil
	}

	// Single write syscall
	n, err := sr.out.Write(sr.diffBuf.Bytes())
	return n, err
}
