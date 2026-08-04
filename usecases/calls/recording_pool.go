package calls_usecase

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vozko/domain/calls/recordings"
	"vozko/domain/media"
	"vozko/domain/messaging"
)

const (
	recordingStageExt   = ".wav"
	recordingMetaExt    = ".json"
	recordingTmpSuffix  = ".tmp"
	recordingStageDirFB = "recordings-staging"

	defaultRecordingUploadWorkers = 8
	recordingJobQueueDepth        = 256
	recordingRecoveryInterval     = 15 * time.Second
	maxRecordingUploadRetries     = 3
)

type RecordingUploadJob struct {
	CallID   string
	WAVData  []byte
	FileSize int64
	Meta     recordings.RecordingUploadEvent
}

// RecordingUploadPool is a durable, bounded uploader.
//
// A finished WAV is first persisted to an on-disk staging directory (with a
// JSON sidecar carrying its upload metadata), THEN handed to a fixed set of
// worker goroutines that read it back from disk, upload it to object storage,
// publish a lightweight event for DB persistence, and finally delete the
// staged files. A recovery loop re-enqueues any staged recording left behind
// by a crash (or dropped because the in-memory queue was full), so a recording
// is only ever lost if the staging disk itself is lost.
//
// This fixes the two unsafe properties of the previous implementation: the full
// WAV is no longer held in memory across the (slow) network upload, and an
// unbounded goroutine is no longer spawned per call.
type RecordingUploadPool struct {
	pub         messaging.MessageQueuePub
	fileStorage media.FileStorage
	logger      *log.Logger
	stagingDir  string

	jobs      chan string
	inflight  sync.Map
	workerWG  sync.WaitGroup
	closeOnce sync.Once
	closed    chan struct{}
}

func NewRecordingUploadPool(
	pub messaging.MessageQueuePub,
	fileStorage media.FileStorage,
	stagingDir string,
	workers int,
	logger *log.Logger,
) *RecordingUploadPool {
	if logger == nil {
		logger = log.Default()
	}
	if strings.TrimSpace(stagingDir) == "" {
		stagingDir = filepath.Join(os.TempDir(), recordingStageDirFB)
	}
	if workers <= 0 {
		workers = defaultRecordingUploadWorkers
	}

	p := &RecordingUploadPool{
		pub:         pub,
		fileStorage: fileStorage,
		logger:      logger,
		stagingDir:  stagingDir,
		jobs:        make(chan string, recordingJobQueueDepth),
		closed:      make(chan struct{}),
	}

	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		logger.Printf("recording-pool: CRITICAL cannot create staging dir %s: %v, recordings may be lost", stagingDir, err)
	}

	for i := 0; i < workers; i++ {
		p.workerWG.Add(1)
		go p.worker()
	}
	go p.recoveryLoop()

	logger.Printf("recording-pool: %d workers, staging=%s, durable upload -> R2 -> %s",
		workers, stagingDir, recordings.TopicRecordingUpload)
	return p
}

// Submit durably stages the recording, then queues it for upload. It returns
// after the WAV is safely on disk; the heavy upload happens on a worker.
func (p *RecordingUploadPool) Submit(job RecordingUploadJob) bool {
	if len(job.WAVData) == 0 {
		return false
	}
	if job.FileSize == 0 {
		job.FileSize = int64(len(job.WAVData))
	}

	path, err := p.stage(job)
	if err != nil {
		// Staging to disk failed (disk full / perms). Last resort: upload from
		// memory so we don't silently drop the recording.
		p.logger.Printf("recording-pool: staging failed for call %s: %v, uploading inline as a fallback", job.CallID, err)
		p.uploadAndPublish(job.CallID, job.WAVData, job.Meta)
		return false
	}
	// The durable copy is on disk now; the in-memory WAV can be reclaimed.
	p.tryEnqueue(path)
	return true
}

// stage writes the sidecar then the WAV (via a temp file + atomic rename) so a
// recovered ".wav" is always backed by a complete, readable sidecar.
func (p *RecordingUploadPool) stage(job RecordingUploadJob) (string, error) {
	base := fmt.Sprintf("%s_%d", sanitizeRecordingID(job.CallID), time.Now().UnixNano())
	wavPath := filepath.Join(p.stagingDir, base+recordingStageExt)
	metaPath := wavPath + recordingMetaExt

	job.Meta.FileSize = job.FileSize
	metaBytes, err := json.Marshal(job.Meta)
	if err != nil {
		return "", fmt.Errorf("marshal meta: %w", err)
	}
	if err := os.WriteFile(metaPath, metaBytes, 0o644); err != nil {
		return "", fmt.Errorf("write meta: %w", err)
	}

	tmpPath := wavPath + recordingTmpSuffix
	if err := os.WriteFile(tmpPath, job.WAVData, 0o644); err != nil {
		_ = os.Remove(metaPath)
		return "", fmt.Errorf("write wav tmp: %w", err)
	}
	if err := os.Rename(tmpPath, wavPath); err != nil {
		_ = os.Remove(tmpPath)
		_ = os.Remove(metaPath)
		return "", fmt.Errorf("commit wav: %w", err)
	}
	return wavPath, nil
}

