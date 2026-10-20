package unofficial_whatsapp_group

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

var (
	ErrGroupNotFound           = errors.New("group not found")
	ErrGroupNotFollowed        = errors.New("this group is not followed")
	ErrCategoryTooLong         = errors.New("group category exceeds the 60 character limit")
	ErrNoConnectedNumber       = errors.New("none of your connected numbers is in this group")
	ErrNumberOutOfScope        = errors.New("that number is not one of your numbers")
	ErrInvalidOrigin           = errors.New("group origin must be managed or external")
	ErrWorkspaceRequired       = errors.New("workspace is required")
	ErrGroupJIDRequired        = errors.New("a group JID is required")
	ErrGroupSubjectNeeded      = errors.New("a group name is required")
	ErrNoGroupConversation     = errors.New("the group has no conversation yet")
	ErrSeriesNeedsManagedGroup = errors.New("only a group created here can start a series")
	ErrSeriesPartlyUpdated     = errors.New("some groups of the series could not be updated")
	ErrNoGroupsToExport        = errors.New("choose at least one group to export")
	ErrNoInviteLink            = errors.New("none of your numbers is admin in this group and no earlier join left an invite link; join it with a link")
	ErrNumberAlreadyInGroup    = errors.New("that number is already in this group")
)

const MaxCategoryRunes = 60

type Origin string

const (
	OriginManaged  Origin = "managed"
	OriginExternal Origin = "external"
)

func (o Origin) Valid() bool {
	return o == OriginManaged || o == OriginExternal
}

type Group struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	JID         string    `json:"jid"`
	Subject     string    `json:"subject"`
	Description string    `json:"description,omitempty"`
	Origin      Origin    `json:"origin"`
	Category    string    `json:"category,omitempty"`
	Announce    bool      `json:"announce"`
	Locked      bool      `json:"locked"`
	MemberCount int       `json:"memberCount"`
	PictureURL  string    `json:"pictureUrl,omitempty"`
	CreatedByID string    `json:"createdById,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`

	PrimaryInstanceID string `json:"primaryInstanceId,omitempty"`

	SeriesID       string `json:"seriesId,omitempty"`
	SeriesSequence int    `json:"seriesSequence,omitempty"`

	FollowedAt      *time.Time `json:"followedAt,omitempty"`
	MembersSyncedAt *time.Time `json:"membersSyncedAt,omitempty"`
	LastActivityAt  *time.Time `json:"lastActivityAt,omitempty"`
}

func (g Group) Followed() bool {
	return g.FollowedAt != nil
}

func (g *Group) Normalize() {
	g.JID = strings.TrimSpace(g.JID)
	g.Subject = strings.TrimSpace(g.Subject)
	g.Description = strings.TrimSpace(g.Description)
	g.Category = strings.TrimSpace(g.Category)
	if !g.Origin.Valid() {
		g.Origin = OriginExternal
	}
}

func (g *Group) Validate() error {
	if strings.TrimSpace(g.WorkspaceID) == "" {
		return ErrWorkspaceRequired
	}
	if !uw.IsGroupJID(g.JID) {
		return uw.ErrNotAGroupJID
	}
	if len([]rune(g.Category)) > MaxCategoryRunes {
		return ErrCategoryTooLong
	}
	return nil
}

func OriginOf(seat *uw.Group, own OwnNumbers) Origin {
	if seat != nil && seat.OwnerJID != "" && own.Contains(PersonKey(uw.PhoneFromJID(seat.OwnerJID), seat.OwnerJID)) {
		return OriginManaged
	}
	return OriginExternal
}

func (g *Group) AbsorbSeat(s *uw.Group) {
	if s == nil {
		return
	}
	if subject := strings.TrimSpace(s.Subject); subject != "" {
		g.Subject = subject
	}
	if s.Description != "" {
		g.Description = s.Description
	}
	g.Announce, g.Locked = s.Announce, s.Locked
	if s.ParticipantCount > 0 {
		g.MemberCount = s.ParticipantCount
	}
}

func NormalizeCategory(raw string) (string, error) {
	category := strings.TrimSpace(raw)
	if len([]rune(category)) > MaxCategoryRunes {
		return "", ErrCategoryTooLong
	}
	return category, nil
}

type Presence struct {
	Seat      *uw.Group `json:"seat"`
	Connected bool      `json:"connected"`
}

func (p Presence) InstanceID() string {
	if p.Seat == nil {
		return ""
	}
	return p.Seat.InstanceID
}

func (p Presence) CanPost() bool {
	return p.Seat != nil && p.Connected && p.Seat.CanPost()
}

func (p Presence) IsAdmin() bool {
	return p.Seat != nil && p.Seat.WeAreAdmin
}

func ChoosePrimary(presences []Presence) (string, bool) {
	candidates := make([]Presence, 0, len(presences))
	for _, p := range presences {
		if p.Seat != nil && p.Seat.InstanceID != "" {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		return "", false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.Connected != b.Connected {
			return a.Connected
		}
		if a.IsAdmin() != b.IsAdmin() {
			return a.IsAdmin()
		}
		if a.CanPost() != b.CanPost() {
			return a.CanPost()
		}
		if !a.Seat.CreatedAt.Equal(b.Seat.CreatedAt) {
			return a.Seat.CreatedAt.Before(b.Seat.CreatedAt)
		}
		return a.Seat.InstanceID < b.Seat.InstanceID
	})
	return candidates[0].Seat.InstanceID, true
}

var seriesSuffix = regexp.MustCompile(`\s*#\d+$`)

type SetupStep string

const (
	SetupDescription SetupStep = "description"
	SetupAdminsOnly  SetupStep = "admins_only"
	SetupPromote     SetupStep = "promote_numbers"
)

type SetupWarning struct {
	Step   SetupStep `json:"step"`
	Reason string    `json:"reason"`
}

func SeriesName(base string, sequence int) string {
	suffix := " #" + strconv.Itoa(sequence)
	name := []rune(strings.TrimSpace(seriesSuffix.ReplaceAllString(strings.TrimSpace(base), "")))
	budget := uw.MaxGroupNameRunes - len([]rune(suffix))
	if len(name) > budget {
		name = name[:budget]
	}
	return strings.TrimSpace(string(name)) + suffix
}

func WhatsAppCreatedAt(seats []*uw.Group) *time.Time {
	var earliest *time.Time
	for _, seat := range seats {
		if seat == nil || seat.GroupCreatedAt == nil || seat.GroupCreatedAt.IsZero() {
			continue
		}
		if earliest == nil || seat.GroupCreatedAt.Before(*earliest) {
			created := *seat.GroupCreatedAt
			earliest = &created
		}
	}
	return earliest
}
