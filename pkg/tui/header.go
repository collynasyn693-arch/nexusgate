package tui

import (
	"fmt"
	"time"
)

// HeaderWidget renders the top status banner and system resource metrics.
type HeaderWidget struct {
	Version         string
	StartTime       time.Time
	ActiveConns     int64
	TotalRequests   uint64
	CurrentRPS      float64
	Goroutines      int
	AllocBytes      uint64
	BatteryPct      int  // -1 if unknown
	BatteryCharging bool
	Draining        bool
	Muted           bool
}

// NewHeaderWidget creates an initialized HeaderWidget.
func NewHeaderWidget(version string) *HeaderWidget {
	return &HeaderWidget{
		Version:    version,
		StartTime:  time.Now(),
		BatteryPct: -1,
	}
}

// Draw renders the header banner into the screen buffer starting at row y.
// Returns the number of rows used (typically 2).
func (h *HeaderWidget) Draw(buf *ScreenBuffer, y int) int {
	if y < 0 || y+1 >= buf.Height {
		return 0
	}

	w := buf.Width

	// Line 1: Title and Core Status Badges
	title := fmt.Sprintf(" NEXUSGATE %s [ARM64] ", h.Version)
	buf.SetString(0, y, title, ColorBrightWhite, ColorBlue, StyleBold)

	// Status badge
	badgeX := len(title) + 1
	if h.Draining {
		buf.SetString(badgeX, y, "[DRAINING]", ColorBlack, ColorBrightYellow, StyleBold)
		badgeX += 11
	} else {
		buf.SetString(badgeX, y, "[ONLINE]", ColorBlack, ColorBrightGreen, StyleBold)
		badgeX += 9
	}

	if h.Muted {
		buf.SetString(badgeX, y, "[MUTED]", ColorBrightBlack, ColorDefault, StyleDim)
		badgeX += 8
	}

	// Uptime calculation
	uptime := time.Since(h.StartTime).Truncate(time.Second)
	uptimeStr := fmt.Sprintf("UP: %s", uptime)
	uptimeX := w - len(uptimeStr) - 1
	if uptimeX > badgeX {
		buf.SetString(uptimeX, y, uptimeStr, ColorBrightCyan, ColorDefault, StyleNone)
	}

	// Line 2: Resource and Traffic Statistics
	line2Y := y + 1
	// Conns & RPS
	stats1 := fmt.Sprintf(" Conns: %d | RPS: %.1f | Req: %d ", h.ActiveConns, h.CurrentRPS, h.TotalRequests)
	buf.SetString(0, line2Y, stats1, ColorBrightYellow, ColorDefault, StyleBold)

	// Goroutines and Memory
	memMB := float64(h.AllocBytes) / (1024 * 1024)
	stats2 := fmt.Sprintf("Go: %d | RSS: %.1fMB", h.Goroutines, memMB)
	s2X := len(stats1) + 2
	if s2X < w-25 {
		buf.SetString(s2X, line2Y, stats2, ColorBrightMagenta, ColorDefault, StyleNone)
	}

	// Battery Indicator (Termux power efficiency)
	var battStr string
	var battColor Color
	if h.BatteryPct >= 0 {
		chg := ""
		if h.BatteryCharging {
			chg = "⚡"
		}
		battStr = fmt.Sprintf("BAT: %d%%%s", h.BatteryPct, chg)
		if h.BatteryPct > 50 {
			battColor = ColorBrightGreen
		} else if h.BatteryPct > 20 {
			battColor = ColorBrightYellow
		} else {
			battColor = ColorBrightRed
		}
	} else {
		battStr = "PWR: AC"
		battColor = ColorBrightGreen
	}

	battX := w - len(battStr) - 1
	if battX > 0 {
		buf.SetString(battX, line2Y, battStr, battColor, ColorDefault, StyleBold)
	}

	return 2
}
