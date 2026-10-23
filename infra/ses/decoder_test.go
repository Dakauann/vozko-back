package ses

import (
	"strings"
	"testing"
	"time"

	"vozko/domain/emailsending"
)

const sampleProviderID = "EXAMPLE7c191be45-e9aedb9a-02f9-4d12-a87d-dd0099a07f8a-000000"

func mustTime(t *testing.T, raw string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestDecodeDeliveryEvents(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		check   func(t *testing.T, ev emailsending.DeliveryEvent)
	}{
		{"bounce", fixtureBounce, func(t *testing.T, ev emailsending.DeliveryEvent) {
			if ev.Kind != emailsending.EventBounce || ev.Bounce == nil || ev.Bounce.Kind != emailsending.BounceHard ||
				ev.Bounce.SubType != "General" || ev.Bounce.Diagnostic != "smtp; 550 5.1.1 user unknown" {
				t.Fatalf("bounce = %+v %+v", ev, ev.Bounce)
			}
			if !ev.OccurredAt.Equal(mustTime(t, "2017-08-05T00:41:02.669Z")) {
				t.Fatalf("occurred = %v", ev.OccurredAt)
			}
			if ev.Tags["ses:configuration-set"] != "ConfigSet" {
				t.Fatalf("tags = %v", ev.Tags)
			}
		}},
		{"complaint", fixtureComplaint, func(t *testing.T, ev emailsending.DeliveryEvent) {
			if ev.Kind != emailsending.EventComplaint || ev.Complaint == nil || ev.Complaint.FeedbackType != "abuse" {
				t.Fatalf("complaint = %+v", ev)
			}
			if ev.TenantName != "ws-abc" || !strings.HasPrefix(ev.UserAgent, "Mozilla/5.0 (Windows") {
				t.Fatalf("tenant/user agent = %q %q", ev.TenantName, ev.UserAgent)
			}
		}},
		{"delivery", fixtureDelivery, func(t *testing.T, ev emailsending.DeliveryEvent) {
			if ev.Kind != emailsending.EventDelivery || !ev.OccurredAt.Equal(mustTime(t, "2016-10-19T23:21:04.133Z")) {
				t.Fatalf("delivery = %+v", ev)
			}
			if ev.Tags["myCustomTag2"] != "myCustomTagValue2" {
				t.Fatalf("tags = %v", ev.Tags)
			}
		}},
		{"send", fixtureSend, func(t *testing.T, ev emailsending.DeliveryEvent) {
			if ev.Kind != emailsending.EventSend || !ev.OccurredAt.Equal(mustTime(t, "2016-10-14T05:02:16.645Z")) {
				t.Fatalf("send = %+v", ev)
			}
		}},
		{"reject", fixtureReject, func(t *testing.T, ev emailsending.DeliveryEvent) {
			if ev.Kind != emailsending.EventReject || len(ev.Recipients) != 1 || ev.Recipients[0] != "sender@example.com" {
				t.Fatalf("reject = %+v", ev)
			}
		}},
		{"open", fixtureOpen, func(t *testing.T, ev emailsending.DeliveryEvent) {
			if ev.Kind != emailsending.EventOpen || ev.IsBot || !strings.Contains(ev.UserAgent, "iPhone") {
				t.Fatalf("open = %+v", ev)
			}
			if !ev.OccurredAt.Equal(mustTime(t, "2017-08-09T22:00:19.652Z")) {
				t.Fatalf("occurred = %v", ev.OccurredAt)
			}
		}},
		{"click by a bot", fixtureClick, func(t *testing.T, ev emailsending.DeliveryEvent) {
			if ev.Kind != emailsending.EventClick || !ev.IsBot || ev.Link != "http://docs.aws.amazon.com/ses/latest/DeveloperGuide/send-email-smtp.html" {
				t.Fatalf("click = %+v", ev)
			}
			if ev.Tags[emailsending.MessageIDTag] != "11111111-2222-3333-4444-555555555555" {
				t.Fatalf("tags = %v", ev.Tags)
			}
		}},
		{"rendering failure", fixtureRenderingFailure, func(t *testing.T, ev emailsending.DeliveryEvent) {
			if ev.Kind != emailsending.EventRenderingFailure {
				t.Fatalf("kind = %v", ev.Kind)
			}
		}},
		{"delivery delay", fixtureDeliveryDelay, func(t *testing.T, ev emailsending.DeliveryEvent) {
			if ev.Kind != emailsending.EventDeliveryDelay || !ev.OccurredAt.Equal(mustTime(t, "2020-06-16T00:25:40.095Z")) {
				t.Fatalf("delay = %+v", ev)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := Decoder{}.Decode([]byte(tc.payload))
			if err != nil {
				t.Fatal(err)
			}
			if n.Delivery == nil || n.TenantStatus != nil || n.Finding != nil || n.Inbound != nil {
				t.Fatalf("notification = %+v", n)
			}
			if n.Delivery.ProviderMessageID != sampleProviderID {
				t.Fatalf("provider id = %q", n.Delivery.ProviderMessageID)
			}
			tc.check(t, *n.Delivery)
		})
	}
}

func TestDecodeSoftAndSuppressedBounces(t *testing.T) {
	soft := strings.Replace(fixtureBounce, `"bounceType":"Permanent"`, `"bounceType":"Transient"`, 1)
	suppressed := strings.Replace(fixtureBounce, `"bounceSubType":"General"`, `"bounceSubType":"OnTenantSuppressionList"`, 1)
	for payload, want := range map[string]emailsending.BounceKind{soft: emailsending.BounceSoft, suppressed: emailsending.BounceSuppressed} {
		n, err := Decoder{}.Decode([]byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if n.Delivery.Bounce.Kind != want {
			t.Fatalf("kind = %v, want %v", n.Delivery.Bounce.Kind, want)
		}
	}
}

func TestDecodeReceivedNotification(t *testing.T) {
	n, err := Decoder{}.Decode([]byte(fixtureReceivedS3))
	if err != nil {
		t.Fatal(err)
	}
	if n.Inbound == nil || n.Delivery != nil {
		t.Fatalf("notification = %+v", n)
	}
	in := n.Inbound
	if in.ObjectKey != "inbound/d6iitobk75ur44p8kdnnp7g2n800" || in.MessageID != "d6iitobk75ur44p8kdnnp7g2n800" {
		t.Fatalf("inbound = %+v", in)
	}
	if len(in.Recipients) != 1 || in.Recipients[0] != "reply+token@joana.envios.example.com" {
		t.Fatalf("recipients = %v", in.Recipients)
	}
	if !in.ReceivedAt.Equal(mustTime(t, "2015-09-11T20:32:33.936Z")) {
		t.Fatalf("received = %v", in.ReceivedAt)
	}
}

func TestDecodeTenantStatusEvents(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		origin  emailsending.TenantStatusOrigin
		status  emailsending.TenantSendingStatus
	}{
		{"manual pause", fixtureStatusDisabledCustomer, emailsending.OriginPlatform, emailsending.TenantDisabled},
		{"provider pause", fixtureStatus("AWS_SES", "Sending Status Disabled", "DISABLED"), emailsending.OriginProvider, emailsending.TenantDisabled},
		{"provider reinstated under the disabled type", fixtureStatus("AWS_SES", "Sending Status Disabled", "REINSTATED"), emailsending.OriginProvider, emailsending.TenantReinstated},
		{"enabled", fixtureStatus("CUSTOMER_MANAGED", "Sending Status Enabled", "ENABLED"), emailsending.OriginPlatform, emailsending.TenantEnabled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := Decoder{}.Decode([]byte(tc.payload))
			if err != nil {
				t.Fatal(err)
			}
			s := n.TenantStatus
			if s == nil || s.TenantName != "ws-abc" || s.Origin != tc.origin || s.Status != tc.status {
				t.Fatalf("status = %+v", s)
			}
			if !s.OccurredAt.Equal(mustTime(t, "2025-07-24T12:44:28Z")) || s.Cause == "" {
				t.Fatalf("status = %+v", s)
			}
		})
	}
}

