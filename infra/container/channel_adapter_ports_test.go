package container

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type bareAdapter struct{}

func (bareAdapter) EntryType() shared.EntryType { return shared.EntryTypeInstagram }
func (bareAdapter) ResolveEntry(context.Context, string) (*conversation.EntryContext, error) {
	return nil, nil
}
func (bareAdapter) WindowState(context.Context, *conversation.EntryContext) (conversation.WindowState, error) {
	return conversation.WindowState{}, nil
}
func (bareAdapter) SendText(context.Context, *conversation.EntryContext, conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	return nil, nil
}
func (bareAdapter) SendMedia(context.Context, *conversation.EntryContext, conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	return nil, nil
}

type describedAdapter struct{ bareAdapter }

func (describedAdapter) Capabilities() channel.Capabilities { return channel.Capabilities{} }

type completeAdapter struct{ describedAdapter }

func (completeAdapter) WindowStates(context.Context, []string) (map[string]conversation.WindowState, error) {
	return nil, nil
}

func TestChannelAdapterPortsAreRequiredAtBoot(t *testing.T) {
	cases := []struct {
		name    string
		adapter conversation.ChannelAdapter
		missing string
	}{
		{"no capabilities", bareAdapter{}, "capabilities"},
		{"no page window states", describedAdapter{}, "window states"},
		{"complete", completeAdapter{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := channelAdapterPortsGap(tc.adapter)
			if tc.missing == "" {
				if err != nil {
					t.Fatalf("complete adapter refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.missing) {
				t.Fatalf("err = %v, want it to name %q", err, tc.missing)
			}
		})
	}
}
