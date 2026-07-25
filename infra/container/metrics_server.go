package container

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"
)

type metricsServer struct {
	addr   string
	server *http.Server
}

func newMetricsServer(addr string, handler http.Handler) *metricsServer {
	if addr == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", handler)

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return &metricsServer{
		addr: addr,
		server: &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
}

func (m *metricsServer) Start() {
	if m == nil {
		return
	}
	go func() {
		log.Printf("metrics listener: serving on %s", m.addr)
		if err := m.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("metrics listener: serve error: %v", err)
		}
	}()
}

func (m *metricsServer) Shutdown() {
	if m == nil || m.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.server.Shutdown(ctx); err != nil {
		log.Printf("metrics listener: shutdown error: %v", err)
	}
}
