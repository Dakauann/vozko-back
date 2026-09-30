package sip_trunk_usecase

import (
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/voip"
	"vozko/domain/workflow"
)

type recordingLine struct {
	mu     sync.Mutex
	frames [][]byte
	done   chan struct{}
	onSend func(n int)
}

func newRecordingLine() *recordingLine {
	return &recordingLine{done: make(chan struct{})}
}

func (l *recordingLine) SendAudio(pcm []byte) error {
	l.mu.Lock()
	l.frames = append(l.frames, append([]byte(nil), pcm...))
	n := len(l.frames)
	l.mu.Unlock()
	if l.onSend != nil {
		l.onSend(n)
	}
	return nil
}

func (l *recordingLine) Done() <-chan struct{} { return l.done }

func (l *recordingLine) sent() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.frames)
}

func pcmFrames(n int) []byte {
	return make([]byte, n*voip.PCMFrameBytes)
}

func TestPlaybackGoesOutInPacedTwentyMillisecondFrames(t *testing.T) {
	line := newRecordingLine()
	call := newLiveVoiceCall(line, make(chan rune))

	start := time.Now()
	interrupted, err := call.Play(pcmFrames(5), true)
	if err != nil || interrupted {
		t.Fatalf("Play = %v, %v", interrupted, err)
	}
	if line.sent() != 5 {
		t.Fatalf("sent %d frames, want 5", line.sent())
	}
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Fatalf("5 frames took %v, want real time pacing", elapsed)
	}
}

func TestAKeyPressCutsPlaybackAndIsKeptForTheNextWait(t *testing.T) {
	line := newRecordingLine()
	keys := make(chan rune, 1)
	line.onSend = func(n int) {
		if n == 2 {
			keys <- '3'
		}
	}
	call := newLiveVoiceCall(line, keys)

	interrupted, err := call.Play(pcmFrames(50), true)
	if err != nil || !interrupted {
		t.Fatalf("Play = %v, %v, want interrupted", interrupted, err)
	}
	if line.sent() >= 50 {
		t.Fatal("playback was not cut short")
	}
	key, pressed, err := call.NextKey(time.Second)
	if err != nil || !pressed || key != '3' {
		t.Fatalf("NextKey = %q %v %v, want the key that cut the audio", key, pressed, err)
	}
}

func TestKeysPressedDuringAnUninterruptibleAudioWaitForTheNextPrompt(t *testing.T) {
	line := newRecordingLine()
	keys := make(chan rune, 1)
	line.onSend = func(n int) {
		if n == 1 {
			keys <- '7'
		}
	}
	call := newLiveVoiceCall(line, keys)

	interrupted, err := call.Play(pcmFrames(4), false)
	if err != nil || interrupted || line.sent() != 4 {
		t.Fatalf("Play = %v, %v, sent %d; want the whole audio", interrupted, err, line.sent())
	}
	if key, pressed, _ := call.NextKey(time.Second); !pressed || key != '7' {
		t.Fatalf("NextKey = %q %v, want the key typed ahead", key, pressed)
	}
}

func TestWaitingForAKeyTimesOutOrEndsWithTheCall(t *testing.T) {
	line := newRecordingLine()
	call := newLiveVoiceCall(line, make(chan rune))

	if _, pressed, err := call.NextKey(30 * time.Millisecond); pressed || err != nil {
		t.Fatalf("silence: pressed=%v err=%v", pressed, err)
	}

	close(line.done)
	if _, _, err := call.NextKey(time.Second); !errors.Is(err, workflow.ErrCallEnded) {
		t.Fatalf("NextKey after hang up err = %v", err)
	}
	if _, err := call.Play(pcmFrames(3), true); !errors.Is(err, workflow.ErrCallEnded) {
		t.Fatalf("Play after hang up err = %v", err)
	}
}
