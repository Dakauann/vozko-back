package unofficial_whatsapp

import (
	"context"
	"errors"
	"log"

	"vozko/domain/media"
	uw "vozko/domain/unofficial_whatsapp"
)

// The operator-facing group surface: read a group, and change it.
//
// Every mutation here acts on a real group inside the customer's own WhatsApp —
// renaming it, evicting a member, leaving it — and none of them is undoable from
// our side. Three guards apply to all of them, in this order:
//
//  1. the instance belongs to the caller's workspace;
//  2. the group belongs to that instance;
//  3. our connected number is actually an admin.
//
// The third is not politeness. Without it the UI offers controls that fail at
// the provider, and an operator learns they are not an admin from a red toast
// after the click rather than from a disabled button before it.

var ErrGroupNotInWorkspace = errors.New("that group does not belong to this workspace")

// GroupUseCases serves the group panel.
//
// It holds the metadata reader rather than duplicating it, so an operator's
// refresh and a webhook-triggered re-read go through exactly the same code and
// cannot disagree about what a synced group looks like.
type GroupUseCases struct {
	instances     uw.InstanceRepository
	servers       uw.ServerRepository
	conversations uw.ConversationRepository
	groups        uw.GroupRepository
	groupAPI      uw.GroupAPI
	metadata      groupMetadata
}

// GroupUseCaseDeps groups the dependencies.
type GroupUseCaseDeps struct {
	Instances     uw.InstanceRepository
	Servers       uw.ServerRepository
	Contacts      uw.ContactRepository
	Conversations uw.ConversationRepository
	Groups        uw.GroupRepository
	GroupAPI      uw.GroupAPI
	Messaging     uw.MessagingAPI
	Assets        uw.RemoteAssetFetcher
	FileStorage   media.FileStorage
}

func NewGroupUseCases(d GroupUseCaseDeps) *GroupUseCases {
	profiles := subjectProfile{
		contacts:    d.Contacts,
		servers:     d.Servers,
		messaging:   d.Messaging,
		assets:      d.Assets,
		fileStorage: d.FileStorage,
		ttl:         profileTTL,
		now:         nowUTC,
	}
	return &GroupUseCases{
		instances:     d.Instances,
		servers:       d.Servers,
		conversations: d.Conversations,
		groups:        d.Groups,
		groupAPI:      d.GroupAPI,
		metadata: groupMetadata{
			groups:   d.Groups,
			contacts: d.Contacts,
			servers:  d.Servers,
			groupAPI: d.GroupAPI,
			profiles: profiles,
			ttl:      uw.GroupMetadataTTL,
			now:      nowUTC,
		},
	}
}

// ResolveConversation maps an inbox conversation onto the group it is with.
//
// It exists because the CRM addresses conversations, not WhatsApp JIDs. The
// inbox holds an entry id and an entry type; the group endpoints need an
// instance and a group JID, and both are already on the conversation row. The
// alternative was leaking a channel-specific `group_jid` into the shared,
// channel-neutral InboxEntry that every surface in the CRM renders.
//
// It refuses a private conversation rather than returning an empty JID: a
// caller that asked for the group of a one-to-one chat has a bug, and answering
// "no group" would let the panel render an empty roster instead.
func (uc *GroupUseCases) ResolveConversation(
	ctx context.Context,
	workspaceID, entryID string,
) (instanceID, groupJID string, err error) {
	if uc.conversations == nil {
		return "", "", uw.ErrConversationNotFound
	}
	conv, err := uc.conversations.FindByID(ctx, entryID)
	if err != nil {
		return "", "", err
	}
	if conv.WorkspaceID != workspaceID {
		// Not-found rather than forbidden: whether an id exists is itself
		// information this endpoint has no reason to leak.
		return "", "", uw.ErrConversationNotFound
	}
	if !conv.IsGroup || !uw.IsGroupJID(conv.ChatID) {
		return "", "", uw.ErrNotAGroupJID
	}
	return conv.InstanceID, conv.ChatID, nil
}

// List returns the groups a connected number belongs to.
//
// Served from our own rows, with no provider call. The panel is a list of names
// and the roster is not loaded, so making this a live read would spend a call on
// every render of a screen whose content changes weekly.
func (uc *GroupUseCases) List(ctx context.Context, workspaceID, instanceID string) ([]*uw.Group, error) {
	if _, err := uc.instanceFor(ctx, workspaceID, instanceID); err != nil {
		return nil, err
	}
	return uc.groups.ListByInstance(ctx, instanceID)
}

