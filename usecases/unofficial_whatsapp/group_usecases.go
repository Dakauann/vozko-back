package unofficial_whatsapp

import (
	"context"
	"errors"
	"log"

	"vozko/domain/media"
	uw "vozko/domain/unofficial_whatsapp"
)

var ErrGroupNotInWorkspace = errors.New("that group does not belong to this workspace")

type GroupUseCases struct {
	instances     uw.InstanceRepository
	servers       uw.ServerRepository
	conversations uw.ConversationRepository
	groups        uw.GroupRepository
	groupAPI      uw.GroupAPI
	metadata      groupMetadata
}

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
		return "", "", uw.ErrConversationNotFound
	}
	if !conv.IsGroup || !uw.IsGroupJID(conv.ChatID) {
		return "", "", uw.ErrNotAGroupJID
	}
	return conv.InstanceID, conv.ChatID, nil
}

func (uc *GroupUseCases) List(ctx context.Context, workspaceID, instanceID string) ([]*uw.Group, error) {
	if _, err := uc.instanceFor(ctx, workspaceID, instanceID); err != nil {
		return nil, err
	}
	return uc.groups.ListByInstance(ctx, instanceID)
}

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
		return uc.metadata.syncSubject(ctx, instance, groupJID,
			uw.GroupInfoOptions{Force: true}, true)
	}

	group := uc.metadata.ensureFresh(ctx, instance, groupJID)
	if group == nil {
		return nil, uw.ErrGroupNotFound
	}
	return group, nil
}

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

func (uc *GroupUseCases) UpdateImage(ctx context.Context, workspaceID, instanceID, groupJID, url string) (*uw.Group, error) {
	return uc.mutate(ctx, workspaceID, instanceID, groupJID,
		func(ref uw.InstanceRef, jid string) error {
			return uc.groupAPI.UpdateGroupImage(ctx, ref, jid, url)
		})
}

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
	return uc.metadata.syncSubject(ctx, instance, group.JID, uw.GroupInfoOptions{Force: true}, true)
}

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
	if group.InstanceID != "" && group.InstanceID != instance.ID {
		return nil, nil, ErrGroupNotInWorkspace
	}
	return instance, group, nil
}

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
