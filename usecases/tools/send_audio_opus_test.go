package tools_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/media"
)

type audioClientStub struct {
	conversation.WhatsAppClient
	bytesCalls []struct {
		data     []byte
		fileName string
	}
	linkCalls []string
	bytesErr  error
}

func (c *audioClientStub) SendAudioBytes(_ context.Context, _ string, data []byte, fileName, _ string) (*conversation.SendTextMessageOutput, error) {
	c.bytesCalls = append(c.bytesCalls, struct {
		data     []byte
		fileName string
	}{data, fileName})
	if c.bytesErr != nil {
		return nil, c.bytesErr
	}
	return &conversation.SendTextMessageOutput{}, nil
}

func (c *audioClientStub) SendAudioMessage(_ context.Context, in conversation.SendAudioMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.linkCalls = append(c.linkCalls, in.AudioURL)
	return &conversation.SendTextMessageOutput{}, nil
}

func stubConvert(t *testing.T, out []byte, err error) {
	t.Helper()
	original := convertAudioToOGGOpusFn
	convertAudioToOGGOpusFn = func([]byte) ([]byte, error) { return out, err }
	t.Cleanup(func() { convertAudioToOGGOpusFn = original })
}

func TestAudioIsTranscodedBeforeSending(t *testing.T) {
	srv := oggVorbisServer(t)
	stubConvert(t, []byte("OggS-opus-bytes"), nil)

	c := &audioClientStub{}
	uc := &SendWhatsappMediaTool{}
	res, err := uc.sendAudio(context.Background(), c, "5511999999999", &media.Media{URL: srv})
	if err != nil {
		t.Fatalf("sendAudio: %v", err)
	}
	if len(c.bytesCalls) != 1 {
		t.Fatalf("SendAudioBytes called %d times, want 1 — a link lets Meta refuse the container", len(c.bytesCalls))
	}
	if string(c.bytesCalls[0].data) != "OggS-opus-bytes" {
		t.Errorf("sent %q, want the transcoded bytes", c.bytesCalls[0].data)
	}
	if len(c.linkCalls) != 0 {
		t.Errorf("fell back to the link (%v) even though the transcode worked", c.linkCalls)
	}
	if msg, _ := res.Result.(string); !strings.Contains(msg, "successfully") {
		t.Errorf("result = %v", res.Result)
	}
}

func TestAudioFallsBackToLinkWhenTranscodeFails(t *testing.T) {
	srv := oggVorbisServer(t)
	stubConvert(t, nil, errors.New("ffmpeg not found"))

	c := &audioClientStub{}
	uc := &SendWhatsappMediaTool{}
	if _, err := uc.sendAudio(context.Background(), c, "5511999999999", &media.Media{URL: srv}); err != nil {
		t.Fatalf("a failed transcode must not fail the send: %v", err)
	}
	if len(c.bytesCalls) != 0 {
		t.Error("bytes were sent despite the transcode failing")
	}
	if len(c.linkCalls) != 1 || c.linkCalls[0] != srv {
		t.Errorf("link calls = %v, want the original URL once", c.linkCalls)
	}
}

func TestAudioFallsBackToLinkWhenDownloadFails(t *testing.T) {
	stubConvert(t, []byte("unused"), nil)

	c := &audioClientStub{}
	uc := &SendWhatsappMediaTool{}
	bad := "http://127.0.0.1:1/never-there.ogg"
	if _, err := uc.sendAudio(context.Background(), c, "5511999999999", &media.Media{URL: bad}); err != nil {
		t.Fatalf("an unreachable CDN must not fail the send: %v", err)
	}
	if len(c.linkCalls) != 1 {
		t.Errorf("link calls = %v, want one fallback", c.linkCalls)
	}
}

func TestAudioFallsBackToLinkWhenByteSendFails(t *testing.T) {
	srv := oggVorbisServer(t)
	stubConvert(t, []byte("OggS-opus-bytes"), nil)

	c := &audioClientStub{bytesErr: errors.New("upload rejected")}
	uc := &SendWhatsappMediaTool{}
	if _, err := uc.sendAudio(context.Background(), c, "5511999999999", &media.Media{URL: srv}); err != nil {
		t.Fatalf("sendAudio: %v", err)
	}
	if len(c.bytesCalls) != 1 || len(c.linkCalls) != 1 {
		t.Errorf("bytes=%d link=%d, want one attempt each", len(c.bytesCalls), len(c.linkCalls))
	}
}
