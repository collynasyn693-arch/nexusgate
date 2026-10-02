package stress

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoadGenerator_BasicRun(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := RunLoad(ctx, LoadConfig{
		TargetURL:     ts.URL,
		Concurrency:   4,
		TotalRequests: 50,
	})
	if err != nil {
		t.Fatalf("load run failed: %v", err)
	}

	if res.SuccessCount < 50 {
		t.Errorf("expected at least 50 successes, got %d", res.SuccessCount)
	}
	if res.ErrorCount != 0 {
		t.Errorf("expected 0 errors, got %d", res.ErrorCount)
	}
	if res.StatusCodes[http.StatusOK] < 50 {
		t.Errorf("expected HTTP 200 count >= 50, got %d", res.StatusCodes[http.StatusOK])
	}
}
