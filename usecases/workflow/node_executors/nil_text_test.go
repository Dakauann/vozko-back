package node_executors

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/workflow"
)

func TestANullTemplateParameterIsSentEmptyNeverAsNilText(t *testing.T) {
	got := templateParamValues(&workflow.NodeContext{Node: &workflow.Node{Config: map[string]interface{}{"body_params": map[string]interface{}{"1": nil, "2": "Natal"}}}, State: newTestState()}, "body_params")
	if got["1"] != "" || got["2"] != "Natal" {
		t.Fatalf("params = %q", got)
	}
}

func TestANullQueryOrHeaderValueIsSentEmptyNeverAsNilText(t *testing.T) {
	seen := make(chan *http.Request, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	state := workflow.NewRunState()
	ctx := httpNodeCtx("h1", map[string]interface{}{
		"url":          srv.URL,
		"query_params": map[string]interface{}{"cep": nil},
		"headers":      map[string]interface{}{"X-Cidade": nil},
	}, &state)
	if _, err := NewHTTPRequestExecutor(testEgress(), nil).Execute(ctx); err != nil {
		t.Fatalf("execute: %v", err)
	}
	r := <-seen
	if strings.Contains(r.URL.RawQuery, "nil") || strings.Contains(r.Header.Get("X-Cidade"), "nil") {
		t.Fatalf("query %q header %q", r.URL.RawQuery, r.Header.Get("X-Cidade"))
	}
}

func TestANullAttendeeInAListIsNeverSentAsNilText(t *testing.T) {
	attendees, _ := parseCalendarAttendees([]interface{}{nil, "ana@example.com"}, &workflow.RunState{Vars: map[string]interface{}{}})
	for _, a := range attendees {
		if strings.Contains(a.Email, "<nil>") {
			t.Fatalf("attendees = %+v", attendees)
		}
	}
}

func TestACalendarEventFieldThatIsNotTextNeverBecomesNilText(t *testing.T) {
	ctx := &workflow.NodeContext{Node: &workflow.Node{Config: map[string]interface{}{"title": []interface{}{nil}}}, State: &workflow.RunState{Vars: map[string]interface{}{}}}
	if got := calendarConfigText(ctx, "title"); strings.Contains(got, "<nil>") {
		t.Fatalf("title = %q", got)
	}
}
