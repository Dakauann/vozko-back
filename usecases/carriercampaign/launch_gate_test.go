package carriercampaign_usecase

import (
	"context"
	"errors"
	"slices"
	"testing"

	"vozko/domain/carriermessaging"
)

func TestCampaignsOfAChannelNotLaunchedAreRefused(t *testing.T) {
	h := newHarness(t)
	h.svc.launched = carriermessaging.Channel.Launched
	if _, err := h.svc.Create(context.Background(), wsID, Actor{UserID: owner}, draftOf(rcsCampaign(""))); !errors.Is(err, carriermessaging.ErrChannelNotLaunched) {
		t.Fatalf("create RCS = %v", err)
	}
	if _, err := h.svc.Create(context.Background(), wsID, Actor{UserID: owner}, draftOf(smsCampaign(""))); (err == nil) != carriermessaging.ChannelSMS.Launched() {
		t.Fatalf("create SMS = %v while SMS launched is %v", err, carriermessaging.ChannelSMS.Launched())
	}
	h.addLead("lead-a", 1)
	h.campaigns.put(rcsCampaign("c1"))
	if _, err := h.svc.Launch(context.Background(), wsID, "c1", Actor{UserID: owner}); !errors.Is(err, carriermessaging.ErrChannelNotLaunched) {
		t.Fatalf("launch RCS = %v", err)
	}
	if len(h.runs.all()) != 0 {
		t.Fatal("a refused launch starts no run")
	}
}

func TestNewUsesTheRealLaunchRule(t *testing.T) {
	svc, err := New(newHarness(t).svc.deps)
	if err != nil {
		t.Fatal(err)
	}
	if svc.launched(carriermessaging.ChannelRCS) != carriermessaging.ChannelRCS.Launched() || svc.launched(carriermessaging.ChannelSMS) != carriermessaging.ChannelSMS.Launched() {
		t.Fatal("production services follow carriermessaging.Channel.Launched")
	}
}

func TestAChannelNotLaunchedPausesARun(t *testing.T) {
	if !slices.Contains(pausingErrors, carriermessaging.ErrChannelNotLaunched) {
		t.Fatal("a scheduled run of a channel that is not launched pauses instead of failing every recipient")
	}
}
