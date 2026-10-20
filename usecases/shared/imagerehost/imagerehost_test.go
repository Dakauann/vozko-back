package imagerehost

import (
	"context"
	"errors"
	"testing"
)

type fetcher struct {
	data        []byte
	contentType string
	err         error
}

func (f fetcher) FetchBytes(context.Context, string) ([]byte, string, error) {
	return f.data, f.contentType, f.err
}

type storage struct {
	uploaded map[string]string
}

func (s *storage) UploadFile(key string, data []byte, contentType string) error {
	s.uploaded[key] = contentType + ":" + string(data)
	return nil
}

func (s *storage) GetFileURL(key string) string { return "https://cdn.example/" + key }

func TestAnImageIsStoredUnderItsKeyWithItsExtension(t *testing.T) {
	cases := map[string]string{
		"image/png":                 ".png",
		"image/gif":                 ".gif",
		"image/webp":                ".webp",
		"image/jpeg":                ".jpg",
		"image/jpeg; charset=UTF-8": ".jpg",
	}
	for contentType, ext := range cases {
		s := &storage{uploaded: map[string]string{}}
		store := New(fetcher{data: []byte("img"), contentType: contentType}, s)
		key, err := store.Rehost(context.Background(), "base/thumbnail", "https://img")
		if err != nil {
			t.Fatalf("%s: %v", contentType, err)
		}
		if key != "base/thumbnail"+ext || s.uploaded[key] == "" {
			t.Fatalf("%s: key=%q uploaded=%v", contentType, key, s.uploaded)
		}
	}
}

func TestOnlyImagesAreStored(t *testing.T) {
	s := &storage{uploaded: map[string]string{}}
	if _, err := New(fetcher{data: []byte("<html>"), contentType: "text/html"}, s).Rehost(context.Background(), "k", "https://x"); err == nil {
		t.Fatal("a page is not an image")
	}
	if _, err := New(fetcher{err: errors.New("down")}, s).Rehost(context.Background(), "k", "https://x"); err == nil {
		t.Fatal("a failed download must fail")
	}
	if len(s.uploaded) != 0 {
		t.Fatalf("stored %v", s.uploaded)
	}
}

func TestTheURLOfAKey(t *testing.T) {
	store := New(fetcher{}, &storage{})
	if store.URL("a/b.jpg") != "https://cdn.example/a/b.jpg" || store.URL("") != "" {
		t.Fatal("url resolution wrong")
	}
}

func TestTheStoreNeedsItsDependencies(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a store without storage must not be built")
		}
	}()
	New(fetcher{}, nil)
}
