package messageaction

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/usecases/messagestate"
)

type Access interface {
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
	CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool
}

type Messages interface {
	GetByID(id string) (*conversation.Message, error)
}

type Deps struct {
	Access   Access
	Messages Messages
	Channels Channels
	Writer   messagestate.Writer
	Send     conversation.OperatorSendUseCase
	Now      func() time.Time
}

func NewUseCases(deps Deps) (conversation.MessageActionUseCases, error) {
	missing := []string{}
	if deps.Access == nil {
		missing = append(missing, "access")
	}
	if deps.Messages == nil {
		missing = append(missing, "messages")
	}
	if deps.Writer.Messages == nil {
		missing = append(missing, "message writer")
	}
	if deps.Send == nil {
		missing = append(missing, "operator send")
	}
	if len(missing) > 0 {
		return conversation.MessageActionUseCases{}, fmt.Errorf("message actions: missing %s", strings.Join(missing, ", "))
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return conversation.MessageActionUseCases{
		React:        react{deps: deps},
		Edit:         edit{deps: deps},
		Delete:       retract{deps: deps},
		Resend:       resend{deps: deps},
		Capabilities: deps.Channels,
	}, nil
}
