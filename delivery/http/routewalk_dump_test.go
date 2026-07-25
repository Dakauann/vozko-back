package http

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

func TestRouteWalkDump(t *testing.T) {
	out := os.Getenv("ROUTE_DUMP_PATH")
	if out == "" {
		t.Skip("ROUTE_DUMP_PATH not set")
	}

	r := &router{mux: mux.NewRouter()}
	r.setupRoutes()

	var lines []string
	err := r.mux.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		tmpl, err := route.GetPathTemplate()
		if err != nil {
			return nil
		}
		methods, err := route.GetMethods()
		if err != nil || len(methods) == 0 {
			methods = []string{"ANY"}
		}
		sort.Strings(methods)
		for _, m := range methods {
			lines = append(lines, m+" "+tmpl)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	sort.Strings(lines)
	if err := os.WriteFile(out, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Logf("wrote %d route lines to %s", len(lines), out)
}
