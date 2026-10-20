package httptrace

import (
	"net/http"
	"strings"
)

func Headers(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, values := range h {
		out[k] = strings.Join(values, ", ")
	}
	return out
}

func Redirects(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	hops := 0
	for req := resp.Request; req != nil && req.Response != nil; req = req.Response.Request {
		hops++
	}
	return hops
}
