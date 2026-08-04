package calls_usecase

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/calls/recordings"
)

func validWAV() []byte {
	return buildWAVBuffer(make([]byte, 16000), recorderSampleRate, recorderChannels, recorderBitDepth)
}

func countStaged(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), recordingStageExt) {
			n++
		}
	}
	return n
}

func waitFor(t *testing.T, cond func() bool, d time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s: %s", d, msg)
}

type failingStorage struct {
	mu       sync.Mutex
	calls    int
	attempts chan struct{}
}

func newFailingStorage() *failingStorage {
	return &failingStorage{attempts: make(chan struct{}, 16)}
}

func (s *failingStorage) UploadFile(key string, data []byte, _ string) error {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	select {
	case s.attempts <- struct{}{}:
	default:
	}
	return fmt.Errorf("storage down")
}
func (s *failingStorage) GetFileURL(key string) string { return "" }

func TestRecordingUploadPool_StagesUploadsAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	storage := newCapturingStorage()
	pool := NewRecordingUploadPool(noopPub{}, storage, dir, 2, nil)
	defer pool.Shutdown()

	pool.Submit(RecordingUploadJob{
		CallID:  "wa-call-clean",
		WAVData: validWAV(),
		Meta:    recordings.RecordingUploadEvent{CallID: "wa-call-clean", WorkspaceID: "ws-1"},
	})

	select {
	case <-storage.uploaded:
	case <-time.After(5 * time.Second):
		t.Fatal("recording was never uploaded")
	}

	storage.mu.Lock()
	key := storage.key
	storage.mu.Unlock()
	if !strings.HasPrefix(key, "recordings/wa-call-clean_") || !strings.HasSuffix(key, ".wav") {
		t.Fatalf("unexpected R2 key: %s", key)
	}

	// Staged files are removed only after a successful upload+publish.
	waitFor(t, func() bool { return countStaged(dir) == 0 }, 3*time.Second, "staging dir not cleaned up")
}

func TestRecordingUploadPool_RecoversStagedAfterCrash(t *testing.T) {
	dir := t.TempDir()

	// Simulate a recording staged by a previous process that then crashed:
	// the WAV + sidecar are on disk but were never uploaded.
	base := filepath.Join(dir, "wa-in-orphan_123")
	wavPath := base + recordingStageExt
	if err := os.WriteFile(wavPath, validWAV(), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(recordings.RecordingUploadEvent{CallID: "wa-in-orphan", WorkspaceID: "ws-9"})
	if err := os.WriteFile(wavPath+recordingMetaExt, meta, 0o644); err != nil {
		t.Fatal(err)
	}

	storage := newCapturingStorage()
	pool := NewRecordingUploadPool(noopPub{}, storage, dir, 2, nil)
	defer pool.Shutdown()

	select {
	case <-storage.uploaded:
	case <-time.After(5 * time.Second):
		t.Fatal("crash-staged recording was never recovered/uploaded")
	}

	storage.mu.Lock()
	key := storage.key
	storage.mu.Unlock()
	if !strings.HasPrefix(key, "recordings/wa-in-orphan_") {
		t.Fatalf("unexpected recovered key: %s", key)
	}
	waitFor(t, func() bool { return countStaged(dir) == 0 }, 3*time.Second, "recovered staging not cleaned up")
}

func TestRecordingUploadPool_RetainsStagedOnUploadFailure(t *testing.T) {
	dir := t.TempDir()
	storage := newFailingStorage()
	pool := NewRecordingUploadPool(noopPub{}, storage, dir, 1, nil)
	defer pool.Shutdown()

	pool.Submit(RecordingUploadJob{
		CallID:  "wa-call-fail",
		WAVData: validWAV(),
		Meta:    recordings.RecordingUploadEvent{CallID: "wa-call-fail", WorkspaceID: "ws-2"},
	})

	// Wait for the upload to be attempted (and fail through its retries).
	select {
	case <-storage.attempts:
	case <-time.After(5 * time.Second):
		t.Fatal("upload was never attempted")
	}

	// The staged copy must survive a failed upload so recovery can retry it.
	waitFor(t, func() bool { return countStaged(dir) == 1 }, 3*time.Second, "staged recording was dropped after upload failure")
}
