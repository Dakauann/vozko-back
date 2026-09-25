package copilottools

import (
	"context"
	"errors"
	"log"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

const assignableMembersLimit = 50

type AssignmentDeps struct {
	Assign      ia.PersonAssignUseCase
	Members     workspace.ListAssignableMembersUseCase
	Departments wd.ListDepartmentsUseCase
	Entries     conversation.EntryLookup
}

type listAssignableMembersArgs struct {
	Search string `json:"search" desc:"parte do nome"`
}

type listAssignableMembersTool struct{ deps AssignmentDeps }

func NewListAssignableMembersTool(deps AssignmentDeps) copilot.Tool {
	return &listAssignableMembersTool{deps: deps}
}

func (t *listAssignableMembersTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceMembers, Action: workspace.ActionRead}
}

func (t *listAssignableMembersTool) Definition() tools.Definition {
	return definition("list_assignable_members",
		"Lista as pessoas da equipe para quem o usuário pode passar conversas, com os departamentos de cada uma.",
		listAssignableMembersArgs{})
}

func (t *listAssignableMembersTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a listAssignableMembersArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	members, total, err := t.deps.Members.Execute(cc.UserID, cc.WorkspaceID, cc.SystemAdmin, strings.TrimSpace(a.Search), 1, assignableMembersLimit)
	if err != nil {
		log.Printf("[copilot] list_assignable_members failed: %v", err)
		return copilot.Result{Status: copilot.StatusError, Message: "falha ao consultar a equipe"}
	}
	out := make([]map[string]interface{}, 0, len(members))
	for _, m := range members {
		if m == nil || m.Member == nil {
			continue
		}
		departments := make([]string, 0, len(m.Departments))
		for _, d := range m.Departments {
			departments = append(departments, d.Name)
		}
		out = append(out, map[string]interface{}{"member_id": m.UserID, "name": m.Username, "departments": departments})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"total": total, "members": out}}
}

func (t *listAssignableMembersTool) memberName(cc copilot.Context, userID string) string {
	members, _, err := t.deps.Members.Execute(cc.UserID, cc.WorkspaceID, cc.SystemAdmin, "", 1, assignableMembersLimit)
	if err == nil {
		for _, m := range members {
			if m != nil && m.Member != nil && m.UserID == userID {
				return m.Username
			}
		}
	}
	return "pessoa desconhecida ou fora do alcance do usuário"
}

type assignConversationArgs struct {
	EntryID   string `json:"entry_id" req:"true" desc:"entry_id exato de search_conversations"`
	EntryType string `json:"entry_type" req:"true" desc:"entry_type exato de search_conversations"`
	MemberID  string `json:"member_id" req:"true" desc:"member_id exato de list_assignable_members" id:"true"`
}

type assignConversationTool struct{ deps AssignmentDeps }

func NewAssignConversationTool(deps AssignmentDeps) copilot.Tool {
	return &assignConversationTool{deps: deps}
}

func (t *assignConversationTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceConversations, Action: workspace.ActionAssign}
}

func (t *assignConversationTool) Definition() tools.Definition {
	return definition("assign_conversation",
		"Passa uma conversa para uma pessoa da equipe, que vira a responsável. Só depois da aprovação do usuário.",
		assignConversationArgs{})
}

func (t *assignConversationTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a assignConversationArgs
	bindArgs(args, &a)
	lister := listAssignableMembersTool{deps: t.deps}
	return []copilot.Field{
		{Key: "conversation", Value: describeConversation(t.deps.Entries, cc, a.EntryID, a.EntryType)},
		{Key: "member", Value: lister.memberName(cc, strings.TrimSpace(a.MemberID))},
	}
}

func (t *assignConversationTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a assignConversationArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	target, err := targetOf(a.EntryID, a.EntryType)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	memberID, err := knownID(a.MemberID, "member_id", "list_assignable_members")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if err := t.deps.Assign.Assign(personOf(cc), cc.WorkspaceID, target.EntryID, string(target.EntryType), memberID); err != nil {
		return assignmentFailure("assign_conversation", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"assigned": true}}
}

type transferConversationArgs struct {
	EntryID      string `json:"entry_id" req:"true" desc:"entry_id exato de search_conversations"`
	EntryType    string `json:"entry_type" req:"true" desc:"entry_type exato de search_conversations"`
	DepartmentID string `json:"department_id" req:"true" desc:"id exato de list_departments" id:"true"`
}

type transferConversationTool struct{ deps AssignmentDeps }

func NewTransferConversationTool(deps AssignmentDeps) copilot.Tool {
	return &transferConversationTool{deps: deps}
}

func (t *transferConversationTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceConversations, Action: workspace.ActionAssign}
}

func (t *transferConversationTool) Definition() tools.Definition {
	return definition("transfer_conversation",
		"Transfere uma conversa para um departamento: a roleta do departamento escolhe quem atende. Só depois da aprovação do usuário.",
		transferConversationArgs{})
}

func (t *transferConversationTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a transferConversationArgs
	bindArgs(args, &a)
	return []copilot.Field{
		{Key: "conversation", Value: describeConversation(t.deps.Entries, cc, a.EntryID, a.EntryType)},
		{Key: "department", Value: t.departmentName(cc, strings.TrimSpace(a.DepartmentID))},
	}
}

func (t *transferConversationTool) departmentName(cc copilot.Context, id string) string {
	departments, err := t.deps.Departments.Execute(cc.WorkspaceID)
	if err == nil {
		for _, d := range departments {
			if d.ID == id {
				return d.Name
			}
		}
	}
	return "departamento desconhecido"
}

func (t *transferConversationTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a transferConversationArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	target, err := targetOf(a.EntryID, a.EntryType)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	departmentID, err := knownID(a.DepartmentID, "department_id", "list_departments")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	owner, err := t.deps.Assign.HandOff(personOf(cc), cc.WorkspaceID, target.EntryID, string(target.EntryType), departmentID)
	if err != nil {
		return assignmentFailure("transfer_conversation", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"transferred": true, "someone_assigned": owner != ""}}
}

func assignmentFailure(tool string, err error) copilot.Result {
	switch {
	case errors.Is(err, ia.ErrAssignEntryAccess):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o usuário não tem acesso a esta conversa"}
	case errors.Is(err, ia.ErrAssignTargetOutOfReach):
		return copilot.Result{Status: copilot.StatusDenied, Message: "essa pessoa está fora dos departamentos do usuário"}
	case errors.Is(err, ia.ErrAssignTargetIneligible), errors.Is(err, ia.ErrHandOffTargetNoAccess):
		return copilot.Result{Status: copilot.StatusError, Message: "essa pessoa não pode receber conversas neste workspace"}
	case errors.Is(err, ia.ErrDepartmentOutOfScope):
		return copilot.Result{Status: copilot.StatusError, Message: "departamento desconhecido; use os ids de list_departments"}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao mudar o responsável da conversa"}
}

func (t *assignConversationTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[assignConversationArgs](t.deps.Entries, cc, args)
	return err
}

func (t *transferConversationTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[transferConversationArgs](t.deps.Entries, cc, args)
	return err
}
