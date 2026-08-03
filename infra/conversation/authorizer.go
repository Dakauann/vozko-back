package conversation

import (
	"context"
	"log"
	"time"

	"vozko/domain/cache"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workspace"
	workspace_department "vozko/domain/workspace/workspace_department"
)

type whatsappEntryAccessRepository interface {
	CanUserAccessEntry(workspaceID, entryID string, isAdmin bool) (bool, error)
	GetAccessibleEntryIDs(workspaceID string, isAdmin bool) ([]string, error)
}

// entryAccessRepository is the port for channels whose conversations carry
// workspace_id directly, so the check is a single ownership comparison with no
// campaign indirection to walk. Kept narrow here rather than importing each
// channel's repository, so the authorizer stays decoupled from all of them.
type entryAccessRepository interface {
	WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
	ListEntryIDsByWorkspace(ctx context.Context, workspaceID string) ([]string, error)
}

// instagramEntryAccessRepository is the previous name of entryAccessRepository.
//
// Deprecated: use entryAccessRepository.
type instagramEntryAccessRepository = entryAccessRepository

type workspaceMembershipRepository interface {
	GetMember(workspaceID, userID string) (*workspace.Member, error)
	HasPermission(memberID string, resource workspace.Resource, action workspace.Action) (bool, error)
}

type departmentMembershipRepository interface {
	ListDepartments(workspaceID string) ([]workspace_department.Department, error)
	GetMemberDepartmentIDs(workspaceID, userID string) ([]string, error)
}

type assignmentLookupRepository interface {
	IsAssignedToUser(workspaceID, entryID, entryType, userID string) (bool, error)
}

type Authorizer struct {
	whatsappEntryRepo whatsappEntryAccessRepository
	// entryRepos holds one reader per channel whose conversations own their
	// workspace id, keyed by entry type. Registering one never displaces
	// another, the mistake a single Instagram-shaped field would have forced on
	// the second such channel.
	entryRepos        map[shared.EntryType]entryAccessRepository
	workspaceRepo     workspaceMembershipRepository
	departmentRepo    departmentMembershipRepository
	assignmentRepo    assignmentLookupRepository
	workspaceResolver conversation.CampaignWorkspaceResolver
	shared            cache.SharedState
	cacheTTL          time.Duration
}

func NewAuthorizer(
	whatsappEntryRepo whatsappEntryAccessRepository,
	workspaceRepo workspaceMembershipRepository,
	departmentRepo departmentMembershipRepository,
	assignmentRepo assignmentLookupRepository,
	workspaceResolver conversation.CampaignWorkspaceResolver,
	shared cache.SharedState,
) conversation.ConversationAuthorizer {
	return &Authorizer{
		whatsappEntryRepo: whatsappEntryRepo,
		workspaceRepo:     workspaceRepo,
		departmentRepo:    departmentRepo,
		assignmentRepo:    assignmentRepo,
		workspaceResolver: workspaceResolver,
		shared:            shared,
		cacheTTL:          5 * time.Minute,
	}
}

func (a *Authorizer) CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool {
	if userID == "" || entryID == "" || entryType == "" {
		return false
	}

	if isAdmin {
		cacheKey := userID + ":" + workspaceID
		if a.checkCache(cacheKey, entryID) {
			return true
		}
		hasAccess := a.entryBelongsToWorkspace(workspaceID, entryID, entryType, true)
		if hasAccess {
			a.setCache(cacheKey, entryID)
		}
		return hasAccess
	}

	if workspaceID == "" {
		log.Printf("[Authorizer] user %s has no workspace context, access denied", userID)
		return false
	}

	cacheKey := userID + ":" + workspaceID
	if a.checkCache(cacheKey, entryID) {
		return true
	}

	scope, allowed := a.GetDepartmentScope(userID, workspaceID, false)
	if !allowed {
		return false
	}

	if !a.entryBelongsToWorkspace(workspaceID, entryID, entryType, false) {
		return false
	}

	if !scope.Restrict {
		a.setCache(cacheKey, entryID)
		return true
	}

	if a.workspaceResolver == nil {
		log.Printf("[Authorizer] missing workspace resolver for department-scoped entry check")
		return false
	}

	departmentID, err := a.workspaceResolver.GetEntryDepartmentID(entryID, entryType)
	if err != nil {
		log.Printf("[Authorizer] error resolving department for %s (%s): %v", entryID, entryType, err)
		return false
	}

	if departmentID != "" {
		if containsString(scope.DepartmentIDs, departmentID) {
			a.setCache(cacheKey, entryID)
			return true
		}
	}

	if a.assignmentRepo != nil {
		assigned, err := a.assignmentRepo.IsAssignedToUser(workspaceID, entryID, entryType, userID)
		if err != nil {
			log.Printf("[Authorizer] error checking direct assignment for user %s entry %s: %v", userID, entryID, err)
			return false
		}
		if assigned {
			a.setCache(cacheKey, entryID)
			return true
		}
	}
	return false
}

