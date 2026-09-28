package tui

import (
	"testing"
)

func TestSparkline_MinMaxGlyphs(t *testing.T) {
	spark := NewSparkline(4)
	spark.Min = 0
	spark.Max = 100
	spark.SetData([]float64{0, 50, 100, 100})

	runes := []rune(spark.String())
	if len(runes) != 4 {
		t.Fatalf("expected 4 runes, got %d", len(runes))
	}

	if runes[0] != UnicodeSparklineGlyphs[0] {
		t.Fatalf("expected min glyph %c, got %c", UnicodeSparklineGlyphs[0], runes[0])
	}
	if runes[3] != UnicodeSparklineGlyphs[len(UnicodeSparklineGlyphs)-1] {
		t.Fatalf("expected max glyph %c, got %c", UnicodeSparklineGlyphs[len(UnicodeSparklineGlyphs)-1], runes[3])
	}
}

func TestSparkline_ASCIIMode(t *testing.T) {
	spark := NewSparkline(5)
	spark.UseASCII = true
	spark.Min = 0
	spark.Max = 10
	spark.SetData([]float64{0, 2.5, 5, 7.5, 10})

	s := spark.String()
	if len([]rune(s)) != 5 {
		t.Fatalf("expected 5 runes, got %d", len([]rune(s)))
	}
	if s[0] != byte(ASCIISparklineGlyphs[0]) {
		t.Fatalf("expected ascii min glyph, got %c", s[0])
	}
	if s[4] != byte(ASCIISparklineGlyphs[len(ASCIISparklineGlyphs)-1]) {
		t.Fatalf("expected ascii max glyph, got %c", s[4])
	}
}

func TestSparkline_RollingWindowAdd(t *testing.T) {
	spark := NewSparkline(3)
	spark.Add(10)
	spark.Add(20)
	spark.Add(30)
	if len(spark.Data) != 3 || spark.Data[0] != 10 || spark.Data[2] != 30 {
		t.Fatalf("unexpected data after 3 additions: %v", spark.Data)
	}

	// 4th addition shifts left
	spark.Add(40)
	if len(spark.Data) != 3 {
		t.Fatalf("expected capacity 3, got %d", len(spark.Data))
	}
	if spark.Data[0] != 20 || spark.Data[1] != 30 || spark.Data[2] != 40 {
		t.Fatalf("expected rolling window [20, 30, 40], got %v", spark.Data)
	}
}

func TestSparkline_DrawToBuffer(t *testing.T) {
	buf := NewScreenBuffer(10, 3)
	spark := NewSparkline(5)
	spark.Fg = ColorYellow
	spark.SetData([]float64{1, 2, 3, 4, 5})

	spark.Draw(buf, 2, 1)

	// Verify buffer cells
	for i := 0; i < 5; i++ {
		cell := buf.Get(2+i, 1)
		if cell.R == ' ' && i > 0 {
			t.Fatalf("expected non-empty sparkline glyph at index %d", 2+i)
		}
		if cell.Fg != ColorYellow {
			t.Fatalf("expected ColorYellow fg, got %v", cell.Fg)
		}
	}
}
