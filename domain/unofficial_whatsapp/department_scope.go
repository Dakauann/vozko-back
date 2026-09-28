package unofficial_whatsapp

import "vozko/domain/conversation"

type DepartmentScopeSource interface {
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool)
}

func ResolveScope(source DepartmentScopeSource, userID, workspaceID string, systemAdmin bool) (DepartmentScope, bool) {
	if source == nil {
		return DepartmentScope{}, false
	}
	scope, allowed := source.GetDepartmentScope(userID, workspaceID, systemAdmin)
	if !allowed {
		return DepartmentScope{}, false
	}
	return DepartmentScope{DepartmentIDs: scope.DepartmentIDs, Restrict: scope.Restrict}, true
}
