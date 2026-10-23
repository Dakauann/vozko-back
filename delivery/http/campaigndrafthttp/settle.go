package campaigndrafthttp

import (
	"log"
	"net/http"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/dealautomation"
	"vozko/domain/shared"
	"vozko/domain/user"
	"vozko/infra/http/middleware"
)

const (
	WarningDealAutomation = "deal_automation_not_applied"
	WarningButtonRoutes   = "button_routes_not_applied"
)

type DealAutomation interface {
	Set(by shared.Person, workspaceID string, channel dealautomation.Channel, containerID, pipelineID string) (*dealautomation.Setting, error)
}

func ApplyDealAutomation(r *http.Request, deals DealAutomation, entryType shared.EntryType, campaignID, pipelineID string) []string {
	pipelineID = strings.TrimSpace(pipelineID)
	if pipelineID == "" {
		return nil
	}
	claims := middleware.GetClaims(r)
	if deals == nil || claims == nil {
		return []string{WarningDealAutomation}
	}
	person := shared.Person{UserID: claims.UserID, SystemAdmin: claims.Role == string(user.RoleAdmin)}
	channel := dealautomation.Channel{EntryType: entryType, Kind: conversation.ContainerKindCampaign}
	if _, err := deals.Set(person, middleware.GetWorkspaceID(r), channel, campaignID, pipelineID); err != nil {
		log.Printf("[campaign-drafts] deal automation for campaign %s: %v", campaignID, err)
		return []string{WarningDealAutomation}
	}
	return nil
}
