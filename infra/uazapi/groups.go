package uazapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	uw "vozko/domain/unofficial_whatsapp"
)

var _ uw.GroupAPI = (*Client)(nil)

type groupParticipantResponse struct {
	JID          string `json:"JID"`
	LID          string `json:"LID"`
	PhoneNumber  string `json:"PhoneNumber"`
	IsAdmin      bool   `json:"IsAdmin"`
	IsSuperAdmin bool   `json:"IsSuperAdmin"`
	DisplayName  string `json:"DisplayName"`
}

type groupResponse struct {
	JID      string `json:"JID"`
	OwnerJID string `json:"OwnerJID"`
	Name     string `json:"Name"`
	Topic    string `json:"Topic"`

	IsLocked               bool `json:"IsLocked"`
	IsAnnounce             bool `json:"IsAnnounce"`
	IsEphemeral            bool `json:"IsEphemeral"`
	DisappearingTimer      int  `json:"DisappearingTimer"`
	IsJoinApprovalRequired bool `json:"IsJoinApprovalRequired"`
	IsParent               bool `json:"IsParent"`

	GroupCreated string `json:"GroupCreated"`

	OwnerIsAdmin        bool `json:"OwnerIsAdmin"`
	OwnerCanSendMessage bool `json:"OwnerCanSendMessage"`

	Participants []groupParticipantResponse `json:"Participants"`
	InviteLink   string                     `json:"invite_link"`
}

func (r groupResponse) toDomain() *uw.Group {
	g := &uw.Group{
		JID:              r.JID,
		Subject:          r.Name,
		Description:      r.Topic,
		OwnerJID:         jidString(r.OwnerJID),
		Announce:         r.IsAnnounce,
		Locked:           r.IsLocked,
		JoinApproval:     r.IsJoinApprovalRequired,
		Ephemeral:        r.IsEphemeral,
		DisappearingSecs: r.DisappearingTimer,
		Community:        r.IsParent,
		WeAreAdmin:       r.OwnerIsAdmin,
		WeCanSend:        r.OwnerCanSendMessage || r.OwnerIsAdmin,
		GroupCreatedAt:   parseTime(r.GroupCreated),
		InviteLink:       r.InviteLink,
	}
	for _, p := range r.Participants {
		role := uw.GroupRoleMember
		switch {
		case p.IsSuperAdmin:
			role = uw.GroupRoleSuperAdmin
		case p.IsAdmin:
			role = uw.GroupRoleAdmin
		}
		g.Participants = append(g.Participants, uw.GroupParticipant{
			JID:         jidString(p.JID),
			LID:         jidString(p.LID),
			PhoneNumber: p.PhoneNumber,
			DisplayName: p.DisplayName,
			Role:        role,
		})
	}
	g.Normalize()
	return g
}

func (c *Client) GroupInfo(
	ctx context.Context,
	ref uw.InstanceRef,
	groupJID string,
	opts uw.GroupInfoOptions,
) (*uw.Group, error) {
	groupJID = strings.TrimSpace(groupJID)
	if !uw.IsGroupJID(groupJID) {
		return nil, uw.ErrNotAGroupJID
	}

	var resp groupResponse
	err := c.instanceCall(ctx, ref, http.MethodPost, "/group/info", map[string]any{
		"groupjid":                groupJID,
		"getInviteLink":           opts.WithInviteLink,
		"force":                   opts.Force,
		"getRequestsParticipants": false,
	}, &resp)
	if err != nil {
		return nil, err
	}
	group := resp.toDomain()
	if group.JID == "" {
		group.JID = groupJID
	}
	return group, nil
}

const defaultGroupPageSize = 100

func (c *Client) ListGroups(ctx context.Context, ref uw.InstanceRef, withParticipants bool) ([]*uw.Group, error) {
	var out []*uw.Group
	for offset := 0; ; offset += c.groupPageSize {
		var resp struct {
			Groups []groupResponse `json:"groups"`
		}
		err := c.instanceCall(ctx, ref, http.MethodPost, "/group/list", map[string]any{
			"limit":          c.groupPageSize,
			"offset":         offset,
			"noParticipants": !withParticipants,
		}, &resp)
		if err != nil {
			return nil, err
		}
		for _, item := range resp.Groups {
			if group := item.toDomain(); group.JID != "" {
				out = append(out, group)
			}
		}
		if len(resp.Groups) < c.groupPageSize {
			return out, nil
		}
	}
}

func (c *Client) CreateGroup(ctx context.Context, ref uw.InstanceRef, name string, participants []string) (*uw.Group, error) {
	if err := uw.ValidateGroupName(name); err != nil {
		return nil, err
	}
	var resp struct {
		Group groupResponse `json:"group"`
	}
	err := c.instanceCall(ctx, ref, http.MethodPost, "/group/create", map[string]any{
		"name":         strings.TrimSpace(name),
		"participants": normalizeParticipants(participants),
	}, &resp)
	if err != nil {
		return nil, err
	}
	return groupFrom(resp.Group)
}

