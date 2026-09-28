package tui

import (
	"fmt"
	"time"
)

// LogEntry describes an individual access log event in the viewport.
type LogEntry struct {
	Timestamp  time.Time
	Method     string
	Path       string
	StatusCode int
	LatencyUs  int64
	ClientIP   string
}

// LogViewer maintains a rolling window of recent access logs.
type LogViewer struct {
	Capacity   int
	Entries    []LogEntry
	ScrollDown bool
	ScrollOff  int
}

// NewLogViewer creates a new LogViewer with the specified history buffer capacity.
func NewLogViewer(capacity int) *LogViewer {
	if capacity <= 0 {
		capacity = 100
	}
	return &LogViewer{
		Capacity:   capacity,
		Entries:    make([]LogEntry, 0, capacity),
		ScrollDown: true,
	}
}

// Add appends a new log entry to the rolling buffer.
func (l *LogViewer) Add(entry LogEntry) {
	if len(l.Entries) >= l.Capacity {
		copy(l.Entries, l.Entries[1:])
		l.Entries[len(l.Entries)-1] = entry
	} else {
		l.Entries = append(l.Entries, entry)
	}
}

// Draw renders the rolling access log section into the screen buffer.
// Returns the number of lines drawn.
func (l *LogViewer) Draw(buf *ScreenBuffer, startY, height int) int {
	if startY < 0 || startY >= buf.Height || height <= 0 {
		return 0
	}

	w := buf.Width
	curY := startY

	// Header line
	buf.SetString(0, curY, "─[ LIVE ACCESS & EVENT STREAM ]", ColorBrightCyan, ColorDefault, StyleBold)
	for x := 31; x < w; x++ {
		buf.SetRune(x, curY, '─', ColorBrightBlack, ColorDefault, StyleNone)
	}
	curY++

	contentLines := height - 1
	if contentLines <= 0 || curY >= buf.Height {
		return 1
	}

	if len(l.Entries) == 0 {
		buf.SetString(2, curY, "Waiting for incoming gateway traffic...", ColorBrightBlack, ColorDefault, StyleItalic)
		return 2
	}

	// Calculate slice of logs to display
	total := len(l.Entries)
	startIdx := total - contentLines
	if startIdx < 0 {
		startIdx = 0
	}

	for i := startIdx; i < total; i++ {
		if curY >= buf.Height {
			break
		}
		entry := l.Entries[i]

		// Time prefix (HH:MM:SS)
		timeStr := entry.Timestamp.Format("15:04:05")
		colX := 0
		buf.SetString(colX, curY, timeStr, ColorBrightBlack, ColorDefault, StyleNone)
		colX += len(timeStr) + 1

		// Method badge
		methodColor := ColorBrightWhite
		switch entry.Method {
		case "GET":
			methodColor = ColorBrightGreen
		case "POST":
			methodColor = ColorBrightCyan
		case "PUT", "PATCH":
			methodColor = ColorBrightYellow
		case "DELETE":
			methodColor = ColorBrightRed
		}
		methodBadge := fmt.Sprintf("%-6s", entry.Method)
		buf.SetString(colX, curY, methodBadge, methodColor, ColorDefault, StyleBold)
		colX += len(methodBadge) + 1

		// Status Code Badge
		statusColor := ColorBrightGreen
		if entry.StatusCode >= 500 {
			statusColor = ColorBrightRed
		} else if entry.StatusCode >= 400 {
			statusColor = ColorBrightYellow
		} else if entry.StatusCode >= 300 {
			statusColor = ColorBrightCyan
		}
		statusBadge := fmt.Sprintf("[%d]", entry.StatusCode)
		buf.SetString(colX, curY, statusBadge, ColorBlack, statusColor, StyleBold)
		colX += len(statusBadge) + 1

		// Latency
		latStr := fmt.Sprintf("%.2fms", float64(entry.LatencyUs)/1000.0)
		if entry.LatencyUs == 0 {
			latStr = "<0.1ms"
		}
		buf.SetString(colX, curY, fmt.Sprintf("%-8s", latStr), ColorBrightWhite, ColorDefault, StyleNone)
		colX += 9

		// Client IP
		if entry.ClientIP != "" {
			ipStr := fmt.Sprintf("%-15s", entry.ClientIP)
			buf.SetString(colX, curY, ipStr, ColorBrightBlack, ColorDefault, StyleNone)
			colX += 16
		}

		// Path
		pathRemaining := w - colX - 1
		pathStr := entry.Path
		if pathRemaining > 0 {
			if len(pathStr) > pathRemaining {
				pathStr = pathStr[:pathRemaining-1] + "…"
			}
			buf.SetString(colX, curY, pathStr, ColorBrightWhite, ColorDefault, StyleNone)
		}

		curY++
	}

	return curY - startY
}
