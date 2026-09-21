package audience

import (
	"strings"

	ca "vozko/domain/audience"
)

func parseSubjectKinds(raw string) []ca.SubjectKind {
	var kinds []ca.SubjectKind
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			kinds = append(kinds, ca.SubjectKind(part))
		}
	}
	return kinds
}
