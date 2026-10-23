package carriercampaign_usecase

import (
	"fmt"
	"testing"
	"time"

	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/lead"
	"vozko/domain/rcs"
	"vozko/domain/sms"
	"vozko/usecases/campaignqueue"
)

func message(r cc.Recipient) campaignqueue.Message {
	return campaignqueue.Message{CampaignID: r.RunID, EntryID: r.ID, PhoneNumber: r.Phone}
}

func TestHandleOutcomes(t *testing.T) {
	nextOpen := testNow.Add(90 * time.Minute)
	cases := []struct {
		name      string
		err       error
		lead      func(*lead.Lead)
		result    campaignqueue.Result
		status    cc.RecipientStatus
		skip      cc.SkipReason
		paused    bool
		delivered bool
	}{
		{"sent", nil, nil, campaignqueue.Done, cc.RecipientSent, "", false, true},
		{"quiet hours wait for the opening", &carriermessaging.QuietHoursError{NextOpen: nextOpen}, nil, campaignqueue.RetryLater(90 * time.Minute), cc.RecipientPending, "", false, true},
		{"quiet hours opening already passed", &carriermessaging.QuietHoursError{NextOpen: testNow.Add(-time.Minute)}, nil, campaignqueue.RetryLater(minQuietDelay), cc.RecipientPending, "", false, true},
		{"quiet hours that never open pause", carriermessaging.ErrQuietHours, nil, campaignqueue.Requeue, cc.RecipientPending, "", true, true},
		{"frequency cap", fmt.Errorf("x: %w", carriermessaging.ErrFrequencyCap), nil, campaignqueue.Done, cc.RecipientSkipped, cc.SkipFrequencyCap, false, true},
		{"suppressed", carriermessaging.ErrSuppressed, nil, campaignqueue.Done, cc.RecipientSkipped, cc.SkipSuppressed, false, true},
		{"refused blocked lead", carriermessaging.ErrRecipientRefused, func(l *lead.Lead) { l.Blocked = true }, campaignqueue.Done, cc.RecipientSkipped, cc.SkipBlocked, false, true},
		{"refused opted out lead", carriermessaging.ErrRecipientRefused, func(l *lead.Lead) { now := testNow; l.OptedOutAt = &now }, campaignqueue.Done, cc.RecipientSkipped, cc.SkipOptedOut, false, true},
		{"refused for another reason", carriermessaging.ErrRecipientRefused, nil, campaignqueue.Done, cc.RecipientSkipped, cc.SkipBlocked, false, true},
		{"invalid phone", carriermessaging.ErrPhoneInvalid, nil, campaignqueue.Done, cc.RecipientSkipped, cc.SkipNoPhone, false, true},
		{"missing variable", nil, func(l *lead.Lead) { l.Name = "" }, campaignqueue.Done, cc.RecipientSkipped, cc.SkipMissingVariable, false, false},
		{"balance short", &carriermessaging.ShortfallError{MissingMicros: 5}, nil, campaignqueue.Requeue, cc.RecipientPending, "", true, true},
		{"sender cannot send", rcs.ErrSenderCannotSend, nil, campaignqueue.Requeue, cc.RecipientPending, "", true, true},
		{"route cannot send", sms.ErrRouteCannotSend, nil, campaignqueue.Requeue, cc.RecipientPending, "", true, true},
		{"price missing", carriermessaging.ErrPriceUnavailable, nil, campaignqueue.Requeue, cc.RecipientPending, "", true, true},
		{"other error retries", errBoom, nil, campaignqueue.RetryLater(retryBase), cc.RecipientPending, "", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			l := h.addLead("lead-a", 1)
			if tc.lead != nil {
				tc.lead(l)
			}
			run := h.runningRun(rcsCampaign("c1"), "run-1")
			rec := h.pendingRecipient(run, "rec-1", "lead-a", 1)
			h.carrier.err = tc.err

			got := h.svc.Handle(message(rec))
			if got != tc.result {
				t.Fatalf("result %+v, want %+v", got, tc.result)
			}
			stored := h.recipients.get("rec-1")
			if stored.Status != tc.status || stored.SkipReason != tc.skip {
				t.Fatalf("recipient %+v", stored)
			}
			if tc.status == cc.RecipientSent && stored.MessageID != "rec-1" {
				t.Fatalf("message id %q", stored.MessageID)
			}
			if (len(h.carrier.requests) > 0) != tc.delivered {
				t.Fatalf("delivered = %v", len(h.carrier.requests) > 0)
			}
			pausedRun := h.runs.get("run-1").Status == campaign.StatusPaused
			pausedCampaign := h.campaigns.get("c1").Status == cc.StatusPaused
			if pausedRun != tc.paused || pausedCampaign != tc.paused || h.queue.has("pause:run-1") != tc.paused {
				t.Fatalf("paused run %v campaign %v queue %v", pausedRun, pausedCampaign, h.queue.has("pause:run-1"))
			}
		})
	}
}

