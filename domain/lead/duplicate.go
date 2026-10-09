package lead

import "vozko/domain/shared"

type DuplicateReason string

const (
	DuplicateSharedPhone        DuplicateReason = "shared_phone"
	DuplicateSameNameAndAddress DuplicateReason = "same_name_and_address"
)

type DuplicateCandidate struct {
	LeadID  string
	Reasons []DuplicateReason
}

func DuplicateCandidates(v Viewer, l *Lead, existing []*Lead) []DuplicateCandidate {
	var candidates []DuplicateCandidate
	name := shared.FoldForMatch(l.RealName())
	fingerprints := l.addressFingerprints(v)
	for _, other := range existing {
		if other == nil || (l.ID != "" && other.ID == l.ID) {
			continue
		}
		var reasons []DuplicateReason
		if l.sharesAPhoneWith(other) {
			reasons = append(reasons, DuplicateSharedPhone)
		}
		if name != "" && name == shared.FoldForMatch(other.RealName()) && other.livesAtAnyOf(fingerprints) {
			reasons = append(reasons, DuplicateSameNameAndAddress)
		}
		if len(reasons) > 0 {
			candidates = append(candidates, DuplicateCandidate{LeadID: other.ID, Reasons: reasons})
		}
	}
	return candidates
}

func IdentityHolder(l *Lead, existing []*Lead) *Lead {
	if !l.HasIdentity() {
		return nil
	}
	for _, other := range existing {
		if other != nil && other.ID != l.ID && other.HasIdentity() && sameNumber(other.Number, l.Number) {
			return other
		}
	}
	return nil
}

func (l *Lead) sharesAPhoneWith(other *Lead) bool {
	for _, number := range l.Numbers() {
		if other.HoldsNumber(number) {
			return true
		}
	}
	return false
}

func (l *Lead) addressFingerprints(v Viewer) map[string]bool {
	_, prints := l.DuplicateLookup(v)
	fingerprints := make(map[string]bool, len(prints))
	for _, fingerprint := range prints {
		fingerprints[fingerprint] = true
	}
	return fingerprints
}

func (l *Lead) livesAtAnyOf(fingerprints map[string]bool) bool {
	for _, a := range l.Addresses {
		if fingerprints[a.Fingerprint()] {
			return true
		}
	}
	return false
}

func (l *Lead) DuplicateLookup(v Viewer) ([]string, []string) {
	if !v.ReadsAddresses {
		return l.Numbers(), nil
	}
	fingerprints := make([]string, 0, len(l.Addresses))
	for _, a := range l.Addresses {
		fingerprints = append(fingerprints, a.Fingerprint())
	}
	return l.Numbers(), fingerprints
}

type IdentityTaken struct {
	LeadID string
}

func (e *IdentityTaken) Error() string {
	return ErrLeadDuplicate.Error()
}

func (e *IdentityTaken) Unwrap() error {
	return ErrLeadDuplicate
}
