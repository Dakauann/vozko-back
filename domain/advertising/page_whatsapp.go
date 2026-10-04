package advertising

import (
	"fmt"
	"strings"
)

const (
	linkCodeSentStatus = "VERIFICATION_CODE_SEND_SUCCESS"
	linkVerifiedStatus = "VERIFIED"
	minLinkCodeDigits  = 4
	maxLinkCodeDigits  = 8
)

func NumbersLinkableTo(page RemotePage, numbers []WorkspaceNumber) []WorkspaceNumber {
	var linkable []WorkspaceNumber
	for _, n := range numbers {
		if n.LinkedTo(page) {
			continue
		}
		if n.Kind == NumberOfficial && (page.BusinessID == "" || n.PortfolioID != page.BusinessID) {
			continue
		}
		linkable = append(linkable, n)
	}
	return linkable
}

func LinkableNumber(page RemotePage, numbers []WorkspaceNumber, raw string) (string, error) {
	for _, n := range NumbersLinkableTo(page, numbers) {
		if SameWhatsAppNumber(n.Number, raw) {
			return DigitsOnly(n.Number), nil
		}
	}
	return "", FieldError("number", "not_linkable")
}

func LinkCode(raw string) (string, error) {
	trimmed := strings.ReplaceAll(strings.TrimSpace(raw), " ", "")
	digits := DigitsOnly(trimmed)
	if digits != trimmed || len(digits) < minLinkCodeDigits || len(digits) > maxLinkCodeDigits {
		return "", FieldError("code", "invalid")
	}
	return digits, nil
}

type LinkRequestOutcome string

const (
	LinkCodeSent      LinkRequestOutcome = "code_sent"
	LinkAlreadyLinked LinkRequestOutcome = "linked"
)

func LinkRequested(status string) (LinkRequestOutcome, error) {
	switch status {
	case linkCodeSentStatus:
		return LinkCodeSent, nil
	case linkVerifiedStatus:
		return LinkAlreadyLinked, nil
	}
	return "", &PageLinkRefusal{Answer: status}
}

func LinkVerified(status string) error {
	return expectLinkStatus(status, linkVerifiedStatus)
}

type PageLinkRefusal struct {
	Answer string
}

func (r *PageLinkRefusal) Error() string {
	return fmt.Sprintf("%v: Meta answered %q", ErrPageLinkRefused, r.Answer)
}

func (r *PageLinkRefusal) Unwrap() error { return ErrPageLinkRefused }

func expectLinkStatus(status, want string) error {
	if status != want {
		return &PageLinkRefusal{Answer: status}
	}
	return nil
}
