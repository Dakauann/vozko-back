package inbox_assignment_usecase

import (
	"fmt"
	"log"

	ia "vozko/domain/inbox_assignment"
)

func (s *AssignmentService) SetPortfolio(p ia.PortfolioHolders) { s.portfolio = p }

func (s *AssignmentService) portfolioHolder(workspaceID, entryID, entryType, departmentID string) (string, error) {
	if s.portfolio == nil {
		return "", nil
	}
	holder, err := s.portfolio.HolderOfConversation(workspaceID, entryID, entryType)
	if err != nil {
		return "", fmt.Errorf("carteira of %s (%s): %w", entryID, entryType, err)
	}
	if holder == "" {
		return "", nil
	}
	if !ia.CanReceivePortfolio(s.receivers, holder, workspaceID, departmentID) {
		log.Printf("[InboxAssignment] entry %s (%s) is in the carteira of %s, who cannot receive it in department %q; using the roulette", entryID, entryType, holder, departmentID)
		return "", nil
	}
	return holder, nil
}
