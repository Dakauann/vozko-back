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

// GroupHandler serves the group panel.
//
// Separate from Handler because the two have different blast radii: the instance
// endpoints manage OUR side of a connection, while these act inside the
// customer's own WhatsApp groups — renaming them, evicting members, leaving. The
// routes are scoped accordingly (see RegisterGroupRoutes), and keeping the two
// handlers apart makes that split visible rather than a matter of remembering
// which method got which guard.
type GroupHandler struct {
	groups *uwuc.GroupUseCases
}

func NewGroupHandler(groups *uwuc.GroupUseCases) *GroupHandler {
	return &GroupHandler{groups: groups}
}

// ---------------------------------------------------------------- DTOs

type groupParticipantDTO struct {
	JID         string `json:"jid"`
	PhoneNumber string `json:"phoneNumber,omitempty"`
	Name        string `json:"name"`
	Role        string `json:"role"`
	IsAdmin     bool   `json:"isAdmin"`
	// ContactID is present only for members we already know from a direct chat,
	// so the roster can offer "open the conversation" for them and nothing for
	// the rest. Absent is the normal case in a large group.
	ContactID *string `json:"contactId,omitempty"`
}

// groupDTO is the wire shape of a group.
//
// InviteLink is deliberately absent: it is a credential — anyone holding it can
// join the customer's group — and it is served by its own endpoint so it appears
// when an operator asks for it rather than in every list payload, browser cache
// and screenshot.
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

	// WeAreAdmin drives every admin control in the panel, and CanPost drives the
	// composer. Both are derived here rather than in the browser so the UI and
	// the send path cannot disagree about whether a reply is possible.
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

// ---------------------------------------------------------------- reads

// ListGroups returns the groups a connected number belongs to.
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

// GetGroup returns one group with its roster.
//
// `?refresh=true` forces a re-read past both caches. It is a separate opt-in
// rather than the default because every refresh is a provider call on the
// customer's number, and a panel that re-read on every render would spend that
// budget on a screen whose content changes weekly.
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

// GetInviteLink returns the group's join link.
//
// Admin-only and never cached. The link is a standing credential: anyone who
// receives it can join the customer's group without approval.
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

// ---------------------------------------------------------------- mutations

type updateGroupRequest struct {
	Subject     *string `json:"subject,omitempty"`
	Description *string `json:"description,omitempty"`
	// ImageURL sets the picture; an explicit empty string removes it. A pointer
	// so "not submitted" and "remove it" stay distinguishable.
	ImageURL *string `json:"imageUrl,omitempty"`

	AdminsOnlyMessages *bool `json:"adminsOnlyMessages,omitempty"`
	AdminsOnlyEdit     *bool `json:"adminsOnlyEdit,omitempty"`
}

// UpdateGroup applies whichever fields were submitted.
//
// Every field is a pointer and absent means "leave it alone". A form that posted
// its whole state would otherwise re-open an announce-only group every time
// somebody corrected a typo in its name.
//
// Each change is applied in turn and the first failure returns, so a partially
// applied edit is reported as a failure rather than as a success that quietly
// did half the work. The response carries a fresh read, so the panel renders
// what WhatsApp actually holds rather than what was typed.
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
		// Nothing was submitted. A read is the honest answer: the caller gets the
		// current state rather than a 400 for a request that asked for no change.
		group, err = h.groups.Get(ctx, workspaceID, instanceID, groupJID, false)
		if err != nil {
			writeDomainError(w, err)
			return
		}
	}
	response.WriteSuccess(w, http.StatusOK, toGroupDTO(group))
}

type participantsRequest struct {
	Action string `json:"action"`
	// Participants are phone numbers or JIDs; the adapter normalizes them.
	Participants []string `json:"participants"`
}

// UpdateParticipants adds, promotes, demotes or approves members.
//
// The destructive verbs — remove and reject — are NOT reachable here. They are
// routed separately under ActionDelete, because evicting a customer from a group
// is a different privilege from inviting one, and an attendant who may do the
// second should not thereby be able to do the first.
func (h *GroupHandler) UpdateParticipants(w http.ResponseWriter, r *http.Request) {
	h.participants(w, r, false)
}

// RemoveParticipants evicts members or rejects their join requests.
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
	// The route decides which class of verb it accepts, and the body cannot
	// widen it. Without this check the additive route would happily perform a
	// removal for anyone holding only ActionUpdate — the permission split above
	// would be advisory.
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

// LeaveGroup exits the group.
//
// The conversation is deliberately left in place. The transcript is history, and
// leaving a group does not mean the messages stopped having been exchanged — an
// operator still needs to read what was said.
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

// requireWorkspace is the guard every endpoint here shares. Written once because
// a group endpoint that forgot it would act on another tenant's WhatsApp.
func requireWorkspace(w http.ResponseWriter, r *http.Request) (string, bool) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusForbidden, "workspace is required", nil)
		return "", false
	}
	return workspaceID, true
}

// target is the (instance, group) pair an endpoint acts on.
type target struct {
	workspaceID string
	instanceID  string
	groupJID    string
}

// resolveTarget accepts either route shape and yields the same pair.
//
// Two shapes exist because two callers ask different questions with different
// ids in hand:
//
//   - /instances/{id}/groups/{groupJid} — the instance settings screen, which
//     knows the number and is browsing the groups it belongs to;
//   - /conversations/{entryId}/group — the CRM, which knows only the
//     conversation an operator has open.
//
// The alternative was leaking a channel-specific `group_jid` into InboxEntry,
// the channel-neutral shape every list in the CRM renders. One extra lookup here
// is much cheaper than that.
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
