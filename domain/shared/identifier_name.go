package shared

import "regexp"

const MaxIdentifierNameLength = 64

var identifierNamePattern = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

func ValidIdentifierName(name string) bool {
	return identifierNamePattern.MatchString(name)
}