func TestHandleRetriesThenFails(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	run := h.runningRun(rcsCampaign("c1"), "run-1")
	rec := h.pendingRecipient(run, "rec-1", "lead-a", 1)
	h.carrier.err = errBoom
	delay := retryBase
	for attempt := 1; attempt < maxAttempts; attempt++ {
		if got := h.svc.Handle(message(rec)); got != campaignqueue.RetryLater(delay) {
			t.Fatalf("attempt %d: %+v, want retry in %s", attempt, got, delay)
		}
		delay = min(2*delay, retryCap)
	}
	if got := h.svc.Handle(message(rec)); got != campaignqueue.Done {
		t.Fatalf("last attempt %+v", got)
	}
	stored := h.recipients.get("rec-1")
	if stored.Status != cc.RecipientFailed || stored.ErrorCode != ErrorCodeDeliveryFailed {
		t.Fatalf("recipient %+v", stored)
	}
	if len(h.attempts.counts) != 0 {
		t.Fatalf("attempt counter left behind: %v", h.attempts.counts)
	}
}

func TestHandleBackoffIsCapped(t *testing.T) {
	if got := backoff(30); got != retryCap {
		t.Fatalf("backoff = %s", got)
	}
	if got := backoff(1); got != retryBase {
		t.Fatalf("first backoff = %s", got)
	}
}

func TestHandleFailsClosedWithoutAttemptCounter(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	run := h.runningRun(rcsCampaign("c1"), "run-1")
	rec := h.pendingRecipient(run, "rec-1", "lead-a", 1)
	h.carrier.err = errBoom
	h.attempts.err = errBoom
	if got := h.svc.Handle(message(rec)); got != campaignqueue.Done {
		t.Fatalf("result %+v", got)
	}
	if h.recipients.get("rec-1").Status != cc.RecipientFailed {
		t.Fatal("an uncountable retry must fail the recipient")
	}
}

func TestHandleSuccessClearsAttempts(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	run := h.runningRun(rcsCampaign("c1"), "run-1")
	rec := h.pendingRecipient(run, "rec-1", "lead-a", 1)
	h.carrier.err = errBoom
	h.svc.Handle(message(rec))
	h.carrier.err = nil
	if got := h.svc.Handle(message(rec)); got != campaignqueue.Done {
		t.Fatalf("result %+v", got)
	}
	if len(h.attempts.counts) != 0 {
		t.Fatalf("attempt counter left behind: %v", h.attempts.counts)
	}
}

func TestHandleDeliverRequest(t *testing.T) {
	cases := []struct {
		name      string
		c         cc.Campaign
		marketing bool
	}{
		{"marketing rcs", rcsCampaign("c1"), true},
		{"transactional rcs", func() cc.Campaign { c := rcsCampaign("c1"); c.Kind = cc.KindTransactional; return c }(), false},
		{"sms", smsCampaign("c1"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.addLead("lead-a", 1, func(l *lead.Lead) { l.Name = "Carla Dias" })
			c := tc.c
			c.Variants = append(c.Variants, c.Variants[0])
			c.Variants[0].Name, c.Variants[1].Name = "A", "B"
			if c.Channel == carriermessaging.ChannelRCS {
				c.Fallback.WhatsApp = &cc.WhatsAppFallback{BusinessPhoneID: "bp", TemplateID: "tpl", BodyBindings: []campaign.VariableBinding{{Source: campaign.BindOwnerName}}}
				c.Fallback.SMS = &cc.SMSFallback{RouteID: "route-1", Text: "Oi {{1}}", Bindings: []campaign.VariableBinding{firstNameBinding()}}
			}
			run := h.runningRun(c, "run-1")
			rec := h.pendingRecipient(run, "rec-1", "lead-a", 1)
			rec.Variant = 1
			h.recipients.put(rec)

			if got := h.svc.Handle(message(rec)); got != campaignqueue.Done {
				t.Fatalf("result %+v", got)
			}
			req := h.carrier.requests[0]
			if req.MessageID != "rec-1" || req.WorkspaceID != wsID || req.Channel != c.Channel || req.SenderID != c.SenderID ||
				req.Phone != rec.Phone || req.Lead == nil || req.Lead.ID != "lead-a" || req.RunKind != carriermessaging.RunCampaign ||
				req.Marketing != tc.marketing || req.RunID != "run-1" || req.Variant != 1 || len(req.Vars) != 1 || req.Vars[0] != "Carla" {
				t.Fatalf("request %+v", req)
			}
			if c.Channel == carriermessaging.ChannelSMS {
				if req.Text != c.Variants[1].Text || req.Fallback.Plan().Enabled() {
					t.Fatalf("sms request %+v", req)
				}
				return
			}
			if req.Content.Text != c.Variants[1].Content.Text {
				t.Fatalf("content %+v", req.Content)
			}
			wa := req.Fallback.WhatsApp
			if wa == nil || wa.ActorID != owner || wa.TemplateID != "tpl" || len(wa.BodyParams) != 1 || wa.BodyParams[0] != "Dona owner-1" {
				t.Fatalf("whatsapp fallback %+v", wa)
			}
			if req.Fallback.SMSRouteID != "route-1" || req.Fallback.SMSText != "Oi Carla" {
				t.Fatalf("sms fallback %+v", req.Fallback)
			}
		})
	}
}