func TestDecodeAdvisorFindings(t *testing.T) {
	resolved := strings.Replace(fixtureAdvisorOpen, "Status Open", "Status Resolved", 1)
	for payload, open := range map[string]bool{fixtureAdvisorOpen: true, resolved: false} {
		n, err := Decoder{}.Decode([]byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		f := n.Finding
		if f == nil || f.TenantName != "ws-abc" || f.Impact != emailsending.FindingHigh || f.Type != "BOUNCE" || f.Open != open {
			t.Fatalf("finding = %+v", f)
		}
		if !strings.HasPrefix(f.Description, "The bounce rate exceeded") {
			t.Fatalf("description = %q", f.Description)
		}
	}
}

func TestDecodeRefusesUnknownShapes(t *testing.T) {
	cases := map[string]string{
		"garbage":                 "not json",
		"empty object":            "{}",
		"subscription event":      fixtureSubscription,
		"event without id":        strings.Replace(fixtureDelivery, sampleProviderID, "", 2),
		"event without timestamp": strings.NewReplacer(`"timestamp": "2016-10-19T23:21:04.133Z"`, `"timestamp": ""`, `"timestamp": "2016-10-19T23:20:52.240Z"`, `"timestamp": ""`).Replace(fixtureDelivery),
		"identity notification":   `{"notificationType":"Bounce","mail":{"messageId":"x"}}`,
		"receipt via sns action":  strings.Replace(fixtureReceivedS3, `"type": "S3"`, `"type": "SNS"`, 1),
		"receipt outside prefix":  strings.Replace(fixtureReceivedS3, `"objectKey": "inbound/`, `"objectKey": "other/`, 1),
		"eventbridge other type":  strings.Replace(fixtureAdvisorOpen, "Advisor Recommendation Status Open", "Something Else", 1),
		"eventbridge other src":   strings.Replace(fixtureAdvisorOpen, `"source": "aws.ses"`, `"source": "aws.ec2"`, 1),
		"eventbridge no tenant":   strings.Replace(fixtureAdvisorOpen, "arn:aws:ses:us-east-1:012345678901:tenant/ws-abc/tn-123", "arn:aws:ses:us-east-1:012345678901:identity/x.com", 1),
		"unknown status":          fixtureStatus("AWS_SES", "Sending Status Disabled", "PAUSED"),
		"unknown origin":          fixtureStatus("SOMEONE", "Sending Status Disabled", "DISABLED"),
		"unknown impact":          strings.Replace(fixtureAdvisorOpen, `"impact":"HIGH"`, `"impact":"MEDIUM"`, 1),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if n, err := (Decoder{}).Decode([]byte(payload)); err == nil {
				t.Fatalf("expected error, got %+v", n)
			}
		})
	}
}

