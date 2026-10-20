package mercadolivre

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/mux"

	mluc "vozko/usecases/mercadolivre"
)

const (
	testAppID = "5503910054141466"
	testToken = "q3Jv9xZ0b2xH5nLw1aPq8sT4uV7yB6cD-eF_gH2iJkM"
)

type queue struct {
	mu   sync.Mutex
	sent map[string]int
	err  error
}

func (q *queue) Publish(topic string, _ []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	q.sent[topic]++
	return nil
}

type noMetrics struct{}

func (noMetrics) AddMercadoLivreEvent(string, string) {}

func notificationRouter(q *queue) *mux.Router {
	accept := mluc.NewAcceptNotification(testAppID, testToken, mluc.NewJobPublisher(q), noMetrics{})
	router := mux.NewRouter()
	RegisterPublicRoutes(router, nil, NewNotificationHandler(accept))
	return router
}

const validNotification = `{"_id":"n-1","resource":"/questions/5036111111","user_id":"415458330","topic":"questions","application_id":5503910054141466,"attempts":1,"sent":"2026-10-19T12:00:00.000Z","received":"2026-10-19T12:00:00.000Z"}`

func post(router *mux.Router, token string, body []byte) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/mercadolivre/"+token, bytes.NewReader(body)))
	return rec
}

func TestNotificationStatuses(t *testing.T) {
	cases := []struct {
		name   string
		token  string
		body   []byte
		qErr   error
		status int
	}{
		{"accepted", testToken, []byte(validNotification), nil, http.StatusOK},
		{"wrong token", "q3Jv9xZ0b2xH5nLw1aPq8sT4uV7yB6cD-eF_gH2iJkX", []byte(validNotification), nil, http.StatusNotFound},
		{"oversized body", testToken, append([]byte(validNotification), bytes.Repeat([]byte(" "), 64<<10)...), nil, http.StatusRequestEntityTooLarge},
		{"not json", testToken, []byte("hi"), nil, http.StatusBadRequest},
		{"another application", testToken, []byte(strings.Replace(validNotification, testAppID, "1", 1)), nil, http.StatusBadRequest},
		{"queue down", testToken, []byte(validNotification), errors.New("broker down"), http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := &queue{sent: map[string]int{}, err: tc.qErr}
			rec := post(notificationRouter(q), tc.token, tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
			}
			published := 0
			for _, n := range q.sent {
				published += n
			}
			if (tc.status == http.StatusOK) != (published == 1) {
				t.Fatalf("published = %d", published)
			}
		})
	}
}

func TestTheNotificationRouteAcceptsOnlyPost(t *testing.T) {
	router := notificationRouter(&queue{sent: map[string]int{}})
	var match mux.RouteMatch
	if router.Match(httptest.NewRequest(http.MethodGet, "/webhooks/mercadolivre/"+testToken, nil), &match) && match.MatchErr == nil {
		t.Fatal("GET must not reach the notification handler")
	}
}
