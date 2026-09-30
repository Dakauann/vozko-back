package workflow_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/media"
	"vozko/domain/workflow"
)

func TestSimulatedAudioShowsTheFileInsteadOfStreamingIt(t *testing.T) {
	var shown *media.Media
	audio := newSimVoiceAudio(audioMediaStub{items: map[string]*media.Media{
		"greeting": {ID: "greeting", WorkspaceID: "ws1", Type: media.MediaTypeAudio, URL: "https://files/greeting.mp3"},
		"picture":  {ID: "picture", WorkspaceID: "ws1", Type: media.MediaTypeProductImage, URL: "https://files/p.png"},
	}}, func(m *media.Media) { shown = m })

	pcm, err := audio.LoadPCM(context.Background(), "ws1", "greeting")
	if err != nil || len(pcm) != 0 || shown == nil || shown.URL != "https://files/greeting.mp3" {
		t.Fatalf("LoadPCM = %v, %v, shown %+v", pcm, err, shown)
	}
	for _, id := range []string{"picture", "missing"} {
		if _, err := audio.LoadPCM(context.Background(), "ws1", id); !errors.Is(err, workflow.ErrAudioNotPlayable) {
			t.Errorf("%s: err = %v, the test must fail like a real call would", id, err)
		}
	}
	if _, err := audio.LoadPCM(context.Background(), "ws2", "greeting"); !errors.Is(err, workflow.ErrAudioNotPlayable) {
		t.Fatal("another workspace's audio played in a test")
	}
}

func TestTheSimulatedCallerAnswersKeyWaitsFromThePanel(t *testing.T) {
	keys := make(chan rune, 1)
	var asked []time.Duration
	call := newSimVoiceCall(keys, make(chan struct{}), func(timeout time.Duration) { asked = append(asked, timeout) })

	if _, pressed, _ := call.NextKey(20 * time.Millisecond); pressed {
		t.Fatal("silence should time out")
	}
	if len(asked) != 1 || asked[0] != 20*time.Millisecond {
		t.Fatalf("the panel was asked %v, want once with the node timeout", asked)
	}

	keys <- '2'
	key, pressed, err := call.NextKey(8 * time.Second)
	if err != nil || !pressed || key != '2' {
		t.Fatalf("NextKey = %q %v %v", key, pressed, err)
	}
	if len(asked) != 1 {
		t.Fatal("a key typed ahead should not prompt the panel again")
	}
}

func TestAKeyTypedDuringSimulatedAudioCutsItAndIsKept(t *testing.T) {
	keys := make(chan rune, 1)
	call := newSimVoiceCall(keys, make(chan struct{}), func(time.Duration) {})
	keys <- '9'

	interrupted, err := call.Play(nil, true)
	if err != nil || !interrupted {
		t.Fatalf("Play = %v, %v", interrupted, err)
	}
	if key, pressed, _ := call.NextKey(time.Second); !pressed || key != '9' {
		t.Fatalf("NextKey = %q %v, want the key typed during the audio", key, pressed)
	}
}

func TestCancellingTheSimulationHangsUp(t *testing.T) {
	ended := make(chan struct{})
	call := newSimVoiceCall(make(chan rune), ended, func(time.Duration) {})
	close(ended)
	if _, _, err := call.NextKey(time.Second); !errors.Is(err, workflow.ErrCallEnded) {
		t.Fatalf("err = %v", err)
	}
	if _, err := call.Play(nil, true); !errors.Is(err, workflow.ErrCallEnded) {
		t.Fatalf("err = %v", err)
	}
}
