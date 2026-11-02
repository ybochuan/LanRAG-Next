// Package bridge serves the local Extension Host HTTP boundary.
package bridge

import (
	"context"
	"log"
	"net/http"
	"time"
)

// Address restricts the Extension Host listener to the local IPv4 loopback.
const Address = "127.0.0.1:8090"

// Services explicitly enables individual typed hooks and optional local
// readiness; zero values reject business requests and readiness checks.
type Services struct {
	Ready             func(context.Context) bool // Local DB/services only; never depends on Core availability.
	BeforeRetrieve    *RetrieveHandler
	Parser            http.Handler  // Optional identity-bearing V1 parser; disabled unless configured.
	ParserReadTimeout time.Duration // Zero uses 1000s; applies only to the parser body.
}

// NewServer constructs the protocol foundation without business services.
func NewServer() *http.Server {
	return NewServerWithServices(Services{})
}

// NewServerWithServices configures typed hooks without implying full readiness.
func NewServerWithServices(services Services) *http.Server {
	mux := http.NewServeMux()
	if services.Parser != nil {
		mux.Handle("/parser/native/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			budget := services.ParserReadTimeout
			if budget <= 0 {
				budget = 1000 * time.Second
			}
			controller := http.NewResponseController(w)
			// ReadHeaderTimeout and all Bridge routes keep their original 5s bound.
			_ = controller.SetReadDeadline(time.Now().Add(budget))
			defer controller.SetReadDeadline(time.Time{})
			services.Parser.ServeHTTP(w, r)
		}))
	}
	health := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	}
	mux.HandleFunc("GET /bridge/healthz", health)
	mux.HandleFunc("GET /healthz", health)
	ready := func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 200*time.Millisecond)
		defer cancel()
		if services.Ready == nil || !services.Ready(ctx) {
			writeError(w, http.StatusServiceUnavailable, "not_ready")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"status\":\"ready\",\"scope\":\"local\"}\n"))
	}
	mux.HandleFunc("GET /readyz", ready)
	mux.HandleFunc("GET /health", ready)
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
