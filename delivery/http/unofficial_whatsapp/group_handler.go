package unofficial_whatsapp

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/http/middleware"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

type GroupHandler struct {
	groups *uwuc.GroupUseCases
}

func NewGroupHandler(groups *uwuc.GroupUseCases) *GroupHandler {
	return &GroupHandler{groups: groups}
}

type groupParticipantDTO struct {
	JID         string  `json:"jid"`
	PhoneNumber string  `json:"phoneNumber,omitempty"`
	Name        string  `json:"name"`
	Role        string  `json:"role"`
	IsAdmin     bool    `json:"isAdmin"`
	ContactID   *string `json:"contactId,omitempty"`
}

type groupDTO struct {
	ID          string `json:"id"`
	JID         string `json:"jid"`
	InstanceID  string `json:"instanceId"`
	Subject     string `json:"subject"`
	Description string `json:"description,omitempty"`
	OwnerJID    string `json:"ownerJid,omitempty"`

	AdminsOnlyMessages bool `json:"adminsOnlyMessages"`
	AdminsOnlyEdit     bool `json:"adminsOnlyEdit"`
	JoinApproval       bool `json:"joinApproval"`
	Ephemeral          bool `json:"ephemeral"`
	IsCommunity        bool `json:"isCommunity"`

	WeAreAdmin bool `json:"weAreAdmin"`
	CanPost    bool `json:"canPost"`

	ParticipantCount int        `json:"participantCount"`
	GroupCreatedAt   *time.Time `json:"groupCreatedAt,omitempty"`
	SyncedAt         *time.Time `json:"syncedAt,omitempty"`

	Participants []groupParticipantDTO `json:"participants,omitempty"`
}

func toGroupDTO(g *uw.Group) groupDTO {
	if g == nil {
		return groupDTO{}
	}
	dto := groupDTO{
		ID:                 g.ID,
		JID:                g.JID,
		InstanceID:         g.InstanceID,
		Subject:            g.Subject,
		Description:        g.Description,
		OwnerJID:           g.OwnerJID,
		AdminsOnlyMessages: g.Announce,
		AdminsOnlyEdit:     g.Locked,
		JoinApproval:       g.JoinApproval,
		Ephemeral:          g.Ephemeral,
		IsCommunity:        g.Community,
		WeAreAdmin:         g.WeAreAdmin,
		CanPost:            g.CanPost(),
		ParticipantCount:   g.ParticipantCount,
		GroupCreatedAt:     g.GroupCreatedAt,
		SyncedAt:           g.SyncedAt,
	}
	for _, p := range g.Participants {
		dto.Participants = append(dto.Participants, groupParticipantDTO{
			JID:         p.JID,
			PhoneNumber: p.PhoneNumber,
			Name:        p.Label(),
			Role:        string(p.Role),
			IsAdmin:     p.IsAdmin(),
			ContactID:   p.ContactID,
		})
	}
	return dto
}

func (h *GroupHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requireWorkspace(w, r)
	if !ok {
		return
	}

	groups, err := h.groups.List(r.Context(), workspaceID, mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err)
		return
	}

	out := make([]groupDTO, 0, len(groups))
	for _, g := range groups {
		out = append(out, toGroupDTO(g))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func (h *GroupHandler) GetGroup(w http.ResponseWriter, r *http.Request) {
	tgt, ok := h.resolveTarget(w, r)
	if !ok {
		return
	}

	group, err := h.groups.Get(r.Context(), tgt.workspaceID, tgt.instanceID, tgt.groupJID,
		r.URL.Query().Get("refresh") == "true")
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toGroupDTO(group))
}

func (h *GroupHandler) GetInviteLink(w http.ResponseWriter, r *http.Request) {
	tgt, ok := h.resolveTarget(w, r)
	if !ok {
		return
	}

	link, err := h.groups.InviteLink(r.Context(), tgt.workspaceID, tgt.instanceID, tgt.groupJID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"inviteLink": link})
}

type updateGroupRequest struct {
	Subject     *string `json:"subject,omitempty"`
	Description *string `json:"description,omitempty"`
	ImageURL    *string `json:"imageUrl,omitempty"`

	AdminsOnlyMessages *bool `json:"adminsOnlyMessages,omitempty"`
	AdminsOnlyEdit     *bool `json:"adminsOnlyEdit,omitempty"`
}