func (a *Authorizer) CanAccessCampaign(userID, workspaceID, campaignID, campaignType string, isAdmin bool) bool {
	if userID == "" || campaignID == "" || campaignType == "" {
		return false
	}
	if a.workspaceResolver == nil {
		return false
	}

	resolvedWorkspaceID, err := a.workspaceResolver.GetCampaignWorkspaceID(campaignID, campaignType)
	if err != nil || resolvedWorkspaceID == "" {
		return false
	}
	if workspaceID != "" && resolvedWorkspaceID != workspaceID {
		return false
	}
	if isAdmin {
		return true
	}

	scope, allowed := a.GetDepartmentScope(userID, resolvedWorkspaceID, false)
	if !allowed {
		return false
	}
	if !scope.Restrict {
		return true
	}

	departmentID, err := a.workspaceResolver.GetCampaignDepartmentID(campaignID, campaignType)
	if err != nil {
		log.Printf("[Authorizer] error resolving campaign department for %s (%s): %v", campaignID, campaignType, err)
		return false
	}
	if departmentID == "" {
		return false
	}
	return containsString(scope.DepartmentIDs, departmentID)
}

func (a *Authorizer) GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool) {
	if userID == "" || workspaceID == "" {
		return conversation.DepartmentAccessScope{}, false
	}
	if isAdmin {
		return conversation.DepartmentAccessScope{}, true
	}

	member, err := a.workspaceRepo.GetMember(workspaceID, userID)
	if err != nil || member == nil {
		log.Printf("[Authorizer] user %s is not a member of workspace %s", userID, workspaceID)
		return conversation.DepartmentAccessScope{}, false
	}

	if member.Role == workspace.RoleOwner || member.Role == workspace.RoleAdmin {
		wsHasDepts := false
		if a.departmentRepo != nil {
			if depts, err := a.departmentRepo.ListDepartments(workspaceID); err == nil && len(depts) > 0 {
				wsHasDepts = true
			}
		}
		return conversation.DepartmentAccessScope{WorkspaceHasDepartments: wsHasDepts}, true
	}

	hasPerm, err := a.workspaceRepo.HasPermission(member.ID, workspace.ResourceConversations, workspace.ActionRead)
	if err != nil || !hasPerm {
		log.Printf("[Authorizer] user %s lacks conversations:read in workspace %s", userID, workspaceID)
		return conversation.DepartmentAccessScope{}, false
	}

	if a.departmentRepo == nil {
		log.Printf("[Authorizer] missing department repository for employee %s in workspace %s", userID, workspaceID)
		return conversation.DepartmentAccessScope{Restrict: true}, true
	}

	departments, err := a.departmentRepo.ListDepartments(workspaceID)
	if err != nil {
		log.Printf("[Authorizer] error listing departments for workspace %s: %v", workspaceID, err)
		return conversation.DepartmentAccessScope{Restrict: true}, true
	}
	if len(departments) == 0 {
		return conversation.DepartmentAccessScope{}, true
	}

	departmentIDs, err := a.departmentRepo.GetMemberDepartmentIDs(workspaceID, userID)
	if err != nil {
		log.Printf("[Authorizer] error resolving departments for user %s in workspace %s: %v", userID, workspaceID, err)
		return conversation.DepartmentAccessScope{Restrict: true, WorkspaceHasDepartments: true}, true
	}

	return conversation.DepartmentAccessScope{
		DepartmentIDs:           uniqueStrings(departmentIDs),
		Restrict:                true,
		WorkspaceHasDepartments: true,
	}, true
}

