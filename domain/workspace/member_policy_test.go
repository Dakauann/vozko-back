package workspace

import (
	"errors"
	"testing"
)

var (
	ownerMember  = &Member{UserID: "u-owner", Role: RoleOwner}
	adminMember  = &Member{UserID: "u-admin", Role: RoleAdmin}
	plainMember  = &Member{UserID: "u-ana", Role: RoleMember}
	anotherPlain = &Member{UserID: "u-bia", Role: RoleMember}
)

func TestActorsWhoManageMembers(t *testing.T) {
	cases := map[string]struct {
		actor Actor
		want  error
	}{
		"platform admin": {Actor{PlatformAdmin: true}, nil},
		"owner":          {Actor{Role: RoleOwner}, nil},
		"admin":          {Actor{Role: RoleAdmin}, nil},
		"member":         {Actor{Role: RoleMember}, ErrInsufficientPermissions},
	}
	for label, c := range cases {
		if err := c.actor.ManagesMembers(); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", label, err)
		}
	}
}

func TestReassigningARoleProtectsOwnerAndAdmins(t *testing.T) {
	cases := map[string]struct {
		actor  Actor
		target *Member
		want   error
	}{
		"owner is never reassigned":    {Actor{Role: RoleOwner}, ownerMember, ErrCannotChangeOwnerRole},
		"admin cannot touch an admin":  {Actor{Role: RoleAdmin}, adminMember, ErrInsufficientPermissions},
		"owner can touch an admin":     {Actor{Role: RoleOwner}, adminMember, nil},
		"platform admin acts as owner": {Actor{PlatformAdmin: true}, adminMember, nil},
		"admin can touch a member":     {Actor{Role: RoleAdmin}, plainMember, nil},
	}
	for label, c := range cases {
		if err := c.actor.CanReassign(c.target); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", label, err)
		}
	}
}

func TestRemovingAMember(t *testing.T) {
	cases := map[string]struct {
		actor  Actor
		target *Member
		want   error
	}{
		"nobody removes the owner":        {Actor{Role: RoleAdmin, UserID: "u-admin"}, ownerMember, ErrCannotRemoveOwner},
		"the owner cannot leave either":   {Actor{Role: RoleOwner, UserID: "u-owner"}, ownerMember, ErrCannotRemoveOwner},
		"an admin cannot remove an admin": {Actor{Role: RoleAdmin, UserID: "u-other-admin"}, adminMember, ErrInsufficientPermissions},
		"a member cannot remove an admin": {Actor{Role: RoleMember, UserID: "u-ana"}, adminMember, ErrInsufficientPermissions},
		"the owner removes an admin":      {Actor{Role: RoleOwner, UserID: "u-owner"}, adminMember, nil},
		"a platform admin removes one":    {Actor{PlatformAdmin: true}, adminMember, nil},
		"an admin can leave":              {Actor{Role: RoleAdmin, UserID: "u-admin"}, adminMember, nil},
		"a member can leave":              {Actor{Role: RoleMember, UserID: "u-ana"}, plainMember, nil},
		"removing a member":               {Actor{Role: RoleMember, UserID: "u-ana"}, anotherPlain, nil},
	}
	for label, c := range cases {
		if err := c.actor.CanRemove(c.target); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", label, err)
		}
	}
}

func TestEditingPermissions(t *testing.T) {
	if err := CanEditPermissions("u-ana", plainMember); !errors.Is(err, ErrCannotModifySelf) {
		t.Fatalf("self edit: %v", err)
	}
	if err := CanEditPermissions("u-ana", adminMember); !errors.Is(err, ErrCannotChangeOwnerRole) {
		t.Fatalf("admin edit: %v", err)
	}
	if err := CanEditPermissions("u-ana", anotherPlain); err != nil {
		t.Fatalf("member edit: %v", err)
	}
}

func TestInvitingAsAdminNeedsSomeoneWhoManagesMembers(t *testing.T) {
	if err := (Actor{Role: RoleMember}).CanInviteAs(RoleAdmin); !errors.Is(err, ErrInsufficientPermissions) {
		t.Fatalf("member invited an admin: %v", err)
	}
	if err := (Actor{Role: RoleMember}).CanInviteAs(RoleMember); err != nil {
		t.Fatalf("member with members:create inviting a member: %v", err)
	}
	if err := (Actor{Role: RoleAdmin}).CanInviteAs(RoleAdmin); err != nil {
		t.Fatalf("admin inviting an admin: %v", err)
	}
	if err := (Actor{PlatformAdmin: true}).CanInviteAs(RoleOwner); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("owner invite: %v", err)
	}
}
