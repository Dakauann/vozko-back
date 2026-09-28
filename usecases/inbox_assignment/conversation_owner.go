package inbox_assignment_usecase

import ia "vozko/domain/inbox_assignment"

type ConversationOwners struct {
	repo ia.Repository
}

func NewConversationOwners(repo ia.Repository) *ConversationOwners {
	return &ConversationOwners{repo: repo}
}

func (o *ConversationOwners) ConversationOwner(workspaceID, entryID, entryType string) (string, error) {
	assignment, err := o.repo.FindByEntry(workspaceID, entryID, entryType)
	if err != nil {
		return "", err
	}
	return assignment.Owner(), nil
}
