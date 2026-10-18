package notification_service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/notification"
)

type memoryFiles struct {
	base     string
	uploaded map[string]string
	err      error
}

func (m *memoryFiles) UploadFile(key string, _ []byte, contentType string) error {
	if m.err != nil {
		return m.err
	}
	m.uploaded[key] = contentType
	return nil
}

func (m *memoryFiles) GetFileURL(key string) string { return m.base + "/" + key }

func TestImageStoreSavesUnderTheNotificationPrefixAndOwnsOnlyThoseURLs(t *testing.T) {
	files := &memoryFiles{base: "https://cdn.vozko.test", uploaded: map[string]string{}}
	store := NewImageStore(files)

	url, err := store.Save(context.Background(), []byte("png"), notification.Image{MIME: "image/png", Ext: ".png"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !strings.HasPrefix(url, "https://cdn.vozko.test/notifications/images/") || !strings.HasSuffix(url, ".png") {
		t.Fatalf("url = %q", url)
	}
	for key, mime := range files.uploaded {
		if !strings.HasPrefix(key, "notifications/images/") || mime != "image/png" {
			t.Fatalf("uploaded %q as %q", key, mime)
		}
	}
	if !store.Owns(url) {
		t.Fatal("a saved image is ours")
	}
	for _, foreign := range []string{
		"https://cdn.vozko.test/conversations/x.png",
		"https://evil.example/notifications/images/x.png",
		"https://cdn.vozko.test/notifications/images/",
		"https://cdn.vozko.test/notifications/images/../x.png",
		"https://cdn.vozko.test/notifications/images/a.png?track=1",
	} {
		if store.Owns(foreign) {
			t.Fatalf("%q must not be ours", foreign)
		}
	}
}

func TestImageStoreFailsClosedWithoutHTTPSStorage(t *testing.T) {
	plain := NewImageStore(&memoryFiles{base: "http://cdn.vozko.test", uploaded: map[string]string{}})
	if plain.Owns("http://cdn.vozko.test/notifications/images/a.png") {
		t.Fatal("a non-https storage base owns nothing, so no image can reach an email")
	}
	if NewImageStore(nil).Owns("https://x/notifications/images/a.png") {
		t.Fatal("no storage owns nothing")
	}
	broken := NewImageStore(&memoryFiles{base: "https://cdn", uploaded: map[string]string{}, err: errors.New("r2 down")})
	if _, err := broken.Save(context.Background(), []byte("x"), notification.Image{Ext: ".png"}); err == nil {
		t.Fatal("upload errors propagate")
	}
}
