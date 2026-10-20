package messageaction

import (
	"context"
	"errors"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

type actionTarget struct {
	message *conversation.Message
	channel conversation.MessageActionChannel
	entry   *conversation.EntryContext
}

func (d Deps) authorize(by shared.Person, target conversation.MessageTarget) error {
	switch {
	case strings.TrimSpace(target.MessageID) == "":
		return conversation.ErrMessageIDRequired
	case strings.TrimSpace(target.EntryID) == "":
		return conversation.ErrEntryIDRequired
	case !target.EntryType.SupportsConversationView():
		return conversation.ErrEntryTypeInvalid
	}
	if !d.Access.HasWorkspacePermission(by.UserID, target.WorkspaceID, string(workspace.ResourceConversations), string(workspace.ActionSend), by.SystemAdmin) {
		return conversation.ErrUnauthorized
	}
	if !by.MayActOn(d.Access, target.WorkspaceID, target.EntryID, string(target.EntryType)) {
		return conversation.ErrUnauthorized
	}
	return nil
}

func (d Deps) load(by shared.Person, target conversation.MessageTarget) (*conversation.Message, error) {
	if err := d.authorize(by, target); err != nil {
		return nil, err
	}
	message, err := d.Messages.GetByID(strings.TrimSpace(target.MessageID))
	if errors.Is(err, conversation.ErrMessageNotFound) || (err == nil && !message.BelongsTo(target.EntryID, target.EntryType)) {
		return nil, conversation.ErrMessageNotFound
	}
	if err != nil {
		return nil, err
	}
	return message, nil
}

func (d Deps) prepare(ctx context.Context, by shared.Person, target conversation.MessageTarget, action conversation.MessageAction) (actionTarget, error) {
	message, err := d.load(by, target)
	if err != nil {
		return actionTarget{}, err
	}
	channel, ok := d.Channels.For(target.EntryType)
	if !ok {
		return actionTarget{}, conversation.ErrMessageActionUnsupported
	}
	caps := conversation.MessageCapabilitiesOf(channel)
	if err := conversation.CheckMessageAction(action, message, caps, by.UserID, d.Now()); err != nil {
		return actionTarget{}, err
	}
	entry, err := channel.ResolveEntry(ctx, target.EntryID)
	if err != nil {
		return actionTarget{}, err
	}
	return actionTarget{message: message, channel: channel, entry: entry}, nil
}

func (d Deps) save(message *conversation.Message) (*conversation.Message, error) {
	if err := d.Writer.Save(message); err != nil {
		return nil, err
	}
	return message, nil
}