func (h *GroupHandler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	tgt, ok := h.resolveTarget(w, r)
	if !ok {
		return
	}
	workspaceID, instanceID, groupJID := tgt.workspaceID, tgt.instanceID, tgt.groupJID

	var req updateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid request body", nil)
		return
	}

	ctx := r.Context()
	var group *uw.Group
	var err error

	if req.Subject != nil {
		group, err = h.groups.UpdateName(ctx, uwuc.UpdateNameInput{
			WorkspaceID: workspaceID, InstanceID: instanceID,
			GroupJID: groupJID, Name: *req.Subject,
		})
		if err != nil {
			writeDomainError(w, err)
			return
		}
	}
	if req.Description != nil {
		group, err = h.groups.UpdateDescription(ctx, uwuc.UpdateDescriptionInput{
			WorkspaceID: workspaceID, InstanceID: instanceID,
			GroupJID: groupJID, Description: *req.Description,
		})
		if err != nil {
			writeDomainError(w, err)
			return
		}
	}
	if req.ImageURL != nil {
		group, err = h.groups.UpdateImage(ctx, workspaceID, instanceID, groupJID, *req.ImageURL)
		if err != nil {
			writeDomainError(w, err)
			return
		}
	}
	if req.AdminsOnlyMessages != nil || req.AdminsOnlyEdit != nil {
		group, err = h.groups.UpdateSettings(ctx, uwuc.UpdateSettingsInput{
			WorkspaceID: workspaceID, InstanceID: instanceID, GroupJID: groupJID,
			AdminsOnlyMessages: req.AdminsOnlyMessages,
			AdminsOnlyEdit:     req.AdminsOnlyEdit,
		})
		if err != nil {
			writeDomainError(w, err)
			return
		}
	}

	if group == nil {
		group, err = h.groups.Get(ctx, workspaceID, instanceID, groupJID, false)
		if err != nil {
			writeDomainError(w, err)
			return
		}
	}
	response.WriteSuccess(w, http.StatusOK, toGroupDTO(group))
}

type participantsRequest struct {
	Action       string   `json:"action"`
	Participants []string `json:"participants"`
}

func (h *GroupHandler) UpdateParticipants(w http.ResponseWriter, r *http.Request) {
	h.participants(w, r, false)
}

func (h *GroupHandler) RemoveParticipants(w http.ResponseWriter, r *http.Request) {
	h.participants(w, r, true)
}

func (h *GroupHandler) participants(w http.ResponseWriter, r *http.Request, destructive bool) {
	tgt, ok := h.resolveTarget(w, r)
	if !ok {
		return
	}

	var req participantsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid request body", nil)
		return
	}

	action := uw.GroupAction(req.Action)
	if !action.Valid() {
		response.WriteError(w, http.StatusBadRequest, uw.ErrInvalidGroupAction.Error(), nil)
		return
	}
	if action.Destructive() != destructive {
		response.WriteError(w, http.StatusBadRequest,
			"that participant action is not allowed on this endpoint", nil)
		return
	}

	group, err := h.groups.UpdateParticipants(r.Context(), uwuc.UpdateParticipantsInput{
		WorkspaceID:  tgt.workspaceID,
		InstanceID:   tgt.instanceID,
		GroupJID:     tgt.groupJID,
		Action:       action,
		Participants: req.Participants,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toGroupDTO(group))
}

func (h *GroupHandler) LeaveGroup(w http.ResponseWriter, r *http.Request) {
	tgt, ok := h.resolveTarget(w, r)
	if !ok {
		return
	}

	if err := h.groups.Leave(r.Context(), tgt.workspaceID, tgt.instanceID, tgt.groupJID); err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"left": true})
}

func requireWorkspace(w http.ResponseWriter, r *http.Request) (string, bool) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusForbidden, "workspace is required", nil)
		return "", false
	}
	return workspaceID, true
}

type target struct {
	workspaceID string
	instanceID  string
	groupJID    string
}

func (h *GroupHandler) resolveTarget(w http.ResponseWriter, r *http.Request) (target, bool) {
	workspaceID, ok := requireWorkspace(w, r)
	if !ok {
		return target{}, false
	}

	vars := mux.Vars(r)
	if entryID := vars["entryId"]; entryID != "" {
		instanceID, groupJID, err := h.groups.ResolveConversation(r.Context(), workspaceID, entryID)
		if err != nil {
			writeDomainError(w, err)
			return target{}, false
		}
		return target{workspaceID: workspaceID, instanceID: instanceID, groupJID: groupJID}, true
	}
	return target{
		workspaceID: workspaceID,
		instanceID:  vars["id"],
		groupJID:    vars["groupJid"],
	}, true
}
