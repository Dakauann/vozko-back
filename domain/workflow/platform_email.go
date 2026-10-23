package workflow

import (
	"errors"
	"fmt"
	"strings"
)

const LintInvalidPlatformEmail LintIssueCode = "NODE_INVALID_PLATFORM_EMAIL"

const (
	PlatformEmailSenderKey   = "sender_id"
	PlatformEmailToKey       = "to"
	PlatformEmailSubjectKey  = "subject"
	PlatformEmailBodyModeKey = "body_mode"
	PlatformEmailTextKey     = "text"
	PlatformEmailDesignKey   = "design_id"

	PlatformEmailBodyText   = "text"
	PlatformEmailBodyDesign = "design"

	PlatformEmailHandleSent   = "sent"
	PlatformEmailHandleFailed = "failed"
)

var (
	ErrPlatformEmailUnreadable = errors.New("workflow: the platform email node config cannot be read")
	ErrPlatformEmailInvalid    = errors.New("workflow: the platform email node is not valid")
)

type PlatformEmailSend struct {
	SenderID string
	To       string
	Subject  string
	Mode     string
	Text     string
	DesignID string
}

func ReadPlatformEmailSend(config map[string]interface{}) (PlatformEmailSend, error) {
	var send PlatformEmailSend
	fields := []struct {
		key    string
		target *string
	}{
		{PlatformEmailSenderKey, &send.SenderID},
		{PlatformEmailToKey, &send.To},
		{PlatformEmailSubjectKey, &send.Subject},
		{PlatformEmailBodyModeKey, &send.Mode},
		{PlatformEmailTextKey, &send.Text},
		{PlatformEmailDesignKey, &send.DesignID},
	}
	for _, f := range fields {
		value, ok := optionalText(config, f.key)
		if !ok {
			return PlatformEmailSend{}, fmt.Errorf("%w: %s is not text", ErrPlatformEmailUnreadable, f.key)
		}
		*f.target = value
	}
	send.SenderID = strings.TrimSpace(send.SenderID)
	send.DesignID = strings.TrimSpace(send.DesignID)
	send.Mode = strings.TrimSpace(send.Mode)
	if send.Mode == "" {
		send.Mode = PlatformEmailBodyText
	}
	if send.Mode != PlatformEmailBodyText && send.Mode != PlatformEmailBodyDesign {
		return PlatformEmailSend{}, fmt.Errorf("%w: unknown body mode %s", ErrPlatformEmailUnreadable, send.Mode)
	}
	return send, nil
}

func (s PlatformEmailSend) Render(fill func(string) string) PlatformEmailSend {
	out := s
	out.To = fill(s.To)
	out.Subject = fill(s.Subject)
	out.Text = fill(s.Text)
	return out
}

func ValidatePlatformEmailSends(g *Graph) error {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != NodeTypeActionSendPlatformEmail {
			continue
		}
		send, err := ReadPlatformEmailSend(n.Config)
		if err != nil {
			return platformEmailProblem(n.ID, PlatformEmailBodyModeKey, err)
		}
		if interpolationPattern.MatchString(send.SenderID) {
			return platformEmailProblem(n.ID, PlatformEmailSenderKey, errors.New("the sender is picked from the list, not from a variable"))
		}
		if send.Mode == PlatformEmailBodyDesign && interpolationPattern.MatchString(send.DesignID) {
			return platformEmailProblem(n.ID, PlatformEmailDesignKey, errors.New("the design is picked from the list, not from a variable"))
		}
	}
	return nil
}

func platformEmailProblem(nodeID, field string, cause error) error {
	return fmt.Errorf("%w: %s: node %q field %q", ErrPlatformEmailInvalid, strings.ReplaceAll(cause.Error(), `"`, "'"), nodeID, field)
}
