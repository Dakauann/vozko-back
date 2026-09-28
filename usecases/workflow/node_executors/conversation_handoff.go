package node_executors

import ia "vozko/domain/inbox_assignment"

type ConversationHandOff interface {
	HandOffToHuman(workspaceID, entryID, entryType, toUserID string) error
	HandOffToRoulette(in ia.RouletteHandOff) (string, error)
}
