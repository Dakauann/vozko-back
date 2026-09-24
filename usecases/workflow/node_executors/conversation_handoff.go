package node_executors

import ia "vozko/domain/inbox_assignment"

// ConversationHandOff moves a conversation to a person through the assignment
// service, so the move lands in the history and timeline, is credited to the
// automation that held the conversation, reaches the new owner's inbox live,
// and takes the automation out of the conversation.
type ConversationHandOff interface {
	// HandOffToHuman gives the conversation to a named member who can open
	// conversations in the workspace.
	HandOffToHuman(workspaceID, entryID, entryType, toUserID string) error
	// HandOffToRoulette deals it through the roulette inbound conversations
	// use; "" means it went to the team queue.
	HandOffToRoulette(in ia.RouletteHandOff) (string, error)
}
