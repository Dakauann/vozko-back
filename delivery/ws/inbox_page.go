package ws

import (
	"vozko/domain/conversation"
	"vozko/domain/personalinbox"
)

type inboxView struct {
	viewMode             string
	userID               string
	workspaceID          string
	departmentID         string
	campaignType         string
	whatsAppCampaignType string
	containerKind        string
	campaignID           string
	campaignWorkspaceID  string
	conversationStatus   string
	isAdmin              bool
	seq                  uint64
}

func inboxViewOf(conn *WSConnection) inboxView {
	return inboxView{
		viewMode:             conn.ViewMode,
		userID:               conn.UserID,
		workspaceID:          conn.WorkspaceID,
		departmentID:         conn.DepartmentID,
		campaignType:         conn.CampaignType,
		whatsAppCampaignType: conn.WhatsAppCampaignType,
		containerKind:        conn.ContainerKind,
		campaignID:           conn.CampaignID,
		campaignWorkspaceID:  conn.CampaignWorkspaceID,
		conversationStatus:   conn.ConversationStatus,
		isAdmin:              conn.IsAdmin,
		seq:                  conn.viewSeq,
	}
}

func (v inboxView) global() bool { return v.viewMode == "global" }

func (v inboxView) searchInput(page, pageSize int) conversation.SearchInboxInput {
	input := conversation.SearchInboxInput{
		UserID:               v.userID,
		SelectedDepartmentID: v.departmentID,
		CampaignType:         v.campaignType,
		AssignedUserID:       v.userID,
		IsAdmin:              v.isAdmin,
		Personal:             personalinbox.ViewInbox,
		Page:                 page,
		PageSize:             pageSize,
	}
	switch {
	case v.conversationStatus != "":
		input.WhatsAppCampaignType = v.whatsAppCampaignType
		input.ContainerKind = conversation.ContainerKind(v.containerKind)
		input.ConversationStatus = conversation.ConversationStatus(v.conversationStatus)
		if v.global() {
			input.WorkspaceID = v.workspaceID
		} else {
			input.CampaignID = v.campaignID
		}
	case v.global():
		input.WhatsAppCampaignType = v.whatsAppCampaignType
		input.WorkspaceID = v.workspaceID
	default:
		input.CampaignID = v.campaignID
		input.ContainerKind = conversation.ContainerKind(v.containerKind)
		input.WorkspaceID = v.campaignWorkspaceID
	}
	return input
}

func (h *ConversationHub) loadInboxPage(v inboxView, page, pageSize int) (InboxPayload, error) {
	entries, totalItems, err := h.inboxService.SearchInbox(v.userID, v.searchInput(page, pageSize))
	if err != nil {
		return InboxPayload{}, err
	}

	payload := InboxPayload{
		Entries:       entries,
		Page:          page,
		PageSize:      pageSize,
		TotalItems:    totalItems,
		TotalPages:    totalPagesOf(totalItems, pageSize),
		PersonalMarks: h.pageMarks(v.userID, entries),
	}
	if v.global() {
		payload.StageCounts, _ = h.inboxService.GetInboxStageCounts(v.workspaceID, "", v.campaignType)
		payload.ConversationStatusCounts, _ = h.inboxService.GetConversationStatusCounts(v.workspaceID, "", v.campaignType)
	} else {
		payload.StageCounts, _ = h.inboxService.GetInboxStageCounts(v.campaignWorkspaceID, v.campaignID, v.campaignType)
		payload.ConversationStatusCounts, _ = h.inboxService.GetConversationStatusCounts("", v.campaignID, v.campaignType)
	}
	payload.AvailableLabels, _ = h.inboxService.GetAvailableLabels(v.workspaceID)
	return payload, nil
}

func totalPagesOf(totalItems int64, pageSize int) int {
	if totalItems <= 0 || pageSize <= 0 {
		return 0
	}
	return int((totalItems + int64(pageSize) - 1) / int64(pageSize))
}
