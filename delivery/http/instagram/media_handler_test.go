package instagram

import (
	"testing"

	igdomain "vozko/domain/instagram"
)

func TestToMediaResponse_DoesNotUseVideoAsThumbnail(t *testing.T) {
	h := &Handler{}
	media := &igdomain.RemoteMedia{
		IGMediaID: "video-1",
		MediaType: igdomain.MediaTypeVideo,
		MediaURL:  "https://cdn.example/video.mp4",
	}

	got := h.toMediaResponse("account-1", media)
	if got.MediaURL == "" {
		t.Fatal("expected a media URL")
	}
	if got.ThumbnailURL != "" {
		t.Fatalf("thumbnail URL = %q, want empty for a video without thumbnail_url", got.ThumbnailURL)
	}
}

func TestToMediaResponse_UsesImageAsThumbnail(t *testing.T) {
	h := &Handler{}
	media := &igdomain.RemoteMedia{
		IGMediaID: "image-1",
		MediaType: igdomain.MediaTypeImage,
		MediaURL:  "https://cdn.example/image.jpg",
	}

	got := h.toMediaResponse("account-1", media)
	if got.ThumbnailURL != got.MediaURL {
		t.Fatalf("thumbnail URL = %q, want media URL %q", got.ThumbnailURL, got.MediaURL)
	}
}
