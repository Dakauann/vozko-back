package meta

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type failingTransport struct {
	calls atomic.Int32
}

func (t *failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return nil, errors.New("connection reset")
}

func newTestClient(t *testing.T, httpClient *http.Client, host string) *Client {
	t.Helper()
	c, err := NewClient(Config{Host: host, APIVersion: "v25.0", HTTPClient: httpClient, MaxRetries: 2})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	c.baseURL = "http://" + host + "/v25.0"
	c.sleep = func(context.Context, time.Duration) error { return nil }
	return c
}

func TestTransportFailureRetryDependsOnIdempotency(t *testing.T) {
	cases := []struct {
		name      string
		req       Request
		wantCalls int32
	}{
		{"get is retried", Request{Method: http.MethodGet, Path: "/x"}, 3},
		{"delete is retried", Request{Method: http.MethodDelete, Path: "/x"}, 3},
		{"post send is not retried", Request{Method: http.MethodPost, Path: "/x/messages"}, 1},
		{"post opted in is retried", Request{Method: http.MethodPost, Path: "/x/subscribed_apps", Idempotent: true}, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transport := &failingTransport{}
			c := newTestClient(t, &http.Client{Transport: transport}, "graph.test")
			if err := c.Do(context.Background(), tc.req, nil); err == nil {
				t.Fatal("expected an error")
			}
			if got := transport.calls.Load(); got != tc.wantCalls {
				t.Fatalf("calls = %d, want %d", got, tc.wantCalls)
			}
		})
	}
}

func TestMetaErrorRetryDependsOnIdempotency(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		req       Request
		wantCalls int32
	}{
		{"post 500 without transient flag is not retried", 500, `{"error":{"code":2,"message":"unexpected"}}`, Request{Method: http.MethodPost, Path: "/m"}, 1},
		{"post transient is retried", 400, `{"error":{"code":1,"is_transient":true,"message":"try again"}}`, Request{Method: http.MethodPost, Path: "/m"}, 3},
		{"post rate limited is retried", 400, `{"error":{"code":613,"message":"calls exceeded"}}`, Request{Method: http.MethodPost, Path: "/m"}, 3},
		{"get 500 is retried", 500, `{"error":{"code":2,"message":"unexpected"}}`, Request{Method: http.MethodGet, Path: "/m"}, 3},
		{"post permission error is not retried", 400, `{"error":{"code":10,"message":"denied"}}`, Request{Method: http.MethodPost, Path: "/m"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			c := newTestClient(t, srv.Client(), srv.Listener.Addr().String())
			err := c.Do(context.Background(), tc.req, nil)
			if _, ok := AsError(err); !ok {
				t.Fatalf("want *Error, got %v", err)
			}
			if got := calls.Load(); got != tc.wantCalls {
				t.Fatalf("calls = %d, want %d", got, tc.wantCalls)
			}
		})
	}
}

func TestBusinessUseCaseUsageIsParsedPerObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Business-Use-Case-Usage",
			`{"111":[{"type":"pages","call_count":97,"total_cputime":23,"total_time":40,"estimated_time_to_regain_access":0},{"type":"messenger","call_count":12,"total_cputime":90,"total_time":1,"estimated_time_to_regain_access":7}]}`)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv.Client(), srv.Listener.Addr().String())
	if err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/111"}, nil); err != nil {
		t.Fatalf("do: %v", err)
	}
	got, ok := c.UsageFor("111")
	if !ok {
		t.Fatal("usage for object 111 missing")
	}
	want := Usage{CallCount: 97, TotalCPUTime: 90, TotalTime: 40, EstimatedRegainMinutes: 7}
	if got != want {
		t.Fatalf("usage = %+v, want %+v", got, want)
	}
	if got.Percent() != 97 {
		t.Fatalf("percent = %d, want 97", got.Percent())
	}
}

func TestAppUsageHeaderIsParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-App-Usage", `{"call_count":28,"total_time":25,"total_cputime":26}`)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv.Client(), srv.Listener.Addr().String())
	if err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/x"}, nil); err != nil {
		t.Fatalf("do: %v", err)
	}
	want := Usage{CallCount: 28, TotalCPUTime: 26, TotalTime: 25}
	if got := c.LastUsage(); got != want {
		t.Fatalf("usage = %+v, want %+v", got, want)
	}
}

func TestAppSecretProofIsSentWithTheToken(t *testing.T) {
	var proof string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proof = r.URL.Query().Get("appsecret_proof")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c, err := NewClient(Config{Host: srv.Listener.Addr().String(), APIVersion: "v25.0", AppSecret: "s3cret", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	c.baseURL = "http://" + srv.Listener.Addr().String() + "/v25.0"
	if err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/me", Token: "tok"}, nil); err != nil {
		t.Fatal(err)
	}
	if proof != AppSecretProof("tok", "s3cret") {
		t.Fatalf("proof = %q", proof)
	}
}

func TestNewClientRequiresHostAndVersion(t *testing.T) {
	if _, err := NewClient(Config{APIVersion: "v25.0"}); err == nil {
		t.Fatal("missing host accepted")
	}
	if _, err := NewClient(Config{Host: "graph.facebook.com"}); err == nil {
		t.Fatal("missing version accepted")
	}
}

func TestMultipartRequestCarriesFieldsAndFile(t *testing.T) {
	var gotField, gotName, gotType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
		}
		gotField = r.FormValue("message")
		file, header, err := r.FormFile("filedata")
		if err != nil {
			t.Errorf("file: %v", err)
			return
		}
		defer file.Close()
		buf := make([]byte, 16)
		n, _ := file.Read(buf)
		gotName, gotType, gotBody = header.Filename, header.Header.Get("Content-Type"), string(buf[:n])
		_, _ = w.Write([]byte(`{"attachment_id":"42"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv.Client(), srv.Listener.Addr().String())
	var out struct {
		AttachmentID string `json:"attachment_id"`
	}
	err := c.Do(context.Background(), Request{
		Method: http.MethodPost,
		Path:   "/me/message_attachments",
		Form:   map[string][]string{"message": {`{"attachment":{"type":"file"}}`}},
		File:   &FilePart{Field: "filedata", FileName: "doc.pdf", ContentType: "application/pdf", Data: []byte("%PDF")},
	}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.AttachmentID != "42" || gotField == "" || gotName != "doc.pdf" || gotType != "application/pdf" || gotBody != "%PDF" {
		t.Fatalf("field=%q name=%q type=%q body=%q", gotField, gotName, gotType, gotBody)
	}
}

func TestOnlyATopLevelGraphErrorFailsASuccessfulResponse(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"status value named error", `{"id":"9","status":{"video_status":"error"}}`, false},
		{"error key without code or message", `{"error":null,"id":"9"}`, false},
		{"graph error inside a 200", `{"error":{"code":100,"message":"Invalid parameter"}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			c := newTestClient(t, srv.Client(), srv.Listener.Addr().String())
			var out map[string]any
			err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/9"}, &out)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
