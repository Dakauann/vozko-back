package http_server_impl

import (
	"context"
	"log"
	"net/http"
	"time"

	http_protocol "vozko/delivery/http"
)

type HTTPServerImpl struct {
	handler    http.Handler
	httpServer *http.Server
}

func NewHTTPServer(handler http.Handler) http_protocol.HTTPServer {
	return &HTTPServerImpl{
		handler:    handler,
		httpServer: nil,
	}
}

func (s *HTTPServerImpl) Start(port string) error {
	s.httpServer = &http.Server{
		Addr:    ":" + port,
		Handler: s.handler,
	}

	log.Printf("LISTENING ON PORT: %s", port)

	err := s.httpServer.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

func (s *HTTPServerImpl) Shutdown() error {
	if s.httpServer == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}
