package conversation

import (
	"errors"
	"strings"

	"vozko/domain/shared"
	"vozko/domain/workspace"
)

var ErrNoConversationScope = errors.New("conversation: the viewer's conversation scope could not be read")

var ErrNotVisible = errors.New("conversation: the conversation is not visible to this viewer")

type VisibilityScope struct {
	ReadScope
	AssignedUserID string
}

func (v VisibilityScope) Unrestricted() bool {
	return !v.RestrictDepartments && v.AssignedUserID == ""
}

func NothingVisible() VisibilityScope {
	return VisibilityScope{ReadScope: ReadScope{RestrictDepartments: true}}
}

type Visibility interface {
	CanView(viewer shared.Person, workspaceID, entryID, entryType string) (bool, error)
	Scope(viewer shared.Person, workspaceID string) (VisibilityScope, error)
}

type authorizerVisibility struct {
	authorizer ConversationAuthorizer
}

func NewVisibility(authorizer ConversationAuthorizer) Visibility {
	return authorizerVisibility{authorizer: authorizer}
}

func (v authorizerVisibility) CanView(viewer shared.Person, workspaceID, entryID, entryType string) (bool, error) {
	if v.authorizer == nil {
		return false, ErrNoConversationScope
	}
	if strings.TrimSpace(workspaceID) == "" {
		return false, nil
	}
	return viewer.MayActOn(v.authorizer, workspaceID, entryID, entryType), nil
}

func (v authorizerVisibility) Scope(viewer shared.Person, workspaceID string) (VisibilityScope, error) {
	if v.authorizer == nil || strings.TrimSpace(viewer.UserID) == "" || strings.TrimSpace(workspaceID) == "" {
		return VisibilityScope{}, ErrNoConversationScope
	}
	return VisibilityScopeOf(v.authorizer, viewer.UserID, workspaceID, viewer.SystemAdmin, "")
}

type OthersVisibilityAuthorizer interface {
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
	IsWorkspaceOwnerOrAdmin(userID, workspaceID string) bool
}

type ScopeAuthorizer interface {
	OthersVisibilityAuthorizer
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (DepartmentAccessScope, bool)
}

func VisibilityScopeOf(authorizer ScopeAuthorizer, userID, workspaceID string, isAdmin bool, selectedDepartmentID string) (VisibilityScope, error) {
	if authorizer == nil {
		return VisibilityScope{}, ErrNoConversationScope
	}
	scope, allowed := authorizer.GetDepartmentScope(userID, workspaceID, isAdmin)
	if !allowed {
		return VisibilityScope{}, ErrNoConversationScope
	}
	read, err := scope.ReadScope(userID, isAdmin, selectedDepartmentID)
	if err != nil {
		return VisibilityScope{}, err
	}
	visibility := VisibilityScope{ReadScope: read}
	if !isAdmin && !SeesOthers(authorizer, userID, workspaceID) {
		visibility.AssignedUserID = userID
	}
	return visibility, nil
}

func SeesOthers(authorizer OthersVisibilityAuthorizer, userID, workspaceID string) bool {
	return authorizer.HasWorkspacePermission(userID, workspaceID, string(workspace.ResourceConversations), string(workspace.ActionViewOthers), false) ||
		authorizer.IsWorkspaceOwnerOrAdmin(userID, workspaceID)
}
