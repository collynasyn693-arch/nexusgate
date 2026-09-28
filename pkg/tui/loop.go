package tui

import (
	"context"
	"io"
	"sync"
	"time"

	"nexusgate/pkg/telemetry"
)

// AppState holds the runtime metrics displayed by the TUI.
type AppState struct {
	mu           sync.RWMutex
	Version      string
	StartTime    time.Time
	ActiveConns  int64
	TotalReqs    uint64
	CurrentRPS   float64
	P50LatencyUs uint32
	P90LatencyUs uint32
	P99LatencyUs uint32
	Goroutines   int
	AllocBytes   uint64
	BatteryPct   int
	Charging     bool
	Draining     bool
	Muted        bool
	RPSHistory   []float64
	LatHistory   []float64
	Backends     []BackendRow
	Logs         []LogEntry
}

// App encapsulates the live Terminal Observatory application.
type App struct {
	Renderer    *ScreenRenderer
	Input       *InputReader
	IPCClient   *IPCClient
	Header      *HeaderWidget
	RPSSpark    *Sparkline
	LatSpark    *Sparkline
	Matrix      *MatrixWidget
	Logs        *LogViewer
	State       AppState
	TargetFPS   int
	IdleFPS     int
	stopChan    chan struct{}
	forceRedraw bool
}

// NewApp creates an initialized Terminal Observatory dashboard.
func NewApp(w, h int, out io.Writer, in io.Reader, client *IPCClient) *App {
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}

	app := &App{
		Renderer:  NewScreenRenderer(w, h, out),
		Input:     NewInputReader(in),
		IPCClient: client,
		Header:    NewHeaderWidget("v1.0.0"),
		RPSSpark:  NewSparkline(24),
		LatSpark:  NewSparkline(24),
		Matrix:    NewMatrixWidget(),
		Logs:      NewLogViewer(50),
		TargetFPS: 30, // 33ms active
		IdleFPS:   2,  // 500ms low-power idle
		stopChan:  make(chan struct{}),
	}

	app.RPSSpark.Fg = ColorBrightGreen
	app.LatSpark.Fg = ColorBrightYellow
	app.State.StartTime = time.Now()
	app.State.BatteryPct = -1
	app.State.Version = "v1.0.0"

	return app
}

// Stop signals the App to terminate.
func (a *App) Stop() {
	select {
	case <-a.stopChan:
	default:
		close(a.stopChan)
	}
}

// HandleKey processes interactive hotkeys. Returns false if 'q' (exit) is requested.
func (a *App) HandleKey(ev KeyEvent) bool {
	switch ev.Type {
	case KeyCtrlC, KeyCtrlD:
		return false
	case KeyRune:
		switch ev.Rune {
		case 'q', 'Q':
			return false
		case 'd', 'D':
			a.State.mu.Lock()
			a.State.Draining = !a.State.Draining
			a.Header.Draining = a.State.Draining
			a.State.mu.Unlock()
			a.forceRedraw = true
		case 'm', 'M':
			a.State.mu.Lock()
			a.State.Muted = !a.State.Muted
			a.Header.Muted = a.State.Muted
			a.State.mu.Unlock()
			a.forceRedraw = true
		}
	}
	return true
}

// ApplyTelemetryFrame updates the dashboard state from an IPC binary frame.
func (a *App) ApplyTelemetryFrame(f telemetry.BinaryFrame) {
	a.State.mu.Lock()
	defer a.State.mu.Unlock()

	a.State.TotalReqs = f.TotalRequests
	a.State.ActiveConns = int64(f.ActiveConns)
	a.State.CurrentRPS = float64(f.RPS1s) / 1000.0
	a.State.P50LatencyUs = f.P50LatencyUs
	a.State.P90LatencyUs = f.P90LatencyUs
	a.State.P99LatencyUs = f.P99LatencyUs

	// Update sparklines
	a.RPSSpark.Add(a.State.CurrentRPS)
	a.LatSpark.Add(float64(f.P50LatencyUs) / 1000.0)

	// Update header
	a.Header.ActiveConns = a.State.ActiveConns
	a.Header.TotalRequests = a.State.TotalReqs
	a.Header.CurrentRPS = a.State.CurrentRPS
	a.Header.Goroutines = a.State.Goroutines
	a.Header.AllocBytes = a.State.AllocBytes
}

// RenderComposite composes all sub-widgets into the Back screen buffer.
func (a *App) RenderComposite() {
	buf := a.Renderer.Buffer()
	buf.Clear()

	// 1. Header (Rows 0..1)
	headerLines := a.Header.Draw(buf, 0)
	curY := headerLines

	// 2. Micro Sparklines Bar (Row curY)
	if curY < buf.Height {
		buf.SetString(0, curY, " THROUGHPUT (RPS): ", ColorBrightWhite, ColorDefault, StyleBold)
		a.RPSSpark.Draw(buf, 20, curY)

		latLabelX := 48
		if buf.Width > 75 {
			buf.SetString(latLabelX, curY, "LATENCY P50: ", ColorBrightWhite, ColorDefault, StyleBold)
			a.LatSpark.Draw(buf, latLabelX+13, curY)
		}
		curY++
	}

	// 3. Upstream Status Matrix (Middle section)
	availableRows := (buf.Height - curY) / 2
	if availableRows < 3 {
		availableRows = 3
	}
	matrixLines := a.Matrix.Draw(buf, curY, availableRows)
	curY += matrixLines

	// 4. Live Access Logs (Remaining bottom rows)
	logLines := buf.Height - curY
	if logLines > 0 {
		a.Logs.Draw(buf, curY, logLines)
	}
}

// Run executes the main dashboard event and rendering loop.
func (a *App) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if a.Input != nil {
		a.Input.Start(ctx)
	}
	if a.IPCClient != nil {
		a.IPCClient.Start(ctx)
	}

	activeInterval := time.Duration(1000/a.TargetFPS) * time.Millisecond
	idleInterval := time.Duration(1000/a.IdleFPS) * time.Millisecond

	ticker := time.NewTicker(activeInterval)
	defer ticker.Stop()

	var lastActivity time.Time = time.Now()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-a.stopChan:
			return nil

		case ev, ok := <-a.Input.Events():
			if !ok {
				return nil
			}
			if !a.HandleKey(ev) {
				return nil
			}
			lastActivity = time.Now()

		case frame, ok := <-a.IPCClient.Frames():
			if ok {
				a.ApplyTelemetryFrame(frame)
				if frame.RPS1s > 0 {
					lastActivity = time.Now()
				}
			}

		case <-ticker.C:
			// Adaptive FPS: switch to idleInterval if inactive for >3s
			isIdle := time.Since(lastActivity) > 3*time.Second
			if isIdle {
				ticker.Reset(idleInterval)
			} else {
				ticker.Reset(activeInterval)
			}

			if a.forceRedraw {
				a.Renderer.Invalidate()
				a.forceRedraw = false
			}

			a.RenderComposite()
			_, _ = a.Renderer.Flush()
		}
	}
}
