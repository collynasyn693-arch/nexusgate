package stress

import (
	"io"
	"net/http"
	"testing"
	"time"
)

func TestMockCluster_Lifecycle(t *testing.T) {
	cluster := NewMockCluster([]ClusterNodeConfig{
		{ID: "node-1", BaseDelay: 5 * time.Millisecond},
		{ID: "node-2", BaseDelay: 10 * time.Millisecond},
	})
	defer cluster.Close()

	urls := cluster.URLs()
	if len(urls) != 2 {
		t.Fatalf("expected 2 urls, got %d", len(urls))
	}

	resp, err := http.Get(urls[0])
	if err != nil {
		t.Fatalf("failed to query node-1: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "mock response from node-1" {
		t.Errorf("unexpected body: %q", string(body))
	}

	if cluster.TotalRequests() != 1 {
		t.Errorf("expected 1 total request, got %d", cluster.TotalRequests())
	}
}
