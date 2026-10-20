package conversation_usecase

import (
	"log/slog"

	"vozko/domain/conversation"
)

type ActorNameResolver interface {
	ResolveNames(actorIDs ...string) (map[string]string, error)
}

func historySourceLabels(names ActorNameResolver, history []*conversation.Message) conversation.SourceLabels {
	senders := conversation.WorkflowSenders(history)
	if len(senders) == 0 || names == nil {
		return conversation.NewSourceLabels(nil)
	}
	found, err := names.ResolveNames(senders...)
	if err != nil {
		slog.Warn("conversation.history.workflow_names_unreadable", slog.Int("workflows", len(senders)), slog.String("error", err.Error()))
		return conversation.NewSourceLabels(nil)
	}
	return conversation.NewSourceLabels(found)
}
