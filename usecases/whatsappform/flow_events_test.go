package whatsappform_usecase

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/notification"
	wf "vozko/domain/whatsappform"
)

type notices struct {
	published []notification.Draft
	resolved  []string
}

func (n *notices) Publish(_ context.Context, d notification.Draft) (notification.Record, error) {
	n.published = append(n.published, d)
	return notification.Record{}, nil
}

func (n *notices) Resolve(_ context.Context, workspaceID string, keys ...string) error {
	n.resolved = append(n.resolved, keys...)
	return nil
}

func flowsBody(waba, flow, value string) []byte {
	return []byte(fmt.Sprintf(`{"entry":[{"id":%q,"changes":[{"field":"flows","value":{"flow_id":%q,%s}}]}]}`, waba, flow, value))
}

func TestAStatusChangeFromMetaMovesThePublicationAndRaisesAnEpisode(t *testing.T) {
	w, detail := sender(t)
	pub := w.pubs.only(t, wabaA)
	body := flowsBody(wabaA, pub.MetaFlowID, `"event":"FLOW_STATUS_CHANGE","old_status":"PUBLISHED","new_status":"THROTTLED","message":"limitado"`)
	if err := w.svc.HandleFlowWebhook(ctx, body); err != nil {
		t.Fatal(err)
	}
	if got := w.pubs.only(t, wabaA); got.Status != wf.PublicationThrottled {
		t.Fatalf("publication = %+v", got)
	}
	if len(w.notices.published) != 1 {
		t.Fatalf("published = %+v", w.notices.published)
	}
	d := w.notices.published[0]
	if d.WorkspaceID != ws || d.EpisodeKey == "" || !strings.Contains(d.Body, "Cadastro") || d.ActionURL != "/dashboard/whatsapp-forms/"+detail.Form.ID || !d.Email ||
		d.Audience.Permission == nil || d.Audience.Permission.Action != "configure" {
		t.Fatalf("draft = %+v", d)
	}
	back := flowsBody(wabaA, pub.MetaFlowID, `"event":"FLOW_STATUS_CHANGE","old_status":"THROTTLED","new_status":"PUBLISHED"`)
	if err := w.svc.HandleFlowWebhook(ctx, back); err != nil {
		t.Fatal(err)
	}
	if got := w.pubs.only(t, wabaA); got.Status != wf.PublicationPublished || len(w.notices.resolved) == 0 {
		t.Fatalf("publication = %+v resolved = %v", got, w.notices.resolved)
	}
}

func TestAnIllegalStatusJumpIsNotApplied(t *testing.T) {
	w, _ := sender(t)
	pub := w.pubs.only(t, wabaA)
	pub.Status = wf.PublicationDeprecated
	if err := w.pubs.Save(ctx, pub); err != nil {
		t.Fatal(err)
	}
	body := flowsBody(wabaA, pub.MetaFlowID, `"event":"FLOW_STATUS_CHANGE","new_status":"PUBLISHED"`)
	if err := w.svc.HandleFlowWebhook(ctx, body); err != nil {
		t.Fatal(err)
	}
	if got := w.pubs.only(t, wabaA); got.Status != wf.PublicationDeprecated {
		t.Fatalf("a deprecated flow never comes back, got %+v", got)
	}
}

func TestHealthAlertsOpenAndCloseEpisodes(t *testing.T) {
	w, _ := sender(t)
	pub := w.pubs.only(t, wabaA)
	on := flowsBody(wabaA, pub.MetaFlowID, `"event":"CLIENT_ERROR_RATE","alert_state":"ACTIVATED","message":"erros"`)
	off := flowsBody(wabaA, pub.MetaFlowID, `"event":"CLIENT_ERROR_RATE","alert_state":"DEACTIVATED"`)
	if err := w.svc.HandleFlowWebhook(ctx, on); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.HandleFlowWebhook(ctx, off); err != nil {
		t.Fatal(err)
	}
	if len(w.notices.published) != 1 || len(w.notices.resolved) != 1 || w.notices.resolved[0] != w.notices.published[0].EpisodeKey {
		t.Fatalf("published = %+v resolved = %v", w.notices.published, w.notices.resolved)
	}
}

func TestEventsForUnknownFlowsAreIgnored(t *testing.T) {
	w, _ := sender(t)
	body := flowsBody(wabaA, "flow-from-manager", `"event":"FLOW_STATUS_CHANGE","new_status":"BLOCKED"`)
	if err := w.svc.HandleFlowWebhook(ctx, body); err != nil {
		t.Fatal(err)
	}
	if len(w.notices.published) != 0 {
		t.Fatal("a flow Vozko does not publish raises nothing")
	}
}