func (c *Client) JoinGroup(ctx context.Context, ref uw.InstanceRef, inviteLink string) (*uw.Group, error) {
	return c.inviteCall(ctx, ref, "/group/join", inviteLink)
}

func (c *Client) InviteInfo(ctx context.Context, ref uw.InstanceRef, inviteLink string) (*uw.Group, error) {
	return c.inviteCall(ctx, ref, "/group/inviteInfo", inviteLink)
}

func (c *Client) inviteCall(ctx context.Context, ref uw.InstanceRef, path, inviteLink string) (*uw.Group, error) {
	var resp struct {
		Group groupResponse `json:"group"`
	}
	err := c.instanceCall(ctx, ref, http.MethodPost, path, map[string]any{
		"invitecode": strings.TrimSpace(inviteLink),
	}, &resp)
	if err != nil {
		return nil, err
	}
	return groupFrom(resp.Group)
}

func groupFrom(r groupResponse) (*uw.Group, error) {
	group := r.toDomain()
	if !uw.IsGroupJID(group.JID) {
		return nil, uw.ErrGroupNotFound
	}
	return group, nil
}

func (c *Client) UpdateGroupName(ctx context.Context, ref uw.InstanceRef, groupJID, name string) error {
	if err := uw.ValidateGroupName(name); err != nil {
		return err
	}
	return c.groupCall(ctx, ref, "/group/updateName", groupJID, map[string]any{
		"name": strings.TrimSpace(name),
	})
}

func (c *Client) UpdateGroupDescription(ctx context.Context, ref uw.InstanceRef, groupJID, description string) error {
	if err := uw.ValidateGroupTopic(description); err != nil {
		return err
	}
	return c.groupCall(ctx, ref, "/group/updateDescription", groupJID, map[string]any{
		"description": strings.TrimSpace(description),
	})
}

func (c *Client) UpdateGroupImage(ctx context.Context, ref uw.InstanceRef, groupJID, image string) error {
	image = strings.TrimSpace(image)
	if image == "" {
		image = "remove"
	}
	return c.groupCall(ctx, ref, "/group/updateImage", groupJID, map[string]any{
		"image": image,
	})
}

func (c *Client) UpdateParticipants(ctx context.Context, ref uw.InstanceRef, in uw.UpdateParticipantsInput) error {
	if !in.Action.Valid() {
		return uw.ErrInvalidGroupAction
	}
	participants := normalizeParticipants(in.Participants)
	if len(participants) == 0 {
		return uw.ErrNoParticipants
	}
	return c.groupCall(ctx, ref, "/group/updateParticipants", in.GroupJID, map[string]any{
		"action":       string(in.Action),
		"participants": participants,
	})
}

func (c *Client) UpdateAnnounce(ctx context.Context, ref uw.InstanceRef, groupJID string, adminsOnly bool) error {
	return c.groupCall(ctx, ref, "/group/updateAnnounce", groupJID, map[string]any{
		"announce": adminsOnly,
	})
}

func (c *Client) UpdateLocked(ctx context.Context, ref uw.InstanceRef, groupJID string, adminsOnly bool) error {
	return c.groupCall(ctx, ref, "/group/updateLocked", groupJID, map[string]any{
		"locked": adminsOnly,
	})
}

func (c *Client) LeaveGroup(ctx context.Context, ref uw.InstanceRef, groupJID string) error {
	return c.groupCall(ctx, ref, "/group/leave", groupJID, nil)
}

func (c *Client) groupCall(
	ctx context.Context,
	ref uw.InstanceRef,
	path, groupJID string,
	extra map[string]any,
) error {
	groupJID = strings.TrimSpace(groupJID)
	if !uw.IsGroupJID(groupJID) {
		return uw.ErrNotAGroupJID
	}
	body := map[string]any{"groupjid": groupJID}
	for k, v := range extra {
		body[k] = v
	}
	return c.instanceCall(ctx, ref, http.MethodPost, path, body, nil)
}

func normalizeParticipants(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if idx := strings.Index(item, "@"); idx >= 0 {
			item = item[:idx]
		}
		if idx := strings.Index(item, ":"); idx >= 0 {
			item = item[:idx]
		}
		number := uw.NormalizePhone(item)
		if number == "" {
			continue
		}
		if _, dup := seen[number]; dup {
			continue
		}
		seen[number] = struct{}{}
		out = append(out, number)
	}
	return out
}

func (c *Client) GroupInviteLink(ctx context.Context, ref uw.InstanceRef, groupJID string) (string, error) {
	group, err := c.GroupInfo(ctx, ref, groupJID, uw.GroupInfoOptions{WithInviteLink: true})
	if err != nil {
		return "", err
	}
	if group.InviteLink == "" {
		return "", fmt.Errorf("uazapi: the host returned no invite link for %s", groupJID)
	}
	return group.InviteLink, nil
}
