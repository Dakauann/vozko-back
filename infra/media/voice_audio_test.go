package media_infra

import (
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"sync/atomic"
	"testing"

	media_domain "vozko/domain/media"
	"vozko/domain/workflow"
)

type mediaByID struct {
	media_domain.MediaRepository
	items map[string]*media_domain.Media
}

func (m mediaByID) GetMediaByID(id string) (*media_domain.Media, error) {
	if item, ok := m.items[id]; ok {
		return item, nil
	}
	return nil, errors.New("not found")
}

func wavOfSilence(sampleRate, seconds int) []byte {
	samples := sampleRate * seconds
	data := make([]byte, samples*2)
	header := make([]byte, 44)
	copy(header[0:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(36+len(data)))
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], 1)
	binary.LittleEndian.PutUint32(header[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(header[32:], 2)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(len(data)))
	return append(header, data...)
}

func audioServer(t *testing.T, body []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func TestVoiceAudioConvertsAWorkspaceFileToTelephonePCMOnce(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	server, hits := audioServer(t, wavOfSilence(16000, 1))
	loader := NewVoiceAudioLoader(mediaByID{items: map[string]*media_domain.Media{
		"greeting": {ID: "greeting", WorkspaceID: "ws1", Type: media_domain.MediaTypeAudio, URL: server.URL},
	}}, server.Client())

	for range 2 {
		pcm, err := loader.LoadPCM(context.Background(), "ws1", "greeting")
		if err != nil {
			t.Fatalf("LoadPCM: %v", err)
		}
		if len(pcm) < 15000 || len(pcm) > 17000 {
			t.Fatalf("one second at 8 kHz PCM16 is 16000 bytes, got %d", len(pcm))
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("downloaded %d times, want once", hits.Load())
	}
}

func TestVoiceAudioNeverFetchesAudioItMayNotPlay(t *testing.T) {
	server, hits := audioServer(t, wavOfSilence(8000, 1))
	loader := NewVoiceAudioLoader(mediaByID{items: map[string]*media_domain.Media{
		"theirs":  {ID: "theirs", WorkspaceID: "ws2", Type: media_domain.MediaTypeAudio, URL: server.URL},
		"picture": {ID: "picture", WorkspaceID: "ws1", Type: media_domain.MediaTypeProductImage, URL: server.URL},
	}}, server.Client())

	for _, mediaID := range []string{"theirs", "picture", "missing", ""} {
		if _, err := loader.LoadPCM(context.Background(), "ws1", mediaID); !errors.Is(err, workflow.ErrAudioNotPlayable) {
			t.Errorf("%q: err = %v, want ErrAudioNotPlayable", mediaID, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("fetched %d refused files", hits.Load())
	}
}

func TestVoiceAudioRefusesOversizedFiles(t *testing.T) {
	server, _ := audioServer(t, make([]byte, maxVoiceAudioBytes+1))
	loader := NewVoiceAudioLoader(mediaByID{items: map[string]*media_domain.Media{
		"huge": {ID: "huge", WorkspaceID: "ws1", Type: media_domain.MediaTypeAudio, URL: server.URL},
	}}, server.Client())

	if _, err := loader.LoadPCM(context.Background(), "ws1", "huge"); !errors.Is(err, workflow.ErrAudioNotPlayable) {
		t.Fatalf("err = %v, want ErrAudioNotPlayable", err)
	}
}
