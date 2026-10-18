package inbox_assignment

import (
	"vozko/domain/conversation"
	"vozko/domain/customerportfolio"
	"vozko/domain/workspace"
)

type PortfolioHolders interface {
	HolderOfConversation(workspaceID, entryID, entryType string) (string, error)
}

type PortfolioReceivers interface {
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool)
	workspace.PermissionChecker
}

func CanReceivePortfolio(receivers PortfolioReceivers, holderID, workspaceID, departmentID string) bool {
	if receivers == nil || holderID == "" || workspaceID == "" {
		return false
	}
	scope, member := receivers.GetDepartmentScope(holderID, workspaceID, false)
	if !member || !scope.Reaches(departmentID) {
		return false
	}
	return workspace.HoldsCapability(receivers, workspaceID, holderID, false, customerportfolio.CapabilityUse)
}
