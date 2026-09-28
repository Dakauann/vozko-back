package unofficial_whatsapp

import (
	"testing"

	"vozko/domain/conversation"
)

type scopeSource struct {
	scope   conversation.DepartmentAccessScope
	allowed bool
	isAdmin bool
}

func (s *scopeSource) GetDepartmentScope(_, _ string, isAdmin bool) (conversation.DepartmentAccessScope, bool) {
	s.isAdmin = isAdmin
	return s.scope, s.allowed
}

func TestResolveScopeMapsTheAuthorizerScope(t *testing.T) {
	source := &scopeSource{scope: conversation.DepartmentAccessScope{DepartmentIDs: []string{"d1"}, Restrict: true}, allowed: true}
	scope, ok := ResolveScope(source, "u1", "ws1", true)
	if !ok || !scope.Restrict || len(scope.DepartmentIDs) != 1 || !source.isAdmin {
		t.Fatalf("scope %+v ok %v", scope, ok)
	}
}

func TestResolveScopeFailsClosed(t *testing.T) {
	if _, ok := ResolveScope(nil, "u1", "ws1", false); ok {
		t.Fatal("a missing source granted access")
	}
	if _, ok := ResolveScope(&scopeSource{allowed: false}, "u1", "ws1", false); ok {
		t.Fatal("a denied scope granted access")
	}
}