func TestDecodeReceivedVerdicts(t *testing.T) {
	cases := []struct {
		name     string
		spam     string
		virus    string
		want     emailsending.InboundVerdict
		rejected bool
	}{
		{name: "clean", spam: "PASS", virus: "PASS", want: emailsending.InboundVerdict{Spam: emailsending.VerdictPass, Virus: emailsending.VerdictPass}},
		{name: "spam", spam: "FAIL", virus: "PASS", want: emailsending.InboundVerdict{Spam: emailsending.VerdictFail, Virus: emailsending.VerdictPass}, rejected: true},
		{name: "virus", spam: "GRAY", virus: "FAIL", want: emailsending.InboundVerdict{Spam: emailsending.VerdictGray, Virus: emailsending.VerdictFail}, rejected: true},
		{name: "scan failed", spam: "PROCESSING_FAILED", virus: "PROCESSING_FAILED", want: emailsending.InboundVerdict{Spam: emailsending.VerdictProcessingFailed, Virus: emailsending.VerdictProcessingFailed}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := strings.NewReplacer(
				`"spamVerdict": {"status": "PASS"}`, `"spamVerdict": {"status": "`+tc.spam+`"}`,
				`"virusVerdict": {"status": "PASS"}`, `"virusVerdict": {"status": "`+tc.virus+`"}`,
			).Replace(fixtureReceivedS3)
			n, err := Decoder{}.Decode([]byte(payload))
			if err != nil {
				t.Fatal(err)
			}
			if n.Inbound.Verdict != tc.want || n.Inbound.Verdict.Rejected() != tc.rejected {
				t.Fatalf("verdict = %+v", n.Inbound.Verdict)
			}
		})
	}
	unscanned := strings.NewReplacer(`"spamVerdict": {"status": "PASS"},`, "", `"virusVerdict": {"status": "PASS"},`, "").Replace(fixtureReceivedS3)
	n, err := Decoder{}.Decode([]byte(unscanned))
	if err != nil || n.Inbound.Verdict != (emailsending.InboundVerdict{}) || n.Inbound.Verdict.Rejected() {
		t.Fatalf("a receipt without verdicts = %+v, %v", n.Inbound, err)
	}
}

func TestDecodeRecognizesTheEventTopicValidationNotice(t *testing.T) {
	n, err := (Decoder{}).Decode([]byte("Successfully validated SNS topic for Amazon SES event publishing."))
	if err != nil {
		t.Fatal(err)
	}
	if n.Notice == nil || n.Notice.Kind != emailsending.NoticeTopicValidated || n.Delivery != nil || n.Inbound != nil {
		t.Fatalf("notification = %+v", n)
	}
	for _, near := range []string{
		"Successfully validated SNS topic for Amazon SES event publishing",
		" Successfully validated SNS topic for Amazon SES event publishing.",
		"Successfully validated SNS topic for Amazon SES event publishing. Also something else",
		"successfully validated sns topic for amazon ses event publishing.",
	} {
		if n, err := (Decoder{}).Decode([]byte(near)); err == nil {
			t.Errorf("%q decoded as %+v, want the usual refusal", near, n)
		}
	}
}

func TestDecodeRecognizesTheReceiptRuleSetupNotification(t *testing.T) {
	n, err := (Decoder{}).Decode([]byte(fixtureReceiptSetup))
	if err != nil {
		t.Fatal(err)
	}
	if n.Notice == nil || n.Notice.Kind != emailsending.NoticeReceiptSetup || n.Inbound != nil {
		t.Fatalf("notification = %+v", n)
	}
	near := map[string]string{
		"another message id": strings.Replace(fixtureReceiptSetup, `"messageId": "AMAZON_SES_SETUP_NOTIFICATION"`, `"messageId": "d6iitobk75ur44p8kdnnp7g2n800"`, 1),
		"a real recipient":   strings.Replace(fixtureReceiptSetup, `"recipients": ["recipient@example.com"]`, `"recipients": ["reply+token@joana.envios.example.com"]`, 1),
		"an extra recipient": strings.Replace(fixtureReceiptSetup, `"destination": ["recipient@example.com"]`, `"destination": ["recipient@example.com", "reply@joana.envios.example.com"]`, 1),
	}
	for name, payload := range near {
		n, err := (Decoder{}).Decode([]byte(payload))
		if err == nil && n.Notice != nil {
			t.Errorf("%s was taken for the setup notification: %+v", name, n)
		}
	}
}
