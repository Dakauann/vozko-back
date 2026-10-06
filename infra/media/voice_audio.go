package media_infra

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	media_domain "vozko/domain/media"
	"vozko/domain/voip"
	"vozko/domain/workflow"
)

const (
	maxVoiceAudioBytes   = 20 << 20
	maxCachedVoiceAudios = 64
)

type cachedVoiceAudio struct {
	url string
	pcm []byte
}

type VoiceAudioLoader struct {
	media  media_domain.MediaRepository
	client *http.Client

	mu    sync.Mutex
	cache map[string]cachedVoiceAudio
	order []string
}

var _ workflow.VoiceAudio = (*VoiceAudioLoader)(nil)

func NewVoiceAudioLoader(media media_domain.MediaRepository, client *http.Client) *VoiceAudioLoader {
	return &VoiceAudioLoader{media: media, client: client, cache: map[string]cachedVoiceAudio{}}
}

func (l *VoiceAudioLoader) LoadPCM(ctx context.Context, workspaceID, mediaID string) ([]byte, error) {
	m, err := l.playable(workspaceID, mediaID)
	if err != nil {
		return nil, err
	}
	if pcm, ok := l.cached(m); ok {
		return pcm, nil
	}
	original, err := l.download(ctx, m.URL)
	if err != nil {
		return nil, err
	}
	pcm, err := ConvertToPCM(original, voip.PCMSampleRate)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", workflow.ErrAudioNotPlayable, err)
	}
	l.remember(m, pcm)
	return pcm, nil
}

func (l *VoiceAudioLoader) playable(workspaceID, mediaID string) (*media_domain.Media, error) {
	mediaID = strings.TrimSpace(mediaID)
	if mediaID == "" {
		return nil, workflow.ErrAudioNotPlayable
	}
	m, err := l.media.GetMediaByID(mediaID)
	if err != nil || !m.PlayableOnCallFor(workspaceID) {
		return nil, workflow.ErrAudioNotPlayable
	}
	return m, nil
}

func (l *VoiceAudioLoader) download(ctx context.Context, url string) ([]byte, error) {
	body, err := downloadBounded(ctx, l.client, url, maxVoiceAudioBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", workflow.ErrAudioNotPlayable, err)
	}
	return body, nil
}

func (l *VoiceAudioLoader) cached(m *media_domain.Media) ([]byte, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.cache[m.ID]
	if !ok || entry.url != m.URL {
		return nil, false
	}
	return entry.pcm, true
}

func (l *VoiceAudioLoader) remember(m *media_domain.Media, pcm []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.cache[m.ID]; !exists {
		l.order = append(l.order, m.ID)
	}
	l.cache[m.ID] = cachedVoiceAudio{url: m.URL, pcm: pcm}
	for len(l.order) > maxCachedVoiceAudios {
		delete(l.cache, l.order[0])
		l.order = l.order[1:]
	}
}
