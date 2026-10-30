// Package bridge serves the local Extension Host HTTP boundary.
package bridge

import (
	"log"
	"net/http"
	"time"
)

// Address restricts the Extension Host listener to the local IPv4 loopback.
const Address = "127.0.0.1:8090"

// Services explicitly enables individual typed hooks; zero values reject.
// Readiness remains unavailable until the full business boundary is connected.
type Services struct {
	BeforeRetrieve *RetrieveHandler
}

// NewServer constructs the protocol foundation without business services.
func NewServer() *http.Server {
	return NewServerWithServices(Services{})
}

// NewServerWithServices configures typed hooks without implying full readiness.
func NewServerWithServices(services Services) *http.Server {
	mux := http.NewServeMux()
	health := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	}
	mux.HandleFunc("GET /bridge/healthz", health)
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusServiceUnavailable, "not_ready")
	})
	for _, path := range []string{
		"/bridge/before-retrieve",
		"/bridge/after-retrieve",
		"/bridge/before-generate",
		"/bridge/ingest-event",
	} {
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// No policy permit or ingestion ACK without the corresponding service.
			writeError(w, http.StatusServiceUnavailable, "bridge_unavailable")
		})
		if path == "/bridge/before-retrieve" && services.BeforeRetrieve != nil {
			handler = services.BeforeRetrieve.ServeHTTP
		}
		mux.HandleFunc("POST "+path, requireVersion(func(w http.ResponseWriter, r *http.Request) {
			if path == "/bridge/before-retrieve" {
				log.Print("bridge_before_retrieve_received")
			}
			handler(w, r)
		}))
	}

	return &http.Server{
		Addr:              Address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
	}
}
