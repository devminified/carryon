package loadtest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFlakyTransportZeroRateNeverDrops(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: &FlakyTransport{Rate: 0}}
	for i := 0; i < 20; i++ {
		resp, err := client.Get(srv.URL)
		if err != nil {
			t.Fatalf("request %d: unexpected error at Rate=0: %v", i, err)
		}
		_ = resp.Body.Close()
	}
}

func TestFlakyTransportFullRateAlwaysDrops(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: &FlakyTransport{Rate: 1}}
	for i := 0; i < 10; i++ {
		_, err := client.Get(srv.URL)
		if err == nil {
			t.Fatalf("request %d: expected a simulated drop at Rate=1, got no error", i)
		}
	}
}

func TestFlakyTransportPartialRateDropsSome(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: &FlakyTransport{Rate: 0.5}}
	var drops, oks int
	for i := 0; i < 200; i++ {
		resp, err := client.Get(srv.URL)
		if err != nil {
			drops++
			continue
		}
		oks++
		_ = resp.Body.Close()
	}
	if drops == 0 || oks == 0 {
		t.Fatalf("expected a mix of drops and successes at Rate=0.5 over 200 requests, got drops=%d oks=%d", drops, oks)
	}
}