func (a *Authorizer) entryBelongsToWorkspace(workspaceID, entryID, entryType string, isAdmin bool) bool {
	switch entryType {
	case "whatsapp":
		ok, err := a.whatsappEntryRepo.CanUserAccessEntry(workspaceID, entryID, isAdmin)
		if err != nil {
			return false
		}
		return ok
	default:
		repo, ok := a.entryRepoFor(entryType)
		if !ok {
			return false
		}
		owner, err := repo.WorkspaceIDForEntry(context.Background(), entryID)
		if err != nil {
			return false
		}
		return owner != "" && owner == workspaceID
	}
}

func (a *Authorizer) GetAccessibleEntryIDs(workspaceID, entryType string, isAdmin bool) []string {
	if workspaceID == "" || entryType == "" {
		return nil
	}

	switch entryType {
	case "whatsapp":
		ids, err := a.whatsappEntryRepo.GetAccessibleEntryIDs(workspaceID, isAdmin)
		if err != nil {
			return nil
		}
		return ids
	}

	if repo, ok := a.entryRepoFor(entryType); ok {
		ids, err := repo.ListEntryIDsByWorkspace(context.Background(), workspaceID)
		if err != nil {
			return nil
		}
		return ids
	}
	return nil
}

// SetEntryAccessRepo registers a channel's entry-access reader. Optional, so the
// authorizer still constructs when a channel is disabled.
func (a *Authorizer) SetEntryAccessRepo(entryType shared.EntryType, repo entryAccessRepository) {
	if a == nil || repo == nil || entryType == "" {
		return
	}
	if a.entryRepos == nil {
		a.entryRepos = make(map[shared.EntryType]entryAccessRepository, 2)
	}
	a.entryRepos[entryType] = repo
}

// SetInstagramEntryRepo registers the Instagram reader.
//
// Deprecated: use SetEntryAccessRepo(shared.EntryTypeInstagram, repo).
func (a *Authorizer) SetInstagramEntryRepo(repo entryAccessRepository) {
	a.SetEntryAccessRepo(shared.EntryTypeInstagram, repo)
}

// SetTelegramEntryRepo registers the Telegram reader.
func (a *Authorizer) SetTelegramEntryRepo(repo entryAccessRepository) {
	a.SetEntryAccessRepo(shared.EntryTypeTelegram, repo)
}

func (a *Authorizer) entryRepoFor(entryType string) (entryAccessRepository, bool) {
	if a == nil || a.entryRepos == nil {
		return nil, false
	}
	repo, ok := a.entryRepos[shared.EntryType(entryType)]
	return repo, ok && repo != nil
}

func (a *Authorizer) HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool {
	if userID == "" || workspaceID == "" {
		return false
	}

	if isSystemAdmin {
		return true
	}

	member, err := a.workspaceRepo.GetMember(workspaceID, userID)
	if err != nil || member == nil {
		log.Printf("[Authorizer] HasWorkspacePermission: user %s is not a member of workspace %s", userID, workspaceID)
		return false
	}

	if member.Role == workspace.RoleOwner || member.Role == workspace.RoleAdmin {
		return true
	}

	hasPerm, err := a.workspaceRepo.HasPermission(member.ID, workspace.Resource(resource), workspace.Action(action))
	if err != nil || !hasPerm {
		log.Printf("[Authorizer] HasWorkspacePermission: user %s lacks %s:%s in workspace %s", userID, resource, action, workspaceID)
		return false
	}

	return true
}

func (a *Authorizer) IsWorkspaceMember(userID, workspaceID string) bool {
	if userID == "" || workspaceID == "" {
		return false
	}
	member, err := a.workspaceRepo.GetMember(workspaceID, userID)
	if err != nil || member == nil {
		return false
	}
	return true
}

func (a *Authorizer) IsWorkspaceOwnerOrAdmin(userID, workspaceID string) bool {
	if userID == "" || workspaceID == "" {
		return false
	}
	member, err := a.workspaceRepo.GetMember(workspaceID, userID)
	if err != nil || member == nil {
		return false
	}
	return member.Role == workspace.RoleOwner || member.Role == workspace.RoleAdmin
}

func (a *Authorizer) checkCache(cacheKey, entryID string) bool {
	key := "auth_cache:" + cacheKey + ":" + entryID
	exists, err := a.shared.Exists(key)
	if err != nil {
		return false
	}
	return exists
}

func (a *Authorizer) setCache(cacheKey, entryID string) {
	key := "auth_cache:" + cacheKey + ":" + entryID
	_ = a.shared.SetString(key, "1", a.cacheTTL)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
