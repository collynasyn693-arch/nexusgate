package tui

import (
	"testing"
)

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func BenchmarkScreenRenderer_ZeroDiff(b *testing.B) {
	renderer := NewScreenRenderer(80, 24, discardWriter{})
	renderer.Buffer().SetString(0, 0, "NexusGate Terminal Observatory", ColorBrightGreen, ColorDefault, StyleBold)
	_, _ = renderer.Flush() // First flush syncs front and back

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = renderer.Flush()
	}
}

func BenchmarkScreenRenderer_SingleLineDiff(b *testing.B) {
	renderer := NewScreenRenderer(80, 24, discardWriter{})
	renderer.Buffer().SetString(0, 0, "NexusGate Terminal Observatory", ColorBrightGreen, ColorDefault, StyleBold)
	_, _ = renderer.Flush()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		renderer.Buffer().SetString(0, 10, "GET /api/v1/resource [200] 1.25ms 127.0.0.1", ColorBrightCyan, ColorDefault, StyleNone)
		_, _ = renderer.Flush()
	}
}

func BenchmarkScreenRenderer_FullRedraw(b *testing.B) {
	renderer := NewScreenRenderer(80, 24, discardWriter{})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		renderer.Invalidate()
		renderer.Buffer().SetString(0, 0, "Full Redraw Frame Benchmark", ColorBrightWhite, ColorDefault, StyleBold)
		_, _ = renderer.Flush()
	}
}

func BenchmarkSparkline_Add(b *testing.B) {
	spark := NewSparkline(40)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		spark.Add(float64(i % 100))
	}
}
