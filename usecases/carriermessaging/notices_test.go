package carriermessaging_usecase

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/carriermessaging"
	"vozko/domain/notification"
	"vozko/domain/rcs"
	"vozko/domain/workspace"
)

type recordingPublisher struct {
	drafts   []notification.Draft
	resolved [][]string
}

func (p *recordingPublisher) Publish(_ context.Context, d notification.Draft) (notification.Record, error) {
	p.drafts = append(p.drafts, d)
	return notification.Record{}, nil
}

func (p *recordingPublisher) Resolve(_ context.Context, _ string, keys ...string) error {
	p.resolved = append(p.resolved, keys)
	return nil
}

func TestSenderNotices(t *testing.T) {
	pub := &recordingPublisher{}
	notices := NewNotices(pub)
	sender := rcs.Sender{ID: "s1", WorkspaceID: "ws-1", BrandName: "Loja", ContactEmail: "mkt@loja.com.br"}

	sender.Status = rcs.SenderAwaitingConfirmation
	notices.SenderStateChanged(context.Background(), sender)
	d := pub.drafts[0]
	if !d.Email || d.EpisodeKey != "carrier_sender_confirm:s1" || !strings.Contains(d.Body, "mkt@loja.com.br") || !strings.Contains(d.Body, "CONFIRMO") {
		t.Fatalf("draft = %+v", d)
	}
	if d.Audience.Permission == nil || d.Audience.Permission.Resource != workspace.ResourceCarrierMessaging || d.Audience.Permission.Action != workspace.ActionConfigure {
		t.Fatalf("audience = %+v", d.Audience)
	}

	sender.Status, sender.StatusReason = rcs.SenderRejected, "logo ilegível"
	notices.SenderStateChanged(context.Background(), sender)
	if d := pub.drafts[1]; d.Severity != notification.SeverityCritical || !strings.Contains(d.Body, "logo ilegível") {
		t.Fatalf("rejected = %+v", d)
	}

	sender.Status = rcs.SenderLaunched
	notices.SenderStateChanged(context.Background(), sender)
	if len(pub.resolved) == 0 || !strings.Contains(strings.Join(pub.resolved[len(pub.resolved)-1], ","), "carrier_sender_confirm:s1") {
		t.Fatalf("launch must close the pending episodes: %v", pub.resolved)
	}
}

func TestFloodNotice(t *testing.T) {
	pub := &recordingPublisher{}
	NewNotices(pub).ContactFlooding(context.Background(), "ws-1", carriermessaging.ChannelSMS, carriermessaging.Contact{ID: "c1", Phone: "5511999990000"}, 31)
	d := pub.drafts[0]
	if d.EpisodeKey != "carrier_flood:c1" || d.Severity != notification.SeverityWarning || !strings.Contains(d.Body, "31") || d.Source != notification.SourceCarrierFlood {
		t.Fatalf("draft = %+v", d)
	}
	if err := d.Normalize().Validate(); err != nil {
		t.Fatalf("draft invalid: %v", err)
	}
}