// tryEnqueue hands a staged path to a worker. It dedupes against in-flight work
// (so the recovery loop and Submit can't process the same file twice) and, when
// the queue is full, leaves the file on disk for the recovery loop to pick up.
func (p *RecordingUploadPool) tryEnqueue(path string) {
	if _, loaded := p.inflight.LoadOrStore(path, struct{}{}); loaded {
		return
	}
	select {
	case p.jobs <- path:
	case <-p.closed:
		p.inflight.Delete(path)
	default:
		p.inflight.Delete(path)
	}
}

func (p *RecordingUploadPool) worker() {
	defer p.workerWG.Done()
	for {
		select {
		case <-p.closed:
			return
		case path := <-p.jobs:
			p.process(path)
			p.inflight.Delete(path)
		}
	}
}

// process uploads a staged recording and, only on success, removes it. On any
// retryable failure the staged files are left in place for the recovery loop.
func (p *RecordingUploadPool) process(wavPath string) {
	metaPath := wavPath + recordingMetaExt

	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		p.logger.Printf("recording-pool: unreadable sidecar for %s: %v, discarding orphan", wavPath, err)
		_ = os.Remove(wavPath)
		return
	}
	var meta recordings.RecordingUploadEvent
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		p.logger.Printf("recording-pool: corrupt sidecar %s: %v, discarding", metaPath, err)
		_ = os.Remove(wavPath)
		_ = os.Remove(metaPath)
		return
	}

	wavData, err := os.ReadFile(wavPath)
	if err != nil {
		p.logger.Printf("recording-pool: cannot read staged wav %s: %v, will retry", wavPath, err)
		return
	}

	if !p.uploadAndPublish(meta.CallID, wavData, meta) {
		return
	}

	_ = os.Remove(wavPath)
	_ = os.Remove(metaPath)
}

func (p *RecordingUploadPool) uploadAndPublish(callID string, wavData []byte, meta recordings.RecordingUploadEvent) bool {
	start := time.Now()
	timestamp := time.Now().Format("20060102_150405")
	recordingKey := fmt.Sprintf("recordings/%s_%s.wav", callID, timestamp)

	var lastErr error
	for attempt := 0; attempt < maxRecordingUploadRetries; attempt++ {
		if err := p.fileStorage.UploadFile(recordingKey, wavData, "audio/wav"); err != nil {
			lastErr = err
			if attempt < maxRecordingUploadRetries-1 {
				time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			}
			continue
		}
		lastErr = nil
		break
	}
	if lastErr != nil {
		p.logger.Printf("recording-pool: R2 upload failed for call %s after %d attempts: %v, staged copy retained for retry",
			callID, maxRecordingUploadRetries, lastErr)
		return false
	}

	meta.RecordingKey = recordingKey
	meta.FileSize = int64(len(wavData))
	msg, err := json.Marshal(meta)
	if err != nil {
		p.logger.Printf("recording-pool: marshal event failed for call %s: %v, staged copy retained", callID, err)
		return false
	}
	if err := p.pub.Publish(recordings.TopicRecordingUpload, msg); err != nil {
		p.logger.Printf("recording-pool: publish failed for call %s: %v, staged copy retained for retry", callID, err)
		return false
	}

	p.logger.Printf("recording-pool: call %s uploaded in %s and queued for DB save",
		callID, time.Since(start).Round(time.Millisecond))
	return true
}

// recoveryLoop re-processes staged recordings that no worker is handling: those
// left by a crash on the previous run, or dropped because the queue was full.
func (p *RecordingUploadPool) recoveryLoop() {
	p.recoverStaged()

	ticker := time.NewTicker(recordingRecoveryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.closed:
			return
		case <-ticker.C:
			p.recoverStaged()
		}
	}
}

func (p *RecordingUploadPool) recoverStaged() {
	entries, err := os.ReadDir(p.stagingDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		path := filepath.Join(p.stagingDir, name)

		// Clean up debris from a crash mid-write.
		if strings.HasSuffix(name, recordingTmpSuffix) {
			_ = os.Remove(path)
			continue
		}
		if !strings.HasSuffix(name, recordingStageExt) {
			continue
		}
		// Only recover a WAV that has a complete sidecar.
		if _, err := os.Stat(path + recordingMetaExt); err != nil {
			continue
		}
		p.tryEnqueue(path)
	}
}

func (p *RecordingUploadPool) Shutdown() {
	p.logger.Printf("recording-pool: draining workers...")
	p.closeOnce.Do(func() { close(p.closed) })
	p.workerWG.Wait()
	p.logger.Printf("recording-pool: workers drained; any remaining staged recordings resume on next start")
}

func (p *RecordingUploadPool) Pending() int {
	return len(p.jobs)
}

func sanitizeRecordingID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
