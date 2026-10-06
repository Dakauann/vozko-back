package rembg

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/mediagen"
)

func servers(t *testing.T, status int, reply string) (*httptest.Server, *string) {
	t.Helper()
	var model string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/photo.jpg":
			_, _ = w.Write([]byte("jpeg bytes"))
		case "/api/remove":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
			model = r.FormValue("model")
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
			} else {
				data, _ := io.ReadAll(file)
				if string(data) != "jpeg bytes" {
					t.Errorf("uploaded %q", data)
				}
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(reply))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &model
}

func generate(t *testing.T, srv *httptest.Server) (*mediagen.Output, error) {
	t.Helper()
	client, err := New(srv.URL, srv.Client(), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client.Generate(context.Background(), mediagen.Request{}, []mediagen.Source{{MediaID: "m-1", URL: srv.URL + "/photo.jpg"}})
}

func TestTheCommercialModelIsAlwaysNamedAndTheFileUploaded(t *testing.T) {
	srv, model := servers(t, http.StatusOK, pngSignature+"pixels")
	out, err := generate(t, srv)
	if err != nil {
		t.Fatal(err)
	}
	if *model != "isnet-general-use" || out.MIMEType != "image/png" || out.CostReported {
		t.Fatalf("model %q out %+v", *model, out)
	}
}

func TestAnythingButAPNGIsRefused(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		reply  string
	}{
		"error status": {http.StatusInternalServerError, "boom"},
		"not a png":    {http.StatusOK, "<html>"},
	} {
		srv, _ := servers(t, c.status, c.reply)
		if _, err := generate(t, srv); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
