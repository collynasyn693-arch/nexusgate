package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestNewUpstreamContext_Timeout(t *testing.T) {
	ctx, cancel := NewUpstreamContext(context.Background(), 20*time.Millisecond)
	defer cancel()

	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Errorf("expected DeadlineExceeded, got %v", ctx.Err())
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("context did not expire at deadline")
	}
}

func TestNewUpstreamContext_ClientCancel(t *testing.T) {
	parentCtx, parentCancel := context.WithCancel(context.Background())
	childCtx, childCancel := NewUpstreamContext(parentCtx, 5*time.Second)
	defer childCancel()

	// Abort client request
	parentCancel()

	select {
	case <-childCtx.Done():
		if !errors.Is(childCtx.Err(), context.Canceled) {
			t.Errorf("expected Canceled, got %v", childCtx.Err())
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("child context was not cancelled when parent was cancelled")
	}
}

func TestIsClientCanceled_and_IsGatewayTimeout(t *testing.T) {
	if !IsClientCanceled(context.Canceled) {
		t.Error("expected context.Canceled to be identified as client canceled")
	}
	if !IsClientCanceled(errors.New("write: broken pipe")) {
		t.Error("expected broken pipe to be identified as client canceled")
	}
	if IsClientCanceled(nil) {
		t.Error("nil must not be client canceled")
	}

	if !IsGatewayTimeout(context.DeadlineExceeded) {
		t.Error("expected context.DeadlineExceeded to be identified as gateway timeout")
	}
	if !IsGatewayTimeout(errors.New("dial tcp 10.0.0.1:80: i/o timeout")) {
		t.Error("expected i/o timeout to be identified as gateway timeout")
	}
	if IsGatewayTimeout(nil) {
		t.Error("nil must not be gateway timeout")
	}
}

func TestBuildUpstreamURL(t *testing.T) {
	target, _ := url.Parse("http://backend.internal:8080/api/v1")

	tests := []struct {
		reqPath  string
		reqQuery string
		expected string
	}{
		{"/users", "", "http://backend.internal:8080/api/v1/users"},
		{"/users/42", "details=true", "http://backend.internal:8080/api/v1/users/42?details=true"},
		{"/", "", "http://backend.internal:8080/api/v1"},
	}

	for _, tc := range tests {
		reqURL, _ := url.Parse("http://nexusgate.local" + tc.reqPath)
		if tc.reqQuery != "" {
			reqURL.RawQuery = tc.reqQuery
		}
		got := BuildUpstreamURL(target, reqURL)
		if got.String() != tc.expected {
			t.Errorf("BuildUpstreamURL() = %q; want %q", got.String(), tc.expected)
		}
	}
}

func TestUpstream_ClientDisconnectAbortsExecution(t *testing.T) {
	upstreamAbortedCh := make(chan struct{})

	// Upstream server that waits until request context is cancelled
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			close(upstreamAbortedCh)
		case <-time.After(3 * time.Second):
			t.Error("upstream handler timed out without receiving cancellation")
		}
	}))
	defer upstreamServer.Close()

	upstreamURL, err := url.Parse(upstreamServer.URL)
	if err != nil {
		t.Fatalf("failed to parse upstream URL: %v", err)
	}

	// Client context that is cancelled after 20ms
	clientCtx, cancel := context.WithCancel(context.Background())

	inReq, _ := http.NewRequestWithContext(clientCtx, "GET", "http://nexusgate.local/test", nil)
	outReq, err := NewUpstreamRequest(clientCtx, inReq, upstreamURL)
	if err != nil {
		t.Fatalf("NewUpstreamRequest failed: %v", err)
	}

	// Launch upstream roundtrip asynchronously
	errCh := make(chan error, 1)
	go func() {
		resp, roundTripErr := http.DefaultTransport.RoundTrip(outReq)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		errCh <- roundTripErr
	}()

	// Simulate client aborting after 20ms
	time.Sleep(20 * time.Millisecond)
	cancel()

	// Verify upstream was aborted immediately
	select {
	case <-upstreamAbortedCh:
		// Upstream detected context cancellation!
	case <-time.After(1 * time.Second):
		t.Fatal("upstream did not detect context cancellation upon client disconnect")
	}

	// Verify roundtrip returned context error
	roundTripErr := <-errCh
	if !IsClientCanceled(roundTripErr) {
		t.Errorf("expected client cancellation error from roundtrip, got %v", roundTripErr)
	}
}
