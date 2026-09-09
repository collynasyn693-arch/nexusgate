package config

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Watcher monitors configuration files on disk and captures POSIX SIGHUP signals
// to trigger atomic, non-destructive gateway hot-reloads.
type Watcher struct {
	configPath  string
	holder      *ConfigHolder
	interval    time.Duration
	debounce    time.Duration
	lastModTime time.Time
	lastSize    int64

	sigChan  chan os.Signal
	stopChan chan struct{}
	wg       sync.WaitGroup
	mu       sync.Mutex

	onError func(err error)
}

// WatcherOption configures optional Watcher parameters.
type WatcherOption func(*Watcher)

// WithPollInterval sets the disk polling frequency (default: 3 seconds).
func WithPollInterval(d time.Duration) WatcherOption {
	return func(w *Watcher) {
		w.interval = d
	}
}

// WithDebounceDuration sets the debounce settling window for editor multi-step writes (default: 150ms).
func WithDebounceDuration(d time.Duration) WatcherOption {
	return func(w *Watcher) {
		w.debounce = d
	}
}

// WithErrorHandler registers a callback for reload errors.
func WithErrorHandler(fn func(err error)) WatcherOption {
	return func(w *Watcher) {
		w.onError = fn
	}
}

// NewWatcher initializes a Watcher instance bound to a config path and ConfigHolder.
func NewWatcher(configPath string, holder *ConfigHolder, opts ...WatcherOption) *Watcher {
	w := &Watcher{
		configPath: configPath,
		holder:     holder,
		interval:   3 * time.Second,
		debounce:   150 * time.Millisecond,
		sigChan:    make(chan os.Signal, 2),
		stopChan:   make(chan struct{}),
	}

	for _, opt := range opts {
		opt(w)
	}

	// Record initial stat if file exists
	if fi, err := os.Stat(configPath); err == nil {
		w.lastModTime = fi.ModTime()
		w.lastSize = fi.Size()
	}

	return w
}

// Start initiates the background SIGHUP signal listener and file modification polling loops.
func (w *Watcher) Start() {
	signal.Notify(w.sigChan, syscall.SIGHUP)

	w.wg.Add(2)
	go w.signalLoop()
	go w.pollLoop()
}

// Stop terminates background watcher goroutines and stops signal listening.
func (w *Watcher) Stop() {
	w.mu.Lock()
	select {
	case <-w.stopChan:
		w.mu.Unlock()
		return
	default:
		close(w.stopChan)
	}
	w.mu.Unlock()

	signal.Stop(w.sigChan)
	w.wg.Wait()
}

// TriggerSIGHUP simulates or programmatically delivers a SIGHUP event to the watcher.
func (w *Watcher) TriggerSIGHUP() {
	select {
	case w.sigChan <- syscall.SIGHUP:
	default:
	}
}

// Reload reads, parses, validates, and atomically swaps the configuration from disk.
// If any stage of validation or parsing fails, the active configuration is 100% untouched.
func (w *Watcher) Reload() (*GatewayConfig, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	data, err := os.ReadFile(w.configPath)
	if err != nil {
		err = fmt.Errorf("failed to read config file during reload: %w", err)
		w.reportError(err)
		return nil, err
	}

	// If editor left a momentary 0-byte file, wait debounce and retry once
	if len(data) == 0 {
		time.Sleep(w.debounce)
		data, err = os.ReadFile(w.configPath)
		if err != nil || len(data) == 0 {
			err = fmt.Errorf("config file %q is empty during reload", w.configPath)
			w.reportError(err)
			return nil, err
		}
	}

	candidate, err := Parse(data)
	if err != nil {
		err = fmt.Errorf("config parsing failed during reload: %w", err)
		w.reportError(err)
		return nil, err
	}

	ApplyDefaults(candidate)
	ApplyEnvironmentOverrides(candidate)

	if err := Validate(candidate); err != nil {
		err = fmt.Errorf("candidate configuration rejected by validation: %w", err)
		w.reportError(err)
		return nil, err
	}

	// Safe atomic swap
	if _, err := w.holder.Swap(candidate); err != nil {
		w.reportError(err)
		return nil, err
	}

	// Update cached modtime
	if fi, err := os.Stat(w.configPath); err == nil {
		w.lastModTime = fi.ModTime()
		w.lastSize = fi.Size()
	}

	return candidate, nil
}

func (w *Watcher) signalLoop() {
	defer w.wg.Done()
	for {
		select {
		case <-w.stopChan:
			return
		case <-w.sigChan:
			_, _ = w.Reload()
		}
	}
}

func (w *Watcher) pollLoop() {
	defer w.wg.Done()
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopChan:
			return
		case <-ticker.C:
			fi, err := os.Stat(w.configPath)
			if err != nil {
				continue
			}

			w.mu.Lock()
			changed := fi.ModTime().After(w.lastModTime) || fi.Size() != w.lastSize
			w.mu.Unlock()

			if changed {
				// Select with debounce for clean cancellation
				select {
				case <-w.stopChan:
					return
				case <-time.After(w.debounce):
				}
				_, _ = w.Reload()
			}
		}
	}
}

func (w *Watcher) reportError(err error) {
	if w.onError != nil {
		w.onError(err)
	}
}
