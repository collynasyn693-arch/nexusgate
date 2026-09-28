package tui

// Sparkline glyph sets for Unicode (8 levels) and ASCII fallback.
var (
	UnicodeSparklineGlyphs = []rune{' ', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	ASCIISparklineGlyphs   = []rune{'_', '.', ':', '-', '=', '+', '*', '#'}
)

// Sparkline renders a single-row micro bar chart.
type Sparkline struct {
	Width    int
	Data     []float64
	Min      float64
	Max      float64
	Fg       Color
	Bg       Color
	Style    Style
	UseASCII bool
}

// NewSparkline creates a sparkline widget with the specified display width.
func NewSparkline(width int) *Sparkline {
	if width <= 0 {
		width = 20
	}
	return &Sparkline{
		Width: width,
		Data:  make([]float64, 0, width),
		Fg:    ColorGreen,
		Bg:    ColorDefault,
		Style: StyleNone,
	}
}

// Add appends a data point, maintaining the historical capacity up to Width.
func (s *Sparkline) Add(val float64) {
	if val < 0 {
		val = 0
	}
	if len(s.Data) >= s.Width {
		// Shift left
		copy(s.Data, s.Data[1:])
		s.Data[len(s.Data)-1] = val
	} else {
		s.Data = append(s.Data, val)
	}
}

// SetData sets the entire historical series, clamped to Width.
func (s *Sparkline) SetData(values []float64) {
	if len(values) > s.Width {
		s.Data = make([]float64, s.Width)
		copy(s.Data, values[len(values)-s.Width:])
	} else {
		s.Data = make([]float64, len(values), s.Width)
		copy(s.Data, values)
	}
}

// Glyphs returns the active rune set based on ASCII or Unicode mode.
func (s *Sparkline) Glyphs() []rune {
	if s.UseASCII {
		return ASCIISparklineGlyphs
	}
	return UnicodeSparklineGlyphs
}

// Draw renders the sparkline into the screen buffer at (x, y).
func (s *Sparkline) Draw(buf *ScreenBuffer, x, y int) {
	if y < 0 || y >= buf.Height {
		return
	}

	glyphs := s.Glyphs()
	numGlyphs := len(glyphs)

	// Determine min and max
	minVal := s.Min
	maxVal := s.Max
	if maxVal <= minVal {
		// Auto-scale from data
		maxVal = 0.0001
		for _, v := range s.Data {
			if v > maxVal {
				maxVal = v
			}
		}
	}

	valRange := maxVal - minVal
	if valRange <= 0 {
		valRange = 1.0
	}

	// Pad with spaces on left if Data < Width
	padding := s.Width - len(s.Data)
	for i := 0; i < padding; i++ {
		px := x + i
		if px >= 0 && px < buf.Width {
			buf.Set(px, y, Cell{R: ' ', Fg: s.Fg, Bg: s.Bg, Style: s.Style})
		}
	}

	for i, val := range s.Data {
		px := x + padding + i
		if px < 0 || px >= buf.Width {
			continue
		}

		if val <= minVal {
			buf.Set(px, y, Cell{R: glyphs[0], Fg: s.Fg, Bg: s.Bg, Style: s.Style})
			continue
		}

		norm := (val - minVal) / valRange
		if norm > 1.0 {
			norm = 1.0
		}
		glyphIdx := int(norm * float64(numGlyphs-1))
		if glyphIdx < 0 {
			glyphIdx = 0
		}
		if glyphIdx >= numGlyphs {
			glyphIdx = numGlyphs - 1
		}

		buf.Set(px, y, Cell{R: glyphs[glyphIdx], Fg: s.Fg, Bg: s.Bg, Style: s.Style})
	}
}

// String returns a string representation of the sparkline for simple terminal output.
func (s *Sparkline) String() string {
	glyphs := s.Glyphs()
	numGlyphs := len(glyphs)

	maxVal := s.Max
	if maxVal <= s.Min {
		maxVal = 0.0001
		for _, v := range s.Data {
			if v > maxVal {
				maxVal = v
			}
		}
	}
	valRange := maxVal - s.Min
	if valRange <= 0 {
		valRange = 1.0
	}

	res := make([]rune, 0, s.Width)
	padding := s.Width - len(s.Data)
	for i := 0; i < padding; i++ {
		res = append(res, ' ')
	}
	for _, val := range s.Data {
		norm := (val - s.Min) / valRange
		if norm < 0 {
			norm = 0
		}
		if norm > 1.0 {
			norm = 1.0
		}
		idx := int(norm * float64(numGlyphs-1))
		if idx >= numGlyphs {
			idx = numGlyphs - 1
		}
		res = append(res, glyphs[idx])
	}
	return string(res)
}
