package calls_usecase

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"vozko/domain/calls/recordings"
)

const (
	recorderSampleRate = 8000
	recorderChannels   = 1
	recorderBitDepth   = 16
	crossfadeSamples   = 16

	recorderFlushBuf = 128 * 1024

	recorderEventHeaderSize = 1 + 8 + 4
)

type AudioSource int

const (
	AudioSourceCaller AudioSource = iota
	AudioSourceRemote
)

type audioEvent struct {
	timestamp time.Time
	source    AudioSource
	pcm       []byte
}

type CallRecorder struct {
	mu          sync.Mutex
	callID      string
	workspaceID string
	crmEntryID  string
	leadID      string
	startTime   time.Time
	closed      bool

	tmpFile *os.File
	tmpBuf  *bufio.Writer

	currentRemoteSessionID int
	remoteSessionCancelled bool

	callerSamplePos int64

	remoteSessionStart   time.Time
	remoteSessionStarted bool
	remoteSamplePos      int64
}

func NewCallRecorder(callID string, workspaceID string, crmEntryID string, leadID string) *CallRecorder {
	tmpFile, err := os.CreateTemp("", "rec-"+callID+"-*")
	if err != nil {
		log.Printf("CallRecorder: failed to create temp file, falling back to in-memory: %v", err)
		return nil
	}

	return &CallRecorder{
		callID:      callID,
		workspaceID: workspaceID,
		crmEntryID:  crmEntryID,
		leadID:      leadID,
		startTime:   time.Now(),
		tmpFile:     tmpFile,
		tmpBuf:      bufio.NewWriterSize(tmpFile, recorderFlushBuf),
	}
}

func (r *CallRecorder) writeEvent(source AudioSource, ts time.Time, pcm []byte) {
	var hdr [recorderEventHeaderSize]byte
	hdr[0] = byte(source)
	binary.LittleEndian.PutUint64(hdr[1:9], uint64(ts.UnixNano()))
	binary.LittleEndian.PutUint32(hdr[9:13], uint32(len(pcm)))
	_, _ = r.tmpBuf.Write(hdr[:])
	_, _ = r.tmpBuf.Write(pcm)
}

func (r *CallRecorder) RecordCallerPCM(pcm []byte) {
	if r == nil || len(pcm) == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}

	numSamples := int64(len(pcm) / 2)
	sampleTime := time.Duration(r.callerSamplePos) * time.Second / time.Duration(recorderSampleRate)
	ts := r.startTime.Add(sampleTime)
	r.callerSamplePos += numSamples

	r.writeEvent(AudioSourceCaller, ts, pcm)
}

func (r *CallRecorder) RecordRemotePCM(pcm []byte, sampleRate int) {
	if r == nil || len(pcm) == 0 {
		return
	}

	if sampleRate != recorderSampleRate {
		log.Printf("CallRecorder: rejecting remote-leg PCM at %dHz, expected %dHz, audio dropped", sampleRate, recorderSampleRate)
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}

	if r.remoteSessionCancelled {
		return
	}

	if len(pcm) == 0 {
		return
	}

	if !r.remoteSessionStarted {
		r.remoteSessionStart = time.Now()
		r.remoteSessionStarted = true
	}

	numSamples := int64(len(pcm) / 2)
	sampleTime := time.Duration(r.remoteSamplePos) * time.Second / time.Duration(recorderSampleRate)
	ts := r.remoteSessionStart.Add(sampleTime)
	r.remoteSamplePos += numSamples

	r.writeEvent(AudioSourceRemote, ts, pcm)
}

func (r *CallRecorder) StartRemoteSession() int {
	if r == nil {
		return 0
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.currentRemoteSessionID++
	r.remoteSessionCancelled = false
	r.remoteSessionStarted = false
	r.remoteSamplePos = 0
	return r.currentRemoteSessionID
}

func (r *CallRecorder) CancelRemoteSession() {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.remoteSessionCancelled = true
}

func (r *CallRecorder) Finish(pool *RecordingUploadPool) {
	if r == nil || pool == nil {
		return
	}
	wavData, duration, err := r.Close()
	if err != nil || len(wavData) == 0 {
		return
	}
	pool.Submit(RecordingUploadJob{
		CallID:   r.callID,
		WAVData:  wavData,
		FileSize: int64(len(wavData)),
		Meta: recordings.RecordingUploadEvent{
			CallID:      r.callID,
			WorkspaceID: r.workspaceID,
			EntryID:     r.crmEntryID,
			LeadID:      r.leadID,
			CallStart:   r.startTime,
			CallEnd:     r.startTime.Add(duration),
		},
	})
}

func (r *CallRecorder) Close() (wavData []byte, duration time.Duration, err error) {
	if r == nil {
		return nil, 0, nil
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, 0, nil
	}
	r.closed = true
	startTime := r.startTime
	tmpFile := r.tmpFile
	tmpBuf := r.tmpBuf
	r.tmpFile = nil
	r.tmpBuf = nil
	r.mu.Unlock()

	if tmpFile == nil {
		return nil, 0, nil
	}

	if err := tmpBuf.Flush(); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return nil, 0, fmt.Errorf("recorder flush: %w", err)
	}

	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)
	defer tmpFile.Close()

	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return nil, 0, fmt.Errorf("recorder seek: %w", err)
	}

	events, err := r.readEventsFromFile(tmpFile)
	if err != nil {
		return nil, 0, fmt.Errorf("recorder read: %w", err)
	}

	if len(events) == 0 {
		return nil, 0, nil
	}

	monoData, totalDuration := r.buildMonoTimeline(events, startTime)
	if len(monoData) == 0 {
		return nil, 0, nil
	}

	return buildWAVBuffer(monoData, recorderSampleRate, recorderChannels, recorderBitDepth), totalDuration, nil
}

