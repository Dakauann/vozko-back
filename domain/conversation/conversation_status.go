package conversation

type ConversationStatus string

const (
	ConversationStatusNew ConversationStatus = "new"

	ConversationStatusOngoing ConversationStatus = "ongoing"

	ConversationStatusFinished ConversationStatus = "finished"
)

func (s ConversationStatus) Valid() bool {
	switch s {
	case ConversationStatusNew, ConversationStatusOngoing, ConversationStatusFinished:
		return true
	}
	return false
}

func (s ConversationStatus) String() string {
	return string(s)
}
