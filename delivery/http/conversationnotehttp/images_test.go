package conversationnotehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/conversation"
	"vozko/domain/conversationnote"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
)

var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func multipartImage(t *testing.T, field, name string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile(field, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, form.FormDataContentType()
}

func serveUpload(t *testing.T, notes *fakeNotes, path string, body *bytes.Buffer, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	RegisterRoutes(router, NewHandler(notes), (&guard{}).ac)
	req := httptest.NewRequest(http.MethodPost, path, body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Workspace-ID", "ws-1")
	req = req.WithContext(context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "u-1", Role: "member"}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

var sampleImage = conversationnote.Image{
	ID: "img-1", URL: "https://cdn/img-1.png", MimeType: "image/png",
	Layout: conversation.MediaLayout{Width: 1280, Height: 720, Thumbhash: "hash"},
}

func TestAttachImageHandsTheFileToTheNotesAndAnswersTheImage(t *testing.T) {
	notes := &fakeNotes{image: sampleImage}
	body, contentType := multipartImage(t, "image", "print.png", pngBytes)

	rec := serveUpload(t, notes, "/conversations/whatsapp/entry-1/notes/images", body, contentType)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
	in := notes.upload
	if in.WorkspaceID != "ws-1" || in.Entry != (shared.EntryRef{EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp}) || in.FileName != "print.png" || !bytes.Equal(in.Data, pngBytes) {
		t.Fatalf("upload = %+v", in)
	}
	var got struct {
		Image map[string]any `json:"image"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"id": "img-1", "url": "https://cdn/img-1.png", "mime_type": "image/png", "width": 1280.0, "height": 720.0, "thumbhash": "hash"}
	for key, value := range want {
		if got.Image[key] != value {
			t.Fatalf("image[%s] = %v, want %v (body %s)", key, got.Image[key], value, rec.Body)
		}
	}
}

func TestAttachImageRefusesAMissingFileWithACode(t *testing.T) {
	notes := &fakeNotes{}
	body, contentType := multipartImage(t, "media", "print.png", pngBytes)
	rec := serveUpload(t, notes, "/conversations/whatsapp/entry-1/notes/images", body, contentType)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "image_required") {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
	if notes.upload.Data != nil {
		t.Fatal("the use case must not run without a file")
	}
}

func TestAttachImageRefusesABodyOverTheLimitBeforeTheUseCase(t *testing.T) {
	notes := &fakeNotes{}
	huge := append(append([]byte{}, pngBytes...), bytes.Repeat([]byte{0}, conversationnote.MaxImageBytes+uploadOverhead)...)
	body, contentType := multipartImage(t, "image", "big.png", huge)
	rec := serveUpload(t, notes, "/conversations/whatsapp/entry-1/notes/images", body, contentType)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "image_too_large") {
		t.Fatalf("status = %d body = %.200s", rec.Code, rec.Body)
	}
	if notes.upload.Data != nil {
		t.Fatal("an oversized body never reaches the use case")
	}
}

func TestAttachImageRefusesAnUnknownChannel(t *testing.T) {
	body, contentType := multipartImage(t, "image", "a.png", pngBytes)
	rec := serveUpload(t, &fakeNotes{}, "/conversations/voice/entry-1/notes/images", body, contentType)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_entry_type") {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
}

func TestCreatePassesTheImageIDsAndListAnswersEachImage(t *testing.T) {
	withImages := sampleNote
	withImages.ImageIDs = []string{"img-1"}
	withImages.Images = []conversationnote.Image{sampleImage}
	notes := &fakeNotes{created: withImages, page: conversationnote.Page{Notes: []conversationnote.Authored{withImages}}}

	rec := serve(t, notes, http.MethodPost, "/conversations/whatsapp/entry-1/notes", `{"body":"","image_ids":["img-1"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
	if strings.Join(notes.draft.ImageIDs, ",") != "img-1" {
		t.Fatalf("draft = %+v", notes.draft)
	}

	list := serve(t, notes, http.MethodGet, "/conversations/whatsapp/entry-1/notes", "")
	var body struct {
		Notes []struct {
			Images []map[string]any `json:"images"`
		} `json:"notes"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Notes) != 1 || len(body.Notes[0].Images) != 1 || body.Notes[0].Images[0]["url"] != "https://cdn/img-1.png" || body.Notes[0].Images[0]["thumbhash"] != "hash" {
		t.Fatalf("body = %s", list.Body)
	}
}

func TestANoteWithoutImagesAnswersAnEmptyImageList(t *testing.T) {
	rec := serve(t, &fakeNotes{created: sampleNote}, http.MethodPost, "/conversations/whatsapp/entry-1/notes", `{"body":"x"}`)
	if !strings.Contains(rec.Body.String(), `"images":[]`) {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestImageRefusalsMapToStableCodes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{conversationnote.ErrTooManyImages, http.StatusBadRequest, "too_many_images"},
		{conversationnote.ErrImageRequired, http.StatusBadRequest, "image_required"},
		{conversationnote.ErrImageType, http.StatusBadRequest, "image_type"},
		{conversationnote.ErrImageTooLarge, http.StatusRequestEntityTooLarge, "image_too_large"},
		{conversationnote.ErrImageNotFound, http.StatusBadRequest, "image_not_found"},
		{conversationnote.ErrStorageMissing, http.StatusServiceUnavailable, "storage_unavailable"},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			rec := serve(t, &fakeNotes{err: c.err}, http.MethodPost, "/conversations/whatsapp/entry-1/notes", `{"body":"x"}`)
			if rec.Code != c.status || !strings.Contains(rec.Body.String(), `"code":"`+c.code+`"`) {
				t.Fatalf("status = %d body = %s, want %d %s", rec.Code, rec.Body, c.status, c.code)
			}
			body, contentType := multipartImage(t, "image", "a.png", pngBytes)
			up := serveUpload(t, &fakeNotes{err: c.err}, "/conversations/whatsapp/entry-1/notes/images", body, contentType)
			if up.Code != c.status || !strings.Contains(up.Body.String(), `"code":"`+c.code+`"`) {
				t.Fatalf("upload status = %d body = %s, want %d %s", up.Code, up.Body, c.status, c.code)
			}
		})
	}
}
