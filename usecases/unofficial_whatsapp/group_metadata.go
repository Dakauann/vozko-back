package unofficial_whatsapp

import (
	"context"
	"log"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

type groupMetadata struct {
	groups   uw.GroupRepository
	contacts uw.ContactRepository
	servers  uw.ServerRepository
	groupAPI uw.GroupAPI
	profiles subjectProfile
	ttl      time.Duration
	now      func() time.Time
}

func newGroupMetadata(d HandleWebhookDeps, profiles subjectProfile) groupMetadata {
	return groupMetadata{
		groups:   d.Groups,
		contacts: d.Contacts,
		servers:  d.Servers,
		groupAPI: d.GroupAPI,
		profiles: profiles,
		ttl:      uw.GroupMetadataTTL,
		now:      nowUTC,
	}
}

func (s groupMetadata) enabled() bool {
	return s.groups != nil && s.groupAPI != nil && s.servers != nil
}

func (s groupMetadata) ensureFresh(
	ctx context.Context,
	instance *uw.Instance,
	groupJID string,
) *uw.Group {
	if !s.enabled() || !uw.IsGroupJID(groupJID) {
		return nil
	}

	cached, err := s.groups.FindByJID(ctx, instance.ID, groupJID)
	if err != nil && err != uw.ErrGroupNotFound {
		log.Printf("[unofficial-whatsapp] group lookup failed for %s: %v", groupJID, err)
		return nil
	}
	if !cached.NeedsSync(s.now(), s.ttl) {
		return cached
	}

	invalidated := cached != nil && cached.StaleAt != nil

	synced, err := s.syncSubject(ctx, instance, groupJID, uw.GroupInfoOptions{}, invalidated)
	if err != nil {
		log.Printf("[unofficial-whatsapp] group sync failed for %s: %v", groupJID, err)
		return cached
	}
	return synced
}

func (s groupMetadata) syncSubject(
	ctx context.Context,
	instance *uw.Instance,
	groupJID string,
	opts uw.GroupInfoOptions,
	forcePicture bool,
) (*uw.Group, error) {
	synced, err := s.sync(ctx, instance, groupJID, opts)
	if err != nil {
		return nil, err
	}
	if s.contacts == nil {
		return synced, nil
	}

	subject, err := s.contacts.FindByJID(ctx, instance.ID, groupJID)
	if err != nil || subject == nil {
		return synced, nil
	}
	s.profiles.refreshGroupSubject(ctx, instance, subject, synced.Subject, forcePicture)
	return synced, nil
}

func (s groupMetadata) sync(
	ctx context.Context,
	instance *uw.Instance,
	groupJID string,
	opts uw.GroupInfoOptions,
) (*uw.Group, error) {
	if !s.enabled() {
		return nil, uw.ErrGroupNotFound
	}

	server, err := s.servers.FindByID(ctx, instance.ServerID)
	if err != nil {
		return nil, err
	}

	group, err := s.groupAPI.GroupInfo(ctx, uw.RefFor(server, instance), groupJID, opts)
	if err != nil {
		return nil, err
	}
	group.WorkspaceID = instance.WorkspaceID
	group.InstanceID = instance.ID

	if err := s.groups.Upsert(ctx, group); err != nil {
		return nil, err
	}

	if err := s.groups.LinkParticipantContacts(ctx, group.ID, instance.ID); err != nil {
		log.Printf("[unofficial-whatsapp] participant contact link failed for %s: %v", group.JID, err)
	}
	return group, nil
}

func (s groupMetadata) markStale(ctx context.Context, instanceID, groupJID string) error {
	if !s.enabled() || !uw.IsGroupJID(groupJID) {
		return nil
	}
	return s.groups.MarkStale(ctx, instanceID, groupJID, s.now())
}
