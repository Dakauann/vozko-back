package conversation_usecase

import (
	"encoding/json"
	"log"
	"strings"

	conversation_domain "vozko/domain/conversation"
)

type WhatsAppCallWebhookConsumer struct {
	registry conversation_domain.WhatsAppCallRegistry
	inbound  conversation_domain.WhatsAppInboundCallHandler
	log      *log.Logger
}

func NewWhatsAppCallWebhookConsumer(registry conversation_domain.WhatsAppCallRegistry) *WhatsAppCallWebhookConsumer {
	return &WhatsAppCallWebhookConsumer{registry: registry, log: log.Default()}
}

func (c *WhatsAppCallWebhookConsumer) SetInboundHandler(h conversation_domain.WhatsAppInboundCallHandler) {
	c.inbound = h
}

type callsWebhookEnvelope struct {
	Entry []struct {
		Changes []struct {
			Field string `json:"field"`
			Value struct {
				Metadata struct {
					DisplayPhoneNumber string `json:"display_phone_number"`
					PhoneNumberID      string `json:"phone_number_id"`
				} `json:"metadata"`
				Contacts []struct {
					Profile struct {
						Name string `json:"name"`
					} `json:"profile"`
					WaID string `json:"wa_id"`
				} `json:"contacts"`
				Calls []struct {
					ID        string `json:"id"`
					From      string `json:"from"`
					To        string `json:"to"`
					Event     string `json:"event"`
					Direction string `json:"direction"`
					Reason    string `json:"reason"`
					Status    string `json:"status"`
					Session   struct {
						SDPType string `json:"sdp_type"`
						SDP     string `json:"sdp"`
					} `json:"session"`
				} `json:"calls"`
				Statuses []struct {
					ID     string `json:"id"`
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"statuses"`
				Errors []struct {
					Code    int    `json:"code"`
					Title   string `json:"title"`
					Message string `json:"message"`
				} `json:"errors"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

func (c *WhatsAppCallWebhookConsumer) HandleCallsWebhook(payload []byte) error {
	if c == nil || c.registry == nil {
		return nil
	}
	var env callsWebhookEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		c.log.Printf("[WACallWebhook] parse error: %v", err)
		return err
	}

	for _, entry := range env.Entry {
		for _, change := range entry.Changes {
			if change.Field != "calls" {
				continue
			}
			v := change.Value
			for _, e := range v.Errors {
				c.log.Printf("[WACallWebhook] error code=%d title=%q message=%q", e.Code, e.Title, e.Message)
			}
			for _, call := range v.Calls {
				switch strings.ToLower(call.Event) {
				case "connect":
					switch {
					case strings.EqualFold(call.Session.SDPType, "answer"):

						c.registry.Deliver(conversation_domain.WhatsAppCallSignal{
							CallID:    call.ID,
							Kind:      conversation_domain.WhatsAppCallConnect,
							SDPAnswer: call.Session.SDP,
						})
					case strings.EqualFold(call.Session.SDPType, "offer"):

						if c.inbound == nil {
							c.log.Printf("[WACallWebhook] inbound call %s ignored: no inbound handler wired", call.ID)
							continue
						}
						contactName := ""
						if len(v.Contacts) > 0 {
							contactName = v.Contacts[0].Profile.Name
						}
						c.inbound.HandleInboundConnect(conversation_domain.WhatsAppInboundConnect{
							CallID:            call.ID,
							MetaPhoneNumberID: v.Metadata.PhoneNumberID,
							FromNumber:        call.From,
							ToNumber:          call.To,
							SDPOffer:          call.Session.SDP,
							ContactName:       contactName,
						})
					}
				case "terminate":
					reason := call.Reason
					if reason == "" {
						reason = call.Status
					}
					c.registry.Deliver(conversation_domain.WhatsAppCallSignal{
						CallID: call.ID,
						Kind:   conversation_domain.WhatsAppCallTerminate,
						Reason: reason,
					})
				}
			}
			for _, st := range v.Statuses {
				if !strings.EqualFold(st.Type, "call") {
					continue
				}
				kind := mapCallStatus(st.Status)
				if kind == "" {
					continue
				}
				c.registry.Deliver(conversation_domain.WhatsAppCallSignal{
					CallID: st.ID,
					Kind:   kind,
				})
			}
		}
	}
	return nil
}

func mapCallStatus(status string) conversation_domain.WhatsAppCallSignalKind {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "RINGING":
		return conversation_domain.WhatsAppCallRinging
	case "ACCEPTED":
		return conversation_domain.WhatsAppCallAccepted
	case "REJECTED":
		return conversation_domain.WhatsAppCallRejected
	default:
		return ""
	}
}

var _ conversation_domain.WhatsAppCallWebhookHandler = (*WhatsAppCallWebhookConsumer)(nil)
