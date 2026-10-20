package unofficial_whatsapp

import (
	"time"

	"vozko/delivery/http/response"
	"vozko/domain/unofficial_whatsapp_group"
)

type updateWorkspaceGroupRequest struct {
	ApplyToSeries      bool    `json:"applyToSeries,omitempty"`
	Category           *string `json:"category,omitempty"`
	Subject            *string `json:"subject,omitempty"`
	Description        *string `json:"description,omitempty"`
	ImageURL           *string `json:"imageUrl,omitempty"`
	AdminsOnlyMessages *bool   `json:"adminsOnlyMessages,omitempty"`
	AdminsOnlyEdit     *bool   `json:"adminsOnlyEdit,omitempty"`
}

type membersToLeadsRequest struct {
	PersonKeys []string `json:"personKeys"`
}

type leaveWorkspaceGroupRequest struct {
	InstanceIDs []string `json:"instanceIds"`
}

type createWorkspaceGroupRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	InstanceIDs []string `json:"instanceIds"`
	AdminsOnly  bool     `json:"adminsOnly"`
	SkipFollow  bool     `json:"skipFollow"`
}

type inviteRequest struct {
	InviteLink string `json:"inviteLink"`
}

type joinWorkspaceGroupRequest struct {
	InviteLink  string   `json:"inviteLink"`
	InstanceIDs []string `json:"instanceIds"`
	Follow      bool     `json:"follow"`
	Category    string   `json:"category"`
}

type addGroupNumbersRequest struct {
	InstanceIDs []string `json:"instanceIds"`
}

type analysisRequest struct {
	GroupID          string                                   `json:"groupId"`
	Name             string                                   `json:"name"`
	Prompt           string                                   `json:"prompt"`
	Trigger          unofficial_whatsapp_group.Trigger        `json:"trigger"`
	MessagesPerCycle int                                      `json:"messagesPerCycle"`
	ScheduleHour     int                                      `json:"scheduleHour"`
	ScheduleMinute   int                                      `json:"scheduleMinute"`
	TrackedFields    []unofficial_whatsapp_group.TrackedField `json:"trackedFields"`
}

type analysisPatchRequest struct {
	Name             *string                                   `json:"name,omitempty"`
	Prompt           *string                                   `json:"prompt,omitempty"`
	Status           *unofficial_whatsapp_group.AnalysisStatus `json:"status,omitempty"`
	Trigger          *unofficial_whatsapp_group.Trigger        `json:"trigger,omitempty"`
	MessagesPerCycle *int                                      `json:"messagesPerCycle,omitempty"`
	ScheduleHour     *int                                      `json:"scheduleHour,omitempty"`
	ScheduleMinute   *int                                      `json:"scheduleMinute,omitempty"`
	TrackedFields    *[]unofficial_whatsapp_group.TrackedField `json:"trackedFields,omitempty"`
}

type membershipResponse struct {
	Data     []*unofficial_whatsapp_group.MembershipStat `json:"data"`
	Meta     response.PaginationMeta                     `json:"meta"`
	Summary  unofficial_whatsapp_group.MembershipSummary `json:"summary"`
	From     time.Time                                   `json:"from"`
	To       time.Time                                   `json:"to"`
	Timezone string                                      `json:"timezone"`
}

type manageMembersRequest struct {
	Action       string   `json:"action"`
	Participants []string `json:"participants"`
}

type runHistoryResponse struct {
	Data              []*unofficial_whatsapp_group.Run                `json:"data"`
	Meta              response.PaginationMeta                         `json:"meta"`
	ParticipantGroups map[string][]unofficial_whatsapp_group.GroupRef `json:"participantGroups"`
}
