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

func CodeSent(status string) error {
	return expectLinkStatus(status, linkCodeSentStatus)
}

func LinkVerified(status string) error {
	return expectLinkStatus(status, linkVerifiedStatus)
}

func expectLinkStatus(status, want string) error {
	if status != want {
		return fmt.Errorf("%w: Meta answered %q", ErrPageLinkRefused, status)
	}
	return nil
}
