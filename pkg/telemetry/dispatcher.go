package telemetry

import (
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultClientChannelBuffer = 16
	DefaultClientWriteTimeout  = 250 * time.Millisecond
)

// clientSubscriber represents an active UDS client attached to the telemetry stream.
type clientSubscriber struct {
	conn         net.Conn
	ch           chan [BinaryFrameSize]byte
	dropped      atomic.Uint64
	done         chan struct{}
	closeOnce    sync.Once
}

// Dispatcher manages fan-out broadcast of telemetry frames to multiple connected UDS subscribers.
// It guarantees zero backpressure on the gateway core via non-blocking channel sends
// and strict per-write socket deadlines.
type Dispatcher struct {
	mu           sync.RWMutex
	subscribers  map[net.Conn]*clientSubscriber
	channelSize  int
	writeTimeout time.Duration
	totalDropped atomic.Uint64
	closed       atomic.Bool
	done         chan struct{}
}

// NewDispatcher creates an initialized telemetry broadcast Dispatcher.
func NewDispatcher(channelSize int, writeTimeout time.Duration) *Dispatcher {
	if channelSize <= 0 {
		channelSize = DefaultClientChannelBuffer
	}
	if writeTimeout <= 0 {
		writeTimeout = DefaultClientWriteTimeout
	}
	return &Dispatcher{
		subscribers:  make(map[net.Conn]*clientSubscriber),
		channelSize:  channelSize,
		writeTimeout: writeTimeout,
		done:         make(chan struct{}),
	}
}

// Subscribe registers a new client connection to receive broadcast frames.
func (d *Dispatcher) Subscribe(conn net.Conn) {
	if conn == nil || d.closed.Load() {
		return
	}

	sub := &clientSubscriber{
		conn: conn,
		ch:   make(chan [BinaryFrameSize]byte, d.channelSize),
		done: make(chan struct{}),
	}

	d.mu.Lock()
	if d.closed.Load() {
		d.mu.Unlock()
		_ = conn.Close()
		return
	}
	d.subscribers[conn] = sub
	d.mu.Unlock()

	// Launch dedicated per-client write pump with strict write deadline
	go d.clientPump(sub)
}

// Unsubscribe cleanly removes a client subscriber and shuts down its pump.
func (d *Dispatcher) Unsubscribe(conn net.Conn) {
	d.mu.Lock()
	sub, exists := d.subscribers[conn]
	if exists {
		delete(d.subscribers, conn)
	}
	d.mu.Unlock()

	if exists {
		sub.closeOnce.Do(func() {
			close(sub.done)
			_ = sub.conn.Close()
		})
	}
}

// Broadcast encodes and non-blockingly distributes a BinaryFrame to all active subscribers.
// If a subscriber channel is full, the frame is dropped for that subscriber without blocking.
func (d *Dispatcher) Broadcast(frame BinaryFrame) {
	if d.closed.Load() {
		return
	}

	var buf [BinaryFrameSize]byte
	if err := frame.Encode(buf[:]); err != nil {
		return
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, sub := range d.subscribers {
		select {
		case sub.ch <- buf:
		default:
			// Non-blocking drop: slow subscriber must never block gateway telemetry
			sub.dropped.Add(1)
			d.totalDropped.Add(1)
		}
	}
}

func (d *Dispatcher) clientPump(sub *clientSubscriber) {
	for {
		select {
		case <-d.done:
			return
		case <-sub.done:
			return
		case buf := <-sub.ch:
			// Enforce strict write deadline to isolate stalled subscribers
			_ = sub.conn.SetWriteDeadline(time.Now().Add(d.writeTimeout))
			_, err := sub.conn.Write(buf[:])
			if err != nil {
				// Socket error or write timeout: evict subscriber
				d.Unsubscribe(sub.conn)
				return
			}
		}
	}
}

// SubscriberCount returns the current count of active subscribers.
func (d *Dispatcher) SubscriberCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.subscribers)
}

// TotalDropped returns the cumulative count of dropped frames across all subscribers.
func (d *Dispatcher) TotalDropped() uint64 {
	return d.totalDropped.Load()
}

// SubscriberDropped returns the dropped frame count for a specific subscriber.
func (d *Dispatcher) SubscriberDropped(conn net.Conn) uint64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if sub, exists := d.subscribers[conn]; exists {
		return sub.dropped.Load()
	}
	return 0
}

// Close gracefully shuts down the dispatcher and evicts all subscribers.
func (d *Dispatcher) Close() {
	if d.closed.Swap(true) {
		return
	}
	close(d.done)

	d.mu.Lock()
	subs := make([]*clientSubscriber, 0, len(d.subscribers))
	for _, sub := range d.subscribers {
		subs = append(subs, sub)
	}
	d.subscribers = make(map[net.Conn]*clientSubscriber)
	d.mu.Unlock()

	for _, sub := range subs {
		sub.closeOnce.Do(func() {
			close(sub.done)
			_ = sub.conn.Close()
		})
	}
}