// Get returns one group with its roster, re-reading it only when stale.
//
// refresh forces past both our TTL and the provider's own cache. It is what the
// panel's refresh button calls, and the only path that always costs a call.
func (uc *GroupUseCases) Get(
	ctx context.Context,
	workspaceID, instanceID, groupJID string,
	refresh bool,
) (*uw.Group, error) {
	instance, err := uc.instanceFor(ctx, workspaceID, instanceID)
	if err != nil {
		return nil, err
	}
	if !uw.IsGroupJID(groupJID) {
		return nil, uw.ErrNotAGroupJID
	}

	if refresh {
		// An operator pressing refresh is positive evidence that the cached copy
		// is wrong, so the picture is re-read too rather than waiting out its own
		// clock. This is the button someone reaches for precisely BECAUSE they
		// just changed the group's photo and the CRM still shows the old one.
		return uc.metadata.syncSubject(ctx, instance, groupJID,
			uw.GroupInfoOptions{Force: true}, true)
	}

	group := uc.metadata.ensureFresh(ctx, instance, groupJID)
	if group == nil {
		return nil, uw.ErrGroupNotFound
	}
	return group, nil
}

// InviteLink fetches the join link.
//
// Its own method, and never part of Get: the link is a credential — anyone
// holding it can join the customer's group — so it is fetched on an explicit
// request, returned once, and never cached in our database.
func (uc *GroupUseCases) InviteLink(ctx context.Context, workspaceID, instanceID, groupJID string) (string, error) {
	instance, group, err := uc.adminContext(ctx, workspaceID, instanceID, groupJID)
	if err != nil {
		return "", err
	}
	ref, err := uc.refFor(ctx, instance)
	if err != nil {
		return "", err
	}

	fresh, err := uc.groupAPI.GroupInfo(ctx, ref, group.JID, uw.GroupInfoOptions{WithInviteLink: true})
	if err != nil {
		return "", err
	}
	return fresh.InviteLink, nil
}

// UpdateNameInput renames a group.
type UpdateNameInput struct {
	WorkspaceID string
	InstanceID  string
	GroupJID    string
	Name        string
}

func (uc *GroupUseCases) UpdateName(ctx context.Context, in UpdateNameInput) (*uw.Group, error) {
	if err := uw.ValidateGroupName(in.Name); err != nil {
		return nil, err
	}
	return uc.mutate(ctx, in.WorkspaceID, in.InstanceID, in.GroupJID,
		func(ref uw.InstanceRef, jid string) error {
			return uc.groupAPI.UpdateGroupName(ctx, ref, jid, in.Name)
		})
}

// UpdateDescriptionInput edits a group's description.
type UpdateDescriptionInput struct {
	WorkspaceID string
	InstanceID  string
	GroupJID    string
	Description string
}

func (uc *GroupUseCases) UpdateDescription(ctx context.Context, in UpdateDescriptionInput) (*uw.Group, error) {
	if err := uw.ValidateGroupTopic(in.Description); err != nil {
		return nil, err
	}
	return uc.mutate(ctx, in.WorkspaceID, in.InstanceID, in.GroupJID,
		func(ref uw.InstanceRef, jid string) error {
			return uc.groupAPI.UpdateGroupDescription(ctx, ref, jid, in.Description)
		})
}

// UpdateSettingsInput toggles who may post and who may edit the group.
//
// Both are pointers so "leave this alone" is distinguishable from "set it to
// false" — a form that submits every field would otherwise silently re-open an
// announce-only group every time someone edited its name.
type UpdateSettingsInput struct {
	WorkspaceID        string
	InstanceID         string
	GroupJID           string
	AdminsOnlyMessages *bool
	AdminsOnlyEdit     *bool
}

func (uc *GroupUseCases) UpdateSettings(ctx context.Context, in UpdateSettingsInput) (*uw.Group, error) {
	return uc.mutate(ctx, in.WorkspaceID, in.InstanceID, in.GroupJID,
		func(ref uw.InstanceRef, jid string) error {
			if in.AdminsOnlyMessages != nil {
				if err := uc.groupAPI.UpdateAnnounce(ctx, ref, jid, *in.AdminsOnlyMessages); err != nil {
					return err
				}
			}
			if in.AdminsOnlyEdit != nil {
				if err := uc.groupAPI.UpdateLocked(ctx, ref, jid, *in.AdminsOnlyEdit); err != nil {
					return err
				}
			}
			return nil
		})
}

// UpdateParticipantsInput adds, removes, promotes, demotes, approves or rejects.
type UpdateParticipantsInput struct {
	WorkspaceID  string
	InstanceID   string
	GroupJID     string
	Action       uw.GroupAction
	Participants []string
}

func (uc *GroupUseCases) UpdateParticipants(ctx context.Context, in UpdateParticipantsInput) (*uw.Group, error) {
	if !in.Action.Valid() {
		return nil, uw.ErrInvalidGroupAction
	}
	if len(in.Participants) == 0 {
		return nil, uw.ErrNoParticipants
	}
	return uc.mutate(ctx, in.WorkspaceID, in.InstanceID, in.GroupJID,
		func(ref uw.InstanceRef, jid string) error {
			return uc.groupAPI.UpdateParticipants(ctx, ref, uw.UpdateParticipantsInput{
				GroupJID:     jid,
				Action:       in.Action,
				Participants: in.Participants,
			})
		})
}

