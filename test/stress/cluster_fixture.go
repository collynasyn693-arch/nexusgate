package stress

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"
)

// ClusterNodeConfig defines behavior settings for an individual cluster mock backend.
type ClusterNodeConfig struct {
	ID          string
	BaseDelay   time.Duration
	Jitter      time.Duration
	FailureRate float64
	BodyPayload []byte
}

// ClusterNode represents a live mock HTTP server node.
type ClusterNode struct {
	Config       ClusterNodeConfig
	Server       *httptest.Server
	RequestsSeen atomic.Int64
	ErrorsServed atomic.Int64
	BytesSent    atomic.Int64
}

// MockCluster manages a fleet of mock backend nodes for stress and load tests.
type MockCluster struct {
	Nodes []*ClusterNode
	rng   *rand.Rand
}

// NewMockCluster initializes and spins up a cluster of HTTP mock servers.
func NewMockCluster(nodeConfigs []ClusterNodeConfig) *MockCluster {
	cluster := &MockCluster{
		Nodes: make([]*ClusterNode, len(nodeConfigs)),
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	for i, cfg := range nodeConfigs {
		node := &ClusterNode{Config: cfg}
		if len(node.Config.BodyPayload) == 0 {
			node.Config.BodyPayload = []byte(fmt.Sprintf("mock response from %s", cfg.ID))
		}

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			node.RequestsSeen.Add(1)

			// 1. Simulate failure rate
			if node.Config.FailureRate > 0 && rand.Float64() < node.Config.FailureRate {
				node.ErrorsServed.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("mock upstream internal error"))
				return
			}

			// 2. Simulate artificial variable latency
			delay := node.Config.BaseDelay
			if node.Config.Jitter > 0 {
				delay += time.Duration(rand.Int63n(int64(node.Config.Jitter)))
			}
			if delay > 0 {
				time.Sleep(delay)
			}

			// 3. Serve standard payload
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			n, _ := w.Write(node.Config.BodyPayload)
			node.BytesSent.Add(int64(n))
		})

		node.Server = httptest.NewServer(handler)
		cluster.Nodes[i] = node
	}

	return cluster
}

// URLs returns a slice of server URLs for all cluster nodes.
func (mc *MockCluster) URLs() []string {
	urls := make([]string, len(mc.Nodes))
	for i, n := range mc.Nodes {
		urls[i] = n.Server.URL
	}
	return urls
}

// TotalRequests returns aggregate requests handled across all nodes.
func (mc *MockCluster) TotalRequests() int64 {
	var total int64
	for _, n := range mc.Nodes {
		total += n.RequestsSeen.Load()
	}
	return total
}

// TotalErrors returns aggregate errors injected across all nodes.
func (mc *MockCluster) TotalErrors() int64 {
	var total int64
	for _, n := range mc.Nodes {
		total += n.ErrorsServed.Load()
	}
	return total
}

// Close gracefully terminates all cluster mock servers.
func (mc *MockCluster) Close() {
	for _, n := range mc.Nodes {
		if n.Server != nil {
			n.Server.Close()
		}
	}
}
