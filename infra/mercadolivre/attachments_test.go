package mercadolivre

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	mldomain "vozko/domain/mercadolivre"
)

func TestUploadSendsMultipartFile(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, attempt int) {
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"id": "210438685_59f0f034-db1b-4ea6-8c5e-1d34e2092482.jpg"}`)
	})
	limiters := newFakeLimiters()
	s, err := NewAttachmentService(newTestClient(t, fs, limiters))
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.Upload(context.Background(), sellerCreds, mldomain.AttachmentUpload{FileName: "Anexo.jpg", ContentType: "image/jpeg", Data: []byte("jpegbytes")})
	if err != nil || id != "210438685_59f0f034-db1b-4ea6-8c5e-1d34e2092482.jpg" {
		t.Fatalf("Upload = %q, %v", id, err)
	}
	r := fs.last()
	if r.Method != http.MethodPost || r.Path != "/messages/attachments" || r.Query.Get("tag") != "post_sale" || r.Query.Get("site_id") != "MLB" {
		t.Fatalf("%s %s %v", r.Method, r.Path, r.Query)
	}
	assertBearer(t, r)
	if keys := limiters.keys(); keys[1] != "ml:app:messages:write" {
		t.Fatalf("limiter keys = %v", keys)
	}
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("content type = %q", r.Header.Get("Content-Type"))
	}
	reader := multipart.NewReader(strings.NewReader(r.Body), params["boundary"])
	part, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(part)
	if part.FormName() != "file" || part.FileName() != "Anexo.jpg" || part.Header.Get("Content-Type") != "image/jpeg" || string(data) != "jpegbytes" {
		t.Fatalf("part %q %q %q %q", part.FormName(), part.FileName(), part.Header.Get("Content-Type"), data)
	}
}

func TestUploadRefusesBeforeCalling(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{"id":"x"}`))
	s, _ := NewAttachmentService(newTestClient(t, fs, newFakeLimiters()))
	cases := []struct {
		up  mldomain.AttachmentUpload
		err error
	}{
		{mldomain.AttachmentUpload{FileName: "a.gif", ContentType: "image/gif", Data: []byte("x")}, mldomain.ErrMediaUnsupported},
		{mldomain.AttachmentUpload{FileName: "a.pdf", ContentType: "application/pdf"}, mldomain.ErrMediaUnsupported},
		{mldomain.AttachmentUpload{FileName: "a/b.pdf", ContentType: "application/pdf", Data: []byte("x")}, mldomain.ErrMediaUnsupported},
		{mldomain.AttachmentUpload{FileName: "a.pdf", ContentType: "application/pdf", Data: make([]byte, 25<<20+1)}, mldomain.ErrMediaTooLarge},
	}
	for i, tc := range cases {
		if _, err := s.Upload(context.Background(), sellerCreds, tc.up); !errors.Is(err, tc.err) {
			t.Errorf("case %d err = %v, want %v", i, err, tc.err)
		}
	}
	if fs.count() != 0 {
		t.Fatal("refused uploads must not reach Mercado Livre")
	}
}

func TestUploadIsNotRetriedOnAServerError(t *testing.T) {
	fs := newFakeServer(t, jsonReply(500, `{"message":"File can not be saved, try it later"}`))
	s, _ := NewAttachmentService(newTestClient(t, fs, newFakeLimiters()))
	if _, err := s.Upload(context.Background(), sellerCreds, mldomain.AttachmentUpload{FileName: "a.pdf", ContentType: "application/pdf", Data: []byte("x")}); err == nil || fs.count() != 1 {
		t.Fatalf("err = %v, attempts = %d", err, fs.count())
	}
}

func TestUploadRetriesARateLimitWithTheSameBody(t *testing.T) {
	var bodies []string
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, attempt int) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
		if attempt == 1 {
			w.WriteHeader(429)
			_, _ = io.WriteString(w, `{"error":"local_rate_limited"}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"1_a.pdf"}`)
	})
	s, _ := NewAttachmentService(newTestClient(t, fs, newFakeLimiters()))
	if _, err := s.Upload(context.Background(), sellerCreds, mldomain.AttachmentUpload{FileName: "a.pdf", ContentType: "application/pdf", Data: []byte("pdf")}); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || bodies[0] != bodies[1] || !strings.Contains(bodies[1], "pdf") {
		t.Fatalf("bodies = %q", bodies)
	}
}

func TestDownload(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, attempt int) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG"))
	})
	limiters := newFakeLimiters()
	s, _ := NewAttachmentService(newTestClient(t, fs, limiters))
	d, err := s.Download(context.Background(), sellerCreds, "76601286_5946e4c4-168a-45fd-945e-b8f0c306c58d.png")
	if err != nil {
		t.Fatal(err)
	}
	r := fs.last()
	if r.Path != "/messages/attachments/76601286_5946e4c4-168a-45fd-945e-b8f0c306c58d.png" || r.Query.Get("tag") != "post_sale" || r.Query.Get("site_id") != "MLB" {
		t.Fatalf("%s %v", r.Path, r.Query)
	}
	if keys := limiters.keys(); keys[1] != "ml:app:messages:read" {
		t.Fatalf("limiter keys = %v", keys)
	}
	if d.ContentType != "image/png" || !bytes.Equal(d.Data, []byte("\x89PNG")) {
		t.Fatalf("download = %+v", d)
	}
	if _, err := s.Download(context.Background(), sellerCreds, "../../users/me"); err == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestDownloadRefusesAFileOverTheLimit(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, attempt int) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(make([]byte, 25<<20+1))
	})
	s, _ := NewAttachmentService(newTestClient(t, fs, newFakeLimiters()))
	if _, err := s.Download(context.Background(), sellerCreds, "1_a.pdf"); err == nil {
		t.Fatal("oversized download accepted")
	}
}
