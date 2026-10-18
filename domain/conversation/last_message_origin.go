package conversation

type LastMessageOrigin string

const (
	LastMessageByNobody   LastMessageOrigin = ""
	LastMessageByContact  LastMessageOrigin = "contact"
	LastMessageByBusiness LastMessageOrigin = "business"
)

func (e EntryWithLastMessage) LastMessageOrigin() LastMessageOrigin {
	sender := RestoreSentBy(e.LastMessageSenderKind, e.LastMessageSenderID)
	switch {
	case sender.IsContact():
		return LastMessageByContact
	case sender.FromTheBusiness():
		return LastMessageByBusiness
	case sender.Valid():
		return LastMessageByNobody
	case e.LastMessageType.IsInbound():
		return LastMessageByContact
	case e.LastMessageType == MessageTypeOperator, e.LastMessageType == MessageTypeAIResponse:
		return LastMessageByBusiness
	}
	return LastMessageByNobody
}
