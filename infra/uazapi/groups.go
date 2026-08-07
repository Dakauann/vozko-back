package uazapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	uw "vozko/domain/unofficial_whatsapp"
)

// The group management surface.
//
// Two vendor quirks shape this file:
//
//  1. The group endpoints answer in Go-style PascalCase (`JID`, `Name`,
//     `Participants`) while every other endpoint answers in camelCase. That is
//     the vendor leaking its internal structs, and it is exactly the kind of
//     detail that must not reach the domain.
//  2. `/group/info` returns no picture. The group's avatar comes from
//     `/chat/details` with the group's JID, which is the same call a contact's
//     avatar comes from — so the caller composes the two rather than this file
//     inventing a merged endpoint.

// Compile-time proof that the client satisfies the group port.
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

	// OwnerIsAdmin and OwnerCanSendMessage describe OUR connected number, not
	// the group's creator, despite the names. The vendor's own field
	// descriptions say so ("verifica se VOCÊ é administrador"), and reading them
	// as being about the owner would gate every admin control on the wrong
	// person.
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
		// An admin can always post, including in an announce-only group, and the
		// vendor does not always set the send flag for them. Defaulting a
		// non-admin to the flag alone would silence an admin's composer in every
		// announce group they run.
		WeCanSend:      r.OwnerCanSendMessage || r.OwnerIsAdmin,
		GroupCreatedAt: parseTime(r.GroupCreated),
		InviteLink:     r.InviteLink,
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

// GroupInfo reads one group's metadata and roster.
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
		"groupjid":      groupJID,
		"getInviteLink": opts.WithInviteLink,
		"force":         opts.Force,
		// Pending join requests are a separate screen with its own permission,
		// and fetching them costs an extra round trip on the host. Never asked
		// for by a background sync.
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

// ListGroups enumerates every group the connected number belongs to.
//
// withParticipants is false for the normal call. A workspace in two hundred
// groups does not need two hundred rosters to render a list of names, and asking
// for them turns a bootstrap into a payload measured in megabytes.
func (c *Client) ListGroups(ctx context.Context, ref uw.InstanceRef, withParticipants bool) ([]*uw.Group, error) {
	path := "/group/list?noparticipants=true"
	if withParticipants {
		path = "/group/list?noparticipants=false"
	}

	var resp struct {
		Groups []groupResponse `json:"groups"`
	}
	if err := c.instanceCall(ctx, ref, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}

	out := make([]*uw.Group, 0, len(resp.Groups))
	for _, item := range resp.Groups {
		group := item.toDomain()
		if group.JID == "" {
			continue
		}
		out = append(out, group)
	}
	return out, nil
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

// UpdateGroupImage sets the picture from a URL or a base64 payload.
//
// An empty image REMOVES it, which is the vendor's documented behaviour behind
// the literal "remove". Spelling that literal here rather than making callers
// know it keeps the vendor's vocabulary inside this package.
func (c *Client) UpdateGroupImage(ctx context.Context, ref uw.InstanceRef, groupJID, image string) error {
	image = strings.TrimSpace(image)
	if image == "" {
		image = "remove"
	}
	return c.groupCall(ctx, ref, "/group/updateImage", groupJID, map[string]any{
		"image": image,
	})
}

// UpdateParticipants adds, removes, promotes, demotes, approves or rejects.
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

// UpdateAnnounce restricts posting to admins.
func (c *Client) UpdateAnnounce(ctx context.Context, ref uw.InstanceRef, groupJID string, adminsOnly bool) error {
	return c.groupCall(ctx, ref, "/group/updateAnnounce", groupJID, map[string]any{
		"announce": adminsOnly,
	})
}

// UpdateLocked restricts editing the group's name, picture and topic to admins.
func (c *Client) UpdateLocked(ctx context.Context, ref uw.InstanceRef, groupJID string, adminsOnly bool) error {
	return c.groupCall(ctx, ref, "/group/updateLocked", groupJID, map[string]any{
		"locked": adminsOnly,
	})
}

func (c *Client) LeaveGroup(ctx context.Context, ref uw.InstanceRef, groupJID string) error {
	return c.groupCall(ctx, ref, "/group/leave", groupJID, nil)
}

// groupCall is the shared shape of every group mutation: validate the JID, merge
// it into the body under the vendor's key, POST.
//
// Centralised because the JID check is the one thing that must never be skipped:
// every one of these endpoints acts on a real group in the customer's WhatsApp,
// and a malformed id is far better refused here than interpreted by the host.
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

// normalizeParticipants reduces whatever the UI collected to bare numbers.
//
// The vendor accepts either a number or a full JID, but mixing the forms within
// one request has produced partial applications, so they are normalized to one.
// A JID is reduced to its user part; anything already numeric is kept.
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

// GroupInviteLink is a convenience read for the one field GroupInfo omits by
// default. Separate because it is a credential: anyone holding the link can
// join, so it is fetched on explicit request and never cached.
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
