package advertising

import (
	"bytes"
	"context"
	"errors"
	"testing"

	ads "vozko/domain/advertising"
	"vozko/domain/media"
)

func bannerFacts() *ads.MediaFacts {
	return &ads.MediaFacts{Kind: ads.MediaImage, Container: ads.ContainerPNG, Bytes: 1 << 20, Width: 3000, Height: 600}
}

func TestPreflightRefusesALibraryImageOutsideTheMetaSpec(t *testing.T) {
	w := newWorld()
	w.media.facts = bannerFacts()
	_, err := w.publisher().Preflight(context.Background(), "ws-1", publishableDraft())
	requireIssue(t, err, "ads[0].creative.media", ads.CodeMediaRatio)
}

func TestPreflightRefusesAFileItCannotRead(t *testing.T) {
	w := newWorld()
	w.media.factsErr = ErrUnreadableCreative
	_, err := w.publisher().Preflight(context.Background(), "ws-1", publishableDraft())
	requireIssue(t, err, "ads[0].creative.media", ads.CodeMediaUnreadable)
}

func TestPreflightNeverReadsTheFilesOfImagesAlreadyOnMeta(t *testing.T) {
	w := newWorld()
	w.media.facts = bannerFacts()
	w.gateway.metaMedia = map[string]string{"meta:h1": "https://cdn.meta/h1.png"}
	if _, err := w.publisher().Preflight(context.Background(), "ws-1", metaImageDraft()); err != nil {
		t.Fatal(err)
	}
	if w.media.inspected != 0 {
		t.Fatalf("media hosted on Meta was inspected %d times", w.media.inspected)
	}
}

func TestACreativeSwapToAnOffSpecFileIsRefusedBeforeMeta(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	w.gateway.detail = &ads.ObjectDetail{Identity: ads.Identity{PageID: "page-1"}}
	w.media.facts = &ads.MediaFacts{Kind: ads.MediaVideo, Container: ads.ContainerMP4, Bytes: 1 << 20, Width: 1080, Height: 1920, DurationMS: 400}
	creative := ads.CreativeDraft{Format: ads.FormatVideo, PrimaryText: "Oi", Media: ads.MediaRef{Kind: ads.MediaVideo, MediaID: "media-9"}}
	_, err := w.manager().CheckEdit(context.Background(), "ws-1", "a-1", ads.ObjectEdit{Creative: &creative})
	requireIssue(t, err, "creative.media", ads.CodeVideoTooShort)
	if len(w.gateway.creatives) != 0 {
		t.Fatal("an off spec swap must never reach Meta")
	}
}

type probeFunc func(kind ads.MediaKind, file ads.MediaFile) (ads.MediaFacts, error)

func (f probeFunc) Facts(kind ads.MediaKind, file ads.MediaFile) (ads.MediaFacts, error) {
	return f(kind, file)
}

type countingFetcher struct{ calls int }

func (f *countingFetcher) Fetch(context.Context, string) ([]byte, error) {
	f.calls++
	return []byte(pngHead), nil
}

const pngHead = "\x89PNG\r\n\x1a\n0000000000000000"

type pngFile struct{ *bytes.Reader }

func (f pngFile) Size() int64 { return f.Reader.Size() }

func (f *countingFetcher) Open(context.Context, string) (ads.MediaFile, error) {
	f.calls++
	return pngFile{bytes.NewReader([]byte(pngHead))}, nil
}

type oneMedia struct{}

func (oneMedia) GetMedia(_, id string) (*media.Media, error) {
	if id != "media-1" {
		return nil, media.ErrMediaNotFound
	}
	return &media.Media{ID: id, URL: "https://cdn/media-1.png", Type: media.MediaTypeProductImage}, nil
}

func TestTheLibraryReadsAFileOnceAndReportsWhatTheProbeFound(t *testing.T) {
	fetcher := &countingFetcher{}
	want := ads.MediaFacts{Kind: ads.MediaImage, Container: ads.ContainerPNG, Bytes: 24, Width: 1080, Height: 1080}
	source := NewMediaSource(oneMedia{}, fetcher, probeFunc(func(ads.MediaKind, ads.MediaFile) (ads.MediaFacts, error) { return want, nil }))
	ref := ads.MediaRef{Kind: ads.MediaImage, MediaID: "media-1"}
	for range 3 {
		facts, err := source.Facts(context.Background(), "ws-1", ref)
		if err != nil || facts != want {
			t.Fatalf("facts %+v err %v", facts, err)
		}
	}
	if fetcher.calls != 1 {
		t.Fatalf("the file was downloaded %d times; its facts never change", fetcher.calls)
	}
	if _, err := source.Facts(context.Background(), "ws-1", ads.MediaRef{Kind: ads.MediaImage, MediaID: "gone"}); !errors.Is(err, media.ErrMediaNotFound) {
		t.Fatalf("a missing media must stay missing, got %v", err)
	}
}

func TestTheLibraryTurnsAProbeFailureIntoAnUnreadableFile(t *testing.T) {
	source := NewMediaSource(oneMedia{}, &countingFetcher{}, probeFunc(func(ads.MediaKind, ads.MediaFile) (ads.MediaFacts, error) {
		return ads.MediaFacts{}, errors.New("broken header")
	}))
	_, err := source.Facts(context.Background(), "ws-1", ads.MediaRef{Kind: ads.MediaImage, MediaID: "media-1"})
	if !errors.Is(err, ErrUnreadableCreative) {
		t.Fatalf("got %v", err)
	}
}
