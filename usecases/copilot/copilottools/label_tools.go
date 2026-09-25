package copilottools

import (
	"context"
	"errors"
	"log"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/label"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type LabelBroadcaster interface {
	BroadcastLabelUpdate(workspaceID, entryID, entryType string)
}

type LabelDeps struct {
	EntryLabels label.EntryLabelsUseCase
	Create      label.CreateLabelUseCase
	List        label.ListLabelsUseCase
	Entries     conversation.EntryLookup
	Broadcast   LabelBroadcaster
}

type entryLabelArgs struct {
	EntryID   string `json:"entry_id" req:"true" desc:"entry_id exato de search_conversations"`
	EntryType string `json:"entry_type" req:"true" desc:"entry_type exato de search_conversations"`
	LabelID   string `json:"label_id" req:"true" desc:"label_id exato de list_labels" id:"true"`
}

type entryLabelTool struct {
	deps   LabelDeps
	remove bool
}

func NewApplyLabelTool(deps LabelDeps) copilot.Tool { return &entryLabelTool{deps: deps} }
func NewRemoveLabelTool(deps LabelDeps) copilot.Tool {
	return &entryLabelTool{deps: deps, remove: true}
}

func (t *entryLabelTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceLabels, Action: workspace.ActionAssign}
}

func (t *entryLabelTool) Definition() tools.Definition {
	if t.remove {
		return definition("remove_label", "Tira uma etiqueta de uma conversa. Só depois da aprovação do usuário.", entryLabelArgs{})
	}
	return definition("apply_label", "Coloca uma etiqueta existente numa conversa. Só depois da aprovação do usuário.", entryLabelArgs{})
}

func (t *entryLabelTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a entryLabelArgs
	bindArgs(args, &a)
	return []copilot.Field{
		{Key: "conversation", Value: describeConversation(t.deps.Entries, cc, a.EntryID, a.EntryType)},
		{Key: "label", Value: t.labelName(cc, a.LabelID)},
	}
}

func (t *entryLabelTool) labelName(cc copilot.Context, labelID string) string {
	labels, err := t.deps.List.Execute(cc.WorkspaceID)
	if err != nil {
		return "etiqueta desconhecida"
	}
	for _, l := range labels {
		if l != nil && l.ID == labelID {
			return l.Name
		}
	}
	return "etiqueta desconhecida"
}

func (t *entryLabelTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a entryLabelArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	target, err := targetOf(a.EntryID, a.EntryType)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	labelID, err := knownID(a.LabelID, "label_id", "list_labels")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if t.remove {
		err = t.deps.EntryLabels.Remove(cc.WorkspaceID, personOf(cc), label.RemoveEntryLabelInput{LabelID: labelID, EntryID: target.EntryID, EntryType: string(target.EntryType)})
	} else {
		_, err = t.deps.EntryLabels.Apply(cc.WorkspaceID, personOf(cc), label.AssignEntryLabelInput{LabelID: labelID, EntryID: target.EntryID, EntryType: string(target.EntryType)})
	}
	if err != nil {
		return labelFailure(t.Definition().Name, err)
	}
	if t.deps.Broadcast != nil {
		t.deps.Broadcast.BroadcastLabelUpdate(cc.WorkspaceID, target.EntryID, string(target.EntryType))
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"done": true, "label": t.labelName(cc, labelID)}}
}

type createLabelArgs struct {
	Name string `json:"name" req:"true" desc:"nome da etiqueta"`
}

type createLabelTool struct{ deps LabelDeps }

func NewCreateLabelTool(deps LabelDeps) copilot.Tool { return &createLabelTool{deps: deps} }

func (t *createLabelTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceLabels, Action: workspace.ActionCreate}
}

func (t *createLabelTool) Definition() tools.Definition {
	return definition("create_label", "Cria uma etiqueta nova no workspace. Só depois da aprovação do usuário.", createLabelArgs{})
}

func (t *createLabelTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a createLabelArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	created, err := t.deps.Create.Execute(cc.WorkspaceID, label.CreateLabelInput{Name: strings.TrimSpace(a.Name)})
	if err != nil {
		return labelFailure("create_label", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"label_id": created.ID, "name": created.Name}}
}

func labelFailure(tool string, err error) copilot.Result {
	switch {
	case errors.Is(err, label.ErrEntryAccess), errors.Is(err, label.ErrUnauthorized):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o usuário não tem acesso a esta conversa ou etiqueta"}
	case errors.Is(err, label.ErrLabelNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "etiqueta desconhecida; use os ids de list_labels"}
	case errors.Is(err, label.ErrEntryLabelExists):
		return copilot.Result{Status: copilot.StatusError, Message: "a conversa já tem essa etiqueta"}
	case errors.Is(err, label.ErrEntryLabelNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "a conversa não tem essa etiqueta"}
	case errors.Is(err, label.ErrLabelNameExists):
		return copilot.Result{Status: copilot.StatusError, Message: "já existe uma etiqueta com esse nome"}
	case errors.Is(err, label.ErrLabelNameRequired):
		return copilot.Result{Status: copilot.StatusError, Message: "o nome da etiqueta é obrigatório"}
	case errors.Is(err, label.ErrInvalidEntryType):
		return copilot.Result{Status: copilot.StatusError, Message: "este tipo de conversa não tem etiquetas"}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao mudar as etiquetas"}
}

func (t *entryLabelTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[entryLabelArgs](t.deps.Entries, cc, args)
	return err
}

func (t *createLabelTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[createLabelArgs](nil, cc, args)
	return err
}
