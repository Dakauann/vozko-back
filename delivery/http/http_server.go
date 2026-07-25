package http

type HTTPServer interface {
	Start(port string) error
	Shutdown() error
}
