package http

import "net/http"

type Router interface {
	GetHandler() http.Handler
}
