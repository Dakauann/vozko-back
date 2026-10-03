package marketing

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/infra/meta/metatest"
)

type uploadedFile struct {
	field    string
	fileName string
	data     []byte
}

type recordedCall struct {
	method string
	path   string
	query  url.Values
	form   url.Values
	file   *uploadedFile
}

func gatewayWith(t *testing.T, respond func(call recordedCall) (int, string)) (*Gateway, *[]recordedCall) {
	t.Helper()
	calls := &[]recordedCall{}
	client := metatest.Server(t, func(w http.ResponseWriter, r *http.Request) {
		call := recordedCall{method: r.Method, path: r.URL.Path, query: r.URL.Query(), form: url.Values{}}
		mediaType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		switch {
		case mediaType == "application/x-www-form-urlencoded":
			raw, _ := io.ReadAll(r.Body)
			call.form, _ = url.ParseQuery(string(raw))
		case strings.HasPrefix(mediaType, "multipart/"):
			reader := multipart.NewReader(r.Body, params["boundary"])
			for {
				part, err := reader.NextPart()
				if err != nil {
					break
				}
				data, _ := io.ReadAll(part)
				if part.FileName() != "" {
					call.file = &uploadedFile{field: part.FormName(), fileName: part.FileName(), data: data}
					continue
				}
				call.form.Add(part.FormName(), string(data))
			}
		}
		*calls = append(*calls, call)
		status, body := respond(call)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	g, err := newGateway(Config{AppSecret: "secret", HTTPClient: client}, 1)
	if err != nil {
		t.Fatal(err)
	}
	g.wait = func(context.Context, time.Duration) error { return nil }
	return g, calls
}

func ok(body string) func(recordedCall) (int, string) {
	return func(recordedCall) (int, string) { return http.StatusOK, body }
}

func routes(t *testing.T, table map[string]string) func(recordedCall) (int, string) {
	t.Helper()
	return func(call recordedCall) (int, string) {
		if body, found := table[call.method+" "+call.path]; found {
			return http.StatusOK, body
		}
		t.Errorf("unexpected call %s %s", call.method, call.path)
		return http.StatusNotFound, `{"error":{"message":"unexpected","code":100}}`
	}
}

func decodeJSON(t *testing.T, raw string) any {
	t.Helper()
	var out any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return out
}

func jsonEqual(t *testing.T, field, got, want string) {
	t.Helper()
	if !reflect.DeepEqual(decodeJSON(t, got), decodeJSON(t, want)) {
		t.Fatalf("%s = %s, want %s", field, got, want)
	}
}
