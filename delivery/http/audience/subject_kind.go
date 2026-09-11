package audience

import (
	"strings"

	ca "vozko/domain/audience"
)

// parseSubjectKinds reads the subjectKind query parameter, a comma-separated
// list.
//
// Unknown values are KEPT rather than dropped, so ListInput.Validate refuses
// them with a message. Dropping them would turn a typo into a request that
// silently matched everything, which is the worst of both outcomes: no error
// and the wrong rows.
func parseSubjectKinds(raw string) []ca.SubjectKind {
	var kinds []ca.SubjectKind
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			kinds = append(kinds, ca.SubjectKind(part))
		}
	}
	return kinds
}