// UpdateImage sets the group picture from a URL, or removes it when url is
// empty.
func (uc *GroupUseCases) UpdateImage(ctx context.Context, workspaceID, instanceID, groupJID, url string) (*uw.Group, error) {
	return uc.mutate(ctx, workspaceID, instanceID, groupJID,
		func(ref uw.InstanceRef, jid string) error {
			return uc.groupAPI.UpdateGroupImage(ctx, ref, jid, url)
		})
}

// Leave exits a group.
//
// The cached row and roster go with it: they describe a membership that no
// longer exists, and keeping them would leave the panel rendering a group the
// number is not in. The CONVERSATION is deliberately left alone — the transcript
// is history, and history does not stop having happened.
func (uc *GroupUseCases) Leave(ctx context.Context, workspaceID, instanceID, groupJID string) error {
	instance, group, err := uc.resolve(ctx, workspaceID, instanceID, groupJID)
	if err != nil {
		return err
	}
	ref, err := uc.refFor(ctx, instance)
	if err != nil {
		return err
	}
	if err := uc.groupAPI.LeaveGroup(ctx, ref, group.JID); err != nil {
		return err
	}
	if err := uc.groups.Delete(ctx, group.ID); err != nil {
		log.Printf("[unofficial-whatsapp] left group %s but could not clear its row: %v", group.JID, err)
	}
	return nil
}

// ---------------------------------------------------------------- internals

// mutate is the shared shape of every group change: verify the caller may
// administer this group, apply, then re-read.
//
// The re-read is not optional and is not a convenience. A mutation's provider
// response says only that it was accepted, so without it the panel would render
// the value the operator typed rather than the one WhatsApp actually holds —
// and those diverge whenever a change is partially applied, which
// updateParticipants does routinely when one number in a batch is unreachable.
func (uc *GroupUseCases) mutate(
	ctx context.Context,
	workspaceID, instanceID, groupJID string,
	apply func(ref uw.InstanceRef, jid string) error,
) (*uw.Group, error) {
	instance, group, err := uc.adminContext(ctx, workspaceID, instanceID, groupJID)
	if err != nil {
		return nil, err
	}
	ref, err := uc.refFor(ctx, instance)
	if err != nil {
		return nil, err
	}
	if err := apply(ref, group.JID); err != nil {
		return nil, err
	}
	// Forced past the provider's cache: it has just changed, and its own cache
	// is exactly what would hand us back the old value. The subject goes with
	// it, so a rename or a new picture reaches the inbox in the same beat as the
	// panel.
	return uc.metadata.syncSubject(ctx, instance, group.JID, uw.GroupInfoOptions{Force: true}, true)
}

// adminContext resolves a group and refuses unless our number administers it.
func (uc *GroupUseCases) adminContext(
	ctx context.Context,
	workspaceID, instanceID, groupJID string,
) (*uw.Instance, *uw.Group, error) {
	instance, group, err := uc.resolve(ctx, workspaceID, instanceID, groupJID)
	if err != nil {
		return nil, nil, err
	}
	if !group.WeAreAdmin {
		return nil, nil, uw.ErrNotGroupAdmin
	}
	return instance, group, nil
}

// resolve loads the instance and the group, enforcing that both belong to the
// caller.
//
// The group is read through the metadata reader rather than straight from the
// repository, so a group the CRM has never opened is synced on first use instead
// of reporting "not found" for something the number is plainly a member of.
func (uc *GroupUseCases) resolve(
	ctx context.Context,
	workspaceID, instanceID, groupJID string,
) (*uw.Instance, *uw.Group, error) {
	instance, err := uc.instanceFor(ctx, workspaceID, instanceID)
	if err != nil {
		return nil, nil, err
	}
	if !uw.IsGroupJID(groupJID) {
		return nil, nil, uw.ErrNotAGroupJID
	}

	group := uc.metadata.ensureFresh(ctx, instance, groupJID)
	if group == nil {
		return nil, nil, uw.ErrGroupNotFound
	}
	// Belt and braces against a caller passing another instance's group id: the
	// lookup is already instance-scoped, so this can only fire on a bug, and a
	// bug here would act on the wrong customer's WhatsApp.
	if group.InstanceID != "" && group.InstanceID != instance.ID {
		return nil, nil, ErrGroupNotInWorkspace
	}
	return instance, group, nil
}

// instanceFor loads an instance and enforces workspace ownership.
//
// Not-found rather than forbidden for another workspace's instance: whether an
// id exists is itself information, and this endpoint has no reason to leak it.
func (uc *GroupUseCases) instanceFor(ctx context.Context, workspaceID, instanceID string) (*uw.Instance, error) {
	instance, err := uc.instances.FindByID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if instance.WorkspaceID != workspaceID {
		return nil, uw.ErrInstanceNotFound
	}
	return instance, nil
}

func (uc *GroupUseCases) refFor(ctx context.Context, instance *uw.Instance) (uw.InstanceRef, error) {
	server, err := uc.servers.FindByID(ctx, instance.ServerID)
	if err != nil {
		return uw.InstanceRef{}, err
	}
	return uw.RefFor(server, instance), nil
}
