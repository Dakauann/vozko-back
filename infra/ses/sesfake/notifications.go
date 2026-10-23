package sesfake

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
)

const (
	stampLayout   = "2006-01-02T15:04:05.000Z"
	tenantNameTag = "ses:tenant-name"
	inboundBucket = "vozko-inbound"
	paddingLine   = "Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod.\r\n"
)

type mailPart struct {
	Timestamp   string              `json:"timestamp"`
	MessageID   string              `json:"messageId"`
	Destination []string            `json:"destination"`
	Tags        map[string][]string `json:"tags,omitempty"`
}

type recipientPart struct {
	EmailAddress   string `json:"emailAddress"`
	DiagnosticCode string `json:"diagnosticCode,omitempty"`
}

func stamp(at time.Time) string { return at.UTC().Format(stampLayout) }

func mailOf(s Sent, at time.Time) mailPart {
	tags := map[string][]string{tenantNameTag: {s.Message.TenantName}}
	for k, v := range s.Message.Tags {
		tags[k] = []string{v}
	}
	return mailPart{Timestamp: stamp(s.At), MessageID: s.ProviderMessageID, Destination: []string{string(s.Message.To)}, Tags: tags}
}

func encode(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("sesfake: %v", err))
	}
	return raw
}

func Delivery(s Sent, at time.Time) []byte {
	return encode(map[string]any{
		"eventType": "Delivery",
		"mail":      mailOf(s, at),
		"delivery":  map[string]any{"timestamp": stamp(at), "recipients": []string{string(s.Message.To)}},
	})
}

func HardBounce(s Sent, at time.Time) []byte {
	return encode(map[string]any{
		"eventType": "Bounce",
		"mail":      mailOf(s, at),
		"bounce": map[string]any{
			"bounceType":        "Permanent",
			"bounceSubType":     "General",
			"bouncedRecipients": []recipientPart{{EmailAddress: string(s.Message.To), DiagnosticCode: "smtp; 550 5.1.1 user unknown"}},
			"timestamp":         stamp(at),
		},
	})
}

func Complaint(s Sent, at time.Time) []byte {
	return encode(map[string]any{
		"eventType": "Complaint",
		"mail":      mailOf(s, at),
		"complaint": map[string]any{
			"complainedRecipients":  []recipientPart{{EmailAddress: string(s.Message.To)}},
			"timestamp":             stamp(at),
			"complaintFeedbackType": "abuse",
		},
	})
}

type Inbound struct {
	MessageID string
	From      string
	To        []emailaddress.Address
	Subject   string
	HeaderID  string
	InReplyTo string
	Text      string
	SizeBytes int
	SenderIP  netip.Addr
	Spam      es.Verdict
	Virus     es.Verdict
	At        time.Time
}

type Received struct {
	Accepted     bool
	Notification []byte
	ObjectKey    string
	MessageID    string
	RawBytes     int
}

func (a *Account) Receive(in Inbound) Received {
	accepted := a.acceptedRecipients(in)
	if len(accepted) == 0 {
		return Received{}
	}
	in.To = accepted
	if in.MessageID == "" {
		in.MessageID = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	if in.At.IsZero() {
		in.At = time.Now().UTC()
	}
	if in.HeaderID == "" {
		in.HeaderID = "<" + uuid.NewString() + "@mail.cliente.example>"
	}
	raw := rawMessage(in)
	key := "inbound/" + in.MessageID
	a.storeRaw(key, raw)
	recipients := make([]string, len(in.To))
	for i, to := range in.To {
		recipients[i] = string(to)
	}
	notification := encode(map[string]any{
		"notificationType": "Received",
		"mail":             mailPart{Timestamp: stamp(in.At), MessageID: in.MessageID, Destination: recipients},
		"receipt": map[string]any{
			"timestamp":    stamp(in.At),
			"recipients":   recipients,
			"spamVerdict":  map[string]string{"status": verdictOr(in.Spam)},
			"virusVerdict": map[string]string{"status": verdictOr(in.Virus)},
			"action":       map[string]string{"type": "S3", "bucketName": inboundBucket, "objectKey": key},
		},
	})
	return Received{Accepted: true, Notification: notification, ObjectKey: key, MessageID: in.MessageID, RawBytes: len(raw)}
}

func (a *Account) acceptedRecipients(in Inbound) []emailaddress.Address {
	a.mu.Lock()
	defer a.mu.Unlock()
	if until, blocked := a.filters[in.SenderIP]; blocked && until.After(time.Now()) {
		return nil
	}
	var out []emailaddress.Address
	for _, to := range in.To {
		local, domain, ok := strings.Cut(string(to), "@")
		if !ok {
			continue
		}
		base, _, _ := strings.Cut(local, "+")
		if a.conditions[domain][base+"@"+domain] {
			out = append(out, to)
		}
	}
	return out
}

func verdictOr(v es.Verdict) string {
	if v == "" {
		return string(es.VerdictPass)
	}
	return string(v)
}

func rawMessage(in Inbound) []byte {
	var b strings.Builder
	if in.SenderIP.IsValid() {
		fmt.Fprintf(&b, "Received: from mail.cliente.example (mail.cliente.example [%s]) by inbound-smtp.%s.amazonaws.com with SMTP id %s; %s\r\n",
			in.SenderIP, Region, in.MessageID, in.At.Format(time.RFC1123Z))
	}
	to := make([]string, len(in.To))
	for i, address := range in.To {
		to[i] = string(address)
	}
	fmt.Fprintf(&b, "From: %s\r\n", in.From)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", in.Subject)
	fmt.Fprintf(&b, "Message-ID: %s\r\n", in.HeaderID)
	if in.InReplyTo != "" {
		fmt.Fprintf(&b, "In-Reply-To: %s\r\n", in.InReplyTo)
	}
	fmt.Fprintf(&b, "Date: %s\r\n", in.At.Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n")
	b.WriteString(in.Text)
	b.WriteString("\r\n")
	for b.Len()+len(paddingLine) <= in.SizeBytes {
		b.WriteString(paddingLine)
	}
	return []byte(b.String())
}
