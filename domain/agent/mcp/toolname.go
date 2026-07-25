package mcp

import "strings"

type QualifiedName struct {
	Kind     Kind
	SourceID string
	Tool     string
}

func ParseQualifiedName(s string) (QualifiedName, error) {
	colon := strings.IndexByte(s, ':')
	if colon < 1 || colon == len(s)-1 {
		return QualifiedName{}, ErrToolNameMalformed
	}
	kind := Kind(s[:colon])
	rest := s[colon+1:]
	dot := strings.IndexByte(rest, '.')
	if dot < 1 || dot == len(rest)-1 {
		return QualifiedName{}, ErrToolNameMalformed
	}
	if kind != KindBuiltin && kind != KindRemote {
		return QualifiedName{}, ErrToolNameMalformed
	}
	return QualifiedName{Kind: kind, SourceID: rest[:dot], Tool: rest[dot+1:]}, nil
}

func (q QualifiedName) String() string {
	return string(q.Kind) + ":" + q.SourceID + "." + q.Tool
}
