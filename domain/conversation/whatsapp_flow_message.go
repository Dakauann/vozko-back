package conversation

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

const (
	WhatsAppFlowCTAMaxLen      = 30
	WhatsAppFlowMessageVersion = "3"
)

var (
	ErrWhatsAppFlowRequired      = errors.New("whatsapp flow id is required")
	ErrWhatsAppFlowTokenRequired = errors.New("whatsapp flow token is required")
	ErrWhatsAppFlowCopyInvalid   = errors.New("whatsapp flow message text is invalid")
)

type SendFlowMessageInput struct {
	To        string
	FlowID    string
	FlowToken string
	CTA       string
	Header    string
	Body      string
	Footer    string
	Draft     bool
}

func (in SendFlowMessageInput) Validate() error {
	switch {
	case strings.TrimSpace(in.To) == "":
		return ErrWhatsAppRecipientRequired
	case strings.TrimSpace(in.FlowID) == "":
		return ErrWhatsAppFlowRequired
	case strings.TrimSpace(in.FlowToken) == "":
		return ErrWhatsAppFlowTokenRequired
	case strings.TrimSpace(in.Body) == "":
		return ErrWhatsAppBodyRequired
	}
	return ValidateFlowCopy(in.Header, in.Body, in.Footer, in.CTA)
}

func ValidateFlowCopy(header, body, footer, cta string) error {
	cta = strings.TrimSpace(cta)
	switch {
	case strings.TrimSpace(body) == "":
		return fmt.Errorf("%w: the body is required", ErrWhatsAppFlowCopyInvalid)
	case overLimit(body, WhatsAppButtonBodyMaxLen):
		return fmt.Errorf("%w: the body is longer than %d characters", ErrWhatsAppFlowCopyInvalid, WhatsAppButtonBodyMaxLen)
	case cta == "":
		return fmt.Errorf("%w: the button text is required", ErrWhatsAppFlowCopyInvalid)
	case overLimit(cta, WhatsAppFlowCTAMaxLen):
		return fmt.Errorf("%w: the button text is longer than %d characters", ErrWhatsAppFlowCopyInvalid, WhatsAppFlowCTAMaxLen)
	case hasPictograph(cta):
		return fmt.Errorf("%w: the button text cannot carry emoji", ErrWhatsAppFlowCopyInvalid)
	case overLimit(header, WhatsAppHeaderTextMaxLen):
		return fmt.Errorf("%w: the header is longer than %d characters", ErrWhatsAppFlowCopyInvalid, WhatsAppHeaderTextMaxLen)
	case overLimit(footer, WhatsAppFooterMaxLen):
		return fmt.Errorf("%w: the footer is longer than %d characters", ErrWhatsAppFlowCopyInvalid, WhatsAppFooterMaxLen)
	}
	return nil
}

func hasPictograph(s string) bool {
	for _, r := range s {
		if r >= 0x1F000 || (r >= 0x2600 && r <= 0x27BF) || unicode.Is(unicode.So, r) {
			return true
		}
	}
	return false
}