func (r *CallRecorder) readEventsFromFile(f *os.File) ([]audioEvent, error) {
	reader := bufio.NewReaderSize(f, recorderFlushBuf)
	var events []audioEvent
	var hdr [recorderEventHeaderSize]byte

	for {
		if _, err := io.ReadFull(reader, hdr[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, err
		}

		source := AudioSource(hdr[0])
		tsNano := int64(binary.LittleEndian.Uint64(hdr[1:9]))
		pcmLen := int(binary.LittleEndian.Uint32(hdr[9:13]))

		pcm := make([]byte, pcmLen)
		if _, err := io.ReadFull(reader, pcm); err != nil {
			return nil, err
		}

		events = append(events, audioEvent{
			timestamp: time.Unix(0, tsNano),
			source:    source,
			pcm:       pcm,
		})
	}

	return events, nil
}

func (r *CallRecorder) buildMonoTimeline(events []audioEvent, startTime time.Time) ([]byte, time.Duration) {
	if len(events) == 0 {
		return nil, 0
	}

	var latestEnd time.Time
	for _, e := range events {
		numSamples := len(e.pcm) / 2
		audioDuration := time.Duration(numSamples) * time.Second / time.Duration(recorderSampleRate)
		eventEnd := e.timestamp.Add(audioDuration)
		if eventEnd.After(latestEnd) {
			latestEnd = eventEnd
		}
	}

	totalDuration := latestEnd.Sub(startTime)
	if totalDuration <= 0 {
		return nil, 0
	}

	totalSamples := int(totalDuration.Nanoseconds()*int64(recorderSampleRate)/1e9) + 1

	monoData := make([]byte, totalSamples*2)

	for _, e := range events {
		numSamples := len(e.pcm) / 2
		if numSamples == 0 {
			continue
		}

		elapsedFromStart := e.timestamp.Sub(startTime)
		sampleOffset := int(elapsedFromStart.Nanoseconds() * int64(recorderSampleRate) / 1e9)

		for i := 0; i < numSamples; i++ {
			targetSample := sampleOffset + i
			if targetSample < 0 || targetSample >= totalSamples {
				continue
			}

			sampleValue := int16(binary.LittleEndian.Uint16(e.pcm[i*2:]))

			monoOffset := targetSample * 2

			existingSample := int16(binary.LittleEndian.Uint16(monoData[monoOffset:]))
			mixedSample := mixSamples(existingSample, sampleValue)
			binary.LittleEndian.PutUint16(monoData[monoOffset:], uint16(mixedSample))
		}
	}

	return monoData, totalDuration
}

func mixSamples(a, b int16) int16 {
	sum := int32(a) + int32(b)
	if sum > 32767 {
		return 32767
	}
	if sum < -32768 {
		return -32768
	}
	return int16(sum)
}

func buildWAVBuffer(pcmData []byte, sampleRate, channels, bitDepth int) []byte {
	header := make([]byte, 44)
	dataSize := len(pcmData)
	fileSize := 36 + dataSize

	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(fileSize))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(sampleRate*channels*bitDepth/8))
	binary.LittleEndian.PutUint16(header[32:34], uint16(channels*bitDepth/8))
	binary.LittleEndian.PutUint16(header[34:36], uint16(bitDepth))
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataSize))

	return append(header, pcmData...)
}

func writeWAVFile(path string, pcmData []byte, sampleRate, channels int) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	header := make([]byte, 44)
	dataSize := len(pcmData)
	fileSize := 36 + dataSize

	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(fileSize))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(sampleRate*channels*2))
	binary.LittleEndian.PutUint16(header[32:34], uint16(channels*2))
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataSize))

	if _, err := file.Write(header); err != nil {
		return err
	}
	if _, err := file.Write(pcmData); err != nil {
		return err
	}

	return nil
}
