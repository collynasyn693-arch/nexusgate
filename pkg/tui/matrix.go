package tui

import (
	"fmt"
	"strings"
)

// BackendRow represents one row in the upstream targets matrix table.
type BackendRow struct {
	RouteID   string
	TargetURL string
	Weight    int
	Status    string // "HEALTHY", "DEGRADED", "TRIPPED", "DRAINING"
	Inflight  int64
	LatencyUs int64
	RPS       float64
}

// MatrixWidget renders the table of upstream backends and circuit health.
type MatrixWidget struct {
	Rows          []BackendRow
	SelectedIndex int
}

// NewMatrixWidget creates an empty MatrixWidget.
func NewMatrixWidget() *MatrixWidget {
	return &MatrixWidget{
		Rows: make([]BackendRow, 0, 8),
	}
}

// SetRows updates the list of active backend rows.
func (m *MatrixWidget) SetRows(rows []BackendRow) {
	m.Rows = rows
	if m.SelectedIndex >= len(m.Rows) {
		m.SelectedIndex = len(m.Rows) - 1
	}
	if m.SelectedIndex < 0 && len(m.Rows) > 0 {
		m.SelectedIndex = 0
	}
}

// Draw renders the backend status table into the screen buffer.
// Returns the total number of lines rendered.
func (m *MatrixWidget) Draw(buf *ScreenBuffer, startY, maxRows int) int {
	if startY < 0 || startY >= buf.Height || maxRows <= 0 {
		return 0
	}

	w := buf.Width
	curY := startY

	// Section Title
	buf.SetString(0, curY, "─[ UPSTREAM BACKEND STATUS & TOPOLOGY ]", ColorBrightCyan, ColorDefault, StyleBold)
	for x := 39; x < w; x++ {
		buf.SetRune(x, curY, '─', ColorBrightBlack, ColorDefault, StyleNone)
	}
	curY++

	if curY >= buf.Height {
		return 1
	}

	// Table Headers: ROUTE (12) | TARGET (26) | WT (4) | STATE (10) | INFLIGHT (8) | LATENCY (9) | RPS (7)
	hdr := fmt.Sprintf("%-12s %-26s %-4s %-10s %-8s %-9s %-7s",
		"ROUTE", "TARGET", "WT", "STATUS", "INFLT", "LATENCY", "RPS")
	if len(hdr) > w {
		hdr = hdr[:w]
	}
	buf.SetString(0, curY, hdr, ColorBrightWhite, ColorBlue, StyleBold)
	curY++

	if len(m.Rows) == 0 {
		if curY < buf.Height {
			buf.SetString(2, curY, "No active upstream backends configured", ColorBrightBlack, ColorDefault, StyleItalic)
			curY++
		}
		return curY - startY
	}

	renderedRows := 0
	for i, r := range m.Rows {
		if curY >= buf.Height || renderedRows >= maxRows {
			break
		}

		style := StyleNone
		if i == m.SelectedIndex {
			style = StyleBold
		}

		// Route & Target formatting
		routeStr := r.RouteID
		if len(routeStr) > 12 {
			routeStr = routeStr[:11] + "…"
		}
		targetStr := r.TargetURL
		if len(targetStr) > 26 {
			targetStr = targetStr[:25] + "…"
		}

		// Status Color
		statusColor := ColorBrightGreen
		switch strings.ToUpper(r.Status) {
		case "TRIPPED", "OPEN", "DOWN":
			statusColor = ColorBrightRed
		case "HALFOPEN", "DEGRADED":
			statusColor = ColorBrightYellow
		case "DRAINING":
			statusColor = ColorBrightMagenta
		}

		// Latency formatting
		latStr := fmt.Sprintf("%.2fms", float64(r.LatencyUs)/1000.0)
		if r.LatencyUs == 0 {
			latStr = "<0.1ms"
		}

		// Line rendering with column positioning
		colX := 0
		buf.SetString(colX, curY, fmt.Sprintf("%-12s", routeStr), ColorBrightWhite, ColorDefault, style)
		colX += 13

		buf.SetString(colX, curY, fmt.Sprintf("%-26s", targetStr), ColorBrightCyan, ColorDefault, style)
		colX += 27

		buf.SetString(colX, curY, fmt.Sprintf("%-4d", r.Weight), ColorDefault, ColorDefault, style)
		colX += 5

		buf.SetString(colX, curY, fmt.Sprintf("%-10s", r.Status), statusColor, ColorDefault, style)
		colX += 11

		buf.SetString(colX, curY, fmt.Sprintf("%-8d", r.Inflight), ColorBrightYellow, ColorDefault, style)
		colX += 9

		buf.SetString(colX, curY, fmt.Sprintf("%-9s", latStr), ColorBrightWhite, ColorDefault, style)
		colX += 10

		buf.SetString(colX, curY, fmt.Sprintf("%-7.1f", r.RPS), ColorGreen, ColorDefault, style)

		curY++
		renderedRows++
	}

	return curY - startY
}
