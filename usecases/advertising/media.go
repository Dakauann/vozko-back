package advertising

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	ads "vozko/domain/advertising"
	"vozko/domain/media"
)

const (
	videoPollInterval = 5 * time.Second
	videoPollAttempts = 18
)

type CreativeFile struct {
	MediaID string
	URL     string
	Name    string
	Bytes   []byte
}

type MediaSource interface {
	Describe(ctx context.Context, workspaceID string, ref ads.MediaRef) (*media.Media, error)
	Load(ctx context.Context, workspaceID string, ref ads.MediaRef) (*CreativeFile, error)
}

type mediaReader interface {
	GetMedia(workspaceID, mediaID string) (*media.Media, error)
}

type fileFetcher interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
}

type library struct {
	media   mediaReader
	fetcher fileFetcher
}

func NewMediaSource(media mediaReader, fetcher fileFetcher) MediaSource {
	return &library{media: media, fetcher: fetcher}
}

func libraryKindMatches(kind ads.MediaKind, t media.MediaType) bool {
	switch kind {
	case ads.MediaImage:
		return t == media.MediaTypeProductImage
	case ads.MediaVideo:
		return t == media.MediaTypeProductVideo || t == media.MediaTypeVslVideo
	}
	return false
}

func (l *library) Describe(_ context.Context, workspaceID string, ref ads.MediaRef) (*media.Media, error) {
	m, err := l.media.GetMedia(workspaceID, strings.TrimSpace(ref.MediaID))
	if err != nil {
		return nil, err
	}
	if m == nil || strings.TrimSpace(m.URL) == "" || !libraryKindMatches(ref.Kind, m.Type) {
		return nil, ads.ErrMediaNotImage
	}
	return m, nil
}

func (l *library) Load(ctx context.Context, workspaceID string, ref ads.MediaRef) (*CreativeFile, error) {
	m, err := l.Describe(ctx, workspaceID, ref)
	if err != nil {
		return nil, err
	}
	data, err := l.fetcher.Fetch(ctx, m.URL)
	if err != nil {
		return nil, fmt.Errorf("ads: download creative media: %w", err)
	}
	sniffed := http.DetectContentType(data)
	if (ref.Kind == ads.MediaImage && !strings.HasPrefix(sniffed, "image/")) || (ref.Kind == ads.MediaVideo && !strings.HasPrefix(sniffed, "video/")) {
		return nil, ads.ErrMediaNotImage
	}
	name := path.Base(m.URL)
	if name == "" || name == "." || name == "/" {
		name = m.ID
	}
	return &CreativeFile{MediaID: m.ID, URL: m.URL, Name: name, Bytes: data}, nil
}

type mediaGateway interface {
	UploadImage(ctx context.Context, token, metaAccountID string, image []byte, fileName string) (string, error)
	UploadVideo(ctx context.Context, token, metaAccountID string, video []byte, fileName string) (string, error)
	VideoStatus(ctx context.Context, token, videoID string) (*ads.RemoteVideo, error)
}

type creativeMedia struct {
	source  MediaSource
	gateway mediaGateway
	sleep   func(context.Context, time.Duration) error
}

func newCreativeMedia(source MediaSource, gateway mediaGateway) creativeMedia {
	return creativeMedia{source: source, gateway: gateway, sleep: sleepFor}
}

func sleepFor(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func IsMetaMedia(ref ads.MediaRef) bool {
	_, ok := ref.MetaID()
	return ok
}

func (c creativeMedia) describe(ctx context.Context, workspaceID string, ref ads.MediaRef) (string, error) {
	if IsMetaMedia(ref) {
		return "", nil
	}
	described, err := c.source.Describe(ctx, workspaceID, ref)
	if err != nil {
		return "", err
	}
	return described.URL, nil
}

func (c creativeMedia) upload(ctx context.Context, workspaceID string, account *ads.AdAccount, token string, ref ads.MediaRef) (string, error) {
	if metaID, ok := ref.MetaID(); ok {
		return metaID, nil
	}
	file, err := c.source.Load(ctx, workspaceID, ref)
	if err != nil {
		return "", &localFailure{err: err}
	}
	if ref.Kind == ads.MediaVideo {
		return c.gateway.UploadVideo(ctx, token, account.MetaAccountID, file.Bytes, file.Name)
	}
	return c.gateway.UploadImage(ctx, token, account.MetaAccountID, file.Bytes, file.Name)
}

func (c creativeMedia) waitReady(ctx context.Context, token, videoID string) error {
	for attempt := 0; attempt < videoPollAttempts; attempt++ {
		video, err := c.gateway.VideoStatus(ctx, token, videoID)
		if err != nil {
			return err
		}
		switch video.State {
		case ads.VideoReady:
			return nil
		case ads.VideoError:
			return &localFailure{err: errors.New("meta could not process the video")}
		}
		if err := c.sleep(ctx, videoPollInterval); err != nil {
			return err
		}
	}
	return ads.ErrVideoNotReady
}

func (c creativeMedia) uploadAll(ctx context.Context, workspaceID string, account *ads.AdAccount, token string, creative ads.CreativeDraft) (ads.UploadedMedia, error) {
	out := ads.UploadedMedia{ImageHashes: map[string]string{}, VideoIDs: map[string]string{}}
	for _, ref := range creative.MediaRefs() {
		id, err := c.upload(ctx, workspaceID, account, token, ref)
		if err != nil {
			return out, err
		}
		if ref.Kind != ads.MediaVideo {
			out.ImageHashes[ref.MediaID] = id
			continue
		}
		if !IsMetaMedia(ref) {
			if err := c.waitReady(ctx, token, id); err != nil {
				return out, err
			}
		}
		out.VideoIDs[ref.MediaID] = id
	}
	return out, nil
}