func TestHandleIdempotence(t *testing.T) {
	t.Run("settled recipient", func(t *testing.T) {
		h := newHarness(t)
		h.addLead("lead-a", 1)
		run := h.runningRun(rcsCampaign("c1"), "run-1")
		rec := h.pendingRecipient(run, "rec-1", "lead-a", 1)
		rec.Status, rec.SkipReason = cc.RecipientSkipped, cc.SkipSuppressed
		h.recipients.put(rec)
		if got := h.svc.Handle(message(rec)); got != campaignqueue.Done || len(h.carrier.requests) != 0 {
			t.Fatalf("result %+v requests %d", got, len(h.carrier.requests))
		}
	})
	t.Run("message already created", func(t *testing.T) {
		h := newHarness(t)
		h.addLead("lead-a", 1)
		run := h.runningRun(rcsCampaign("c1"), "run-1")
		rec := h.pendingRecipient(run, "rec-1", "lead-a", 1)
		h.messages.byID["rec-1"] = &carriermessaging.Message{ID: "rec-1"}
		if got := h.svc.Handle(message(rec)); got != campaignqueue.Done || len(h.carrier.requests) != 0 {
			t.Fatalf("result %+v requests %d", got, len(h.carrier.requests))
		}
		if stored := h.recipients.get("rec-1"); stored.Status != cc.RecipientSent || stored.MessageID != "rec-1" {
			t.Fatalf("recipient %+v", stored)
		}
	})
}

func TestHandleRunStates(t *testing.T) {
	cases := []struct {
		name   string
		status campaign.Status
		want   campaignqueue.Result
	}{
		{"stopped run drops", campaign.StatusStopped, campaignqueue.Drop},
		{"completed run drops", campaign.StatusCompleted, campaignqueue.Drop},
		{"paused run waits", campaign.StatusPaused, campaignqueue.RetryLater(pausedRetryDelay)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.addLead("lead-a", 1)
			run := h.runningRun(rcsCampaign("c1"), "run-1")
			run.Status = tc.status
			h.runs.put(run)
			rec := h.pendingRecipient(run, "rec-1", "lead-a", 1)
			if got := h.svc.Handle(message(rec)); got != tc.want || len(h.carrier.requests) != 0 {
				t.Fatalf("result %+v requests %d", got, len(h.carrier.requests))
			}
		})
	}
}

func TestHandleUnknownRecipientDrops(t *testing.T) {
	h := newHarness(t)
	if got := h.svc.Handle(campaignqueue.Message{CampaignID: "run-1", EntryID: "nope"}); got != campaignqueue.Drop {
		t.Fatalf("result %+v", got)
	}
}

func TestHandleRecipientOfAnotherRunDrops(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	run := h.runningRun(rcsCampaign("c1"), "run-1")
	rec := h.pendingRecipient(run, "rec-1", "lead-a", 1)
	msg := message(rec)
	msg.CampaignID = "run-2"
	if got := h.svc.Handle(msg); got != campaignqueue.Drop || len(h.carrier.requests) != 0 {
		t.Fatalf("result %+v", got)
	}
}

func TestHandleVariantOutOfRangeFails(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	run := h.runningRun(rcsCampaign("c1"), "run-1")
	rec := h.pendingRecipient(run, "rec-1", "lead-a", 1)
	rec.Variant = 3
	h.recipients.put(rec)
	if got := h.svc.Handle(message(rec)); got != campaignqueue.Done || len(h.carrier.requests) != 0 {
		t.Fatalf("result %+v", got)
	}
	if stored := h.recipients.get("rec-1"); stored.Status != cc.RecipientFailed || stored.ErrorCode != ErrorCodeVariantMissing {
		t.Fatalf("recipient %+v", stored)
	}
}
