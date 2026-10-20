package channelmedia

import (
	"context"
	"reflect"
	"testing"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type plainAdapter struct{ entryType shared.EntryType }

func (a plainAdapter) EntryType() shared.EntryType { return a.entryType }
func (plainAdapter) ResolveEntry(context.Context, string) (*conversation.EntryContext, error) {
	return nil, nil
}
func (plainAdapter) WindowState(context.Context, *conversation.EntryContext) (conversation.WindowState, error) {
	return conversation.WindowState{}, nil
}
func (plainAdapter) SendText(context.Context, *conversation.EntryContext, conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	return nil, nil
}
func (plainAdapter) SendMedia(context.Context, *conversation.EntryContext, conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	return nil, nil
}

type describedAdapter struct {
	plainAdapter
	caps channel.Capabilities
}

func (a describedAdapter) Capabilities() channel.Capabilities { return a.caps }

func TestEveryChannelGetsTheMediaLimitsItsAdapterDeclares(t *testing.T) {
	telegram := describedAdapter{plainAdapter{shared.EntryTypeTelegram}, channel.Capabilities{MediaLimits: map[channel.MediaKind]channel.MediaLimit{
		channel.MediaImage: {MaxBytes: 10 << 20, MIMETypes: []string{"image/png", "image/jpeg"}},
	}}}
	textOnly := describedAdapter{plainAdapter{shared.EntryTypeWebchat}, channel.Capabilities{}}
	limits := Limits{
		Adapters: conversation.NewAdapterRegistry(telegram, textOnly, plainAdapter{shared.EntryTypeFacebook}),
		Official: map[channel.MediaKind]channel.MediaLimit{channel.MediaDocument: {MaxBytes: 100 << 20}},
	}
	cases := []struct {
		entryType shared.EntryType
		want      []conversation.MediaLimit
		known     bool
	}{
		{shared.EntryTypeTelegram, []conversation.MediaLimit{{Kind: channel.MediaImage, MaxBytes: 10 << 20, MIMETypes: []string{"image/jpeg", "image/png"}}}, true},
		{shared.EntryTypeWhatsApp, []conversation.MediaLimit{{Kind: channel.MediaDocument, MaxBytes: 100 << 20, MIMETypes: []string{}}}, true},
		{shared.EntryTypeWebchat, []conversation.MediaLimit{}, true},
		{shared.EntryTypeFacebook, nil, false},
		{shared.EntryTypeInstagram, nil, false},
		{"pigeon", nil, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.entryType), func(t *testing.T) {
			got, known := limits.MediaLimits(tc.entryType)
			if known != tc.known || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("limits = %+v, %v; want %+v, %v", got, known, tc.want, tc.known)
			}
		})
	}
}

func TestWithoutSourcesNoChannelHasKnownLimits(t *testing.T) {
	for _, entryType := range []shared.EntryType{shared.EntryTypeWhatsApp, shared.EntryTypeTelegram} {
		if got, known := (Limits{}).MediaLimits(entryType); known || got != nil {
			t.Fatalf("%s = %+v, %v; want unknown", entryType, got, known)
		}
	}
}
