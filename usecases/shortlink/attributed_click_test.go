package shortlink_usecase

import (
	"context"
	"sync"
	"testing"
	"time"

	"vozko/domain/shortlink"
)

type spyLinkClicks struct {
	mu     sync.Mutex
	tokens []string
	at     []time.Time
	spaces []string
}

func (s *spyLinkClicks) RecordLinkClick(_ context.Context, workspaceID, token string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = append(s.tokens, token)
	s.at = append(s.at, at)
	s.spaces = append(s.spaces, workspaceID)
}

func TestAnAttributedHumanClickCountsOnTheTemplateSend(t *testing.T) {
	cases := []struct {
		name        string
		attribution string
		bot         bool
		want        int
	}{
		{"a person opening a tracked button", "1.dHBsOmE", false, 1},
		{"a crawler opening a tracked button", "1.dHBsOmE", true, 0},
		{"a plain short link", "", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := &spyLinkClicks{}
			clickRepo := &fakeClickRepo{RecordFn: func(context.Context, *shortlink.Click) (bool, error) { return true, nil }}
			uc := newConsumerWith(clickRepo, &fakeShortLinkRepo{}, fakeUA{info: shortlink.DeviceInfo{IsBot: tc.bot}}, spy)
			msg := validClickMessage()
			msg.Attribution = tc.attribution
			if err := uc.process(msg); err != nil {
				t.Fatal(err)
			}
			if len(spy.tokens) != tc.want {
				t.Fatalf("recorded %v", spy.tokens)
			}
			if tc.want == 1 && (spy.tokens[0] != tc.attribution || spy.spaces[0] != "ws" || !spy.at[0].Equal(msg.OccurredAt)) {
				t.Fatalf("recorded %v %v %v", spy.tokens, spy.spaces, spy.at)
			}
		})
	}
}

func TestTheClickConsumerRefusesToStartWithoutTheLinkClickRecorder(t *testing.T) {
	uc := newConsumerWith(&fakeClickRepo{}, &fakeShortLinkRepo{}, fakeUA{}, nil)
	if err := uc.Start(); err == nil {
		t.Fatal("started without the recorder")
	}
}
