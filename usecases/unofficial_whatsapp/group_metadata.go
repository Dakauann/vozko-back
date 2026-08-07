package unofficial_whatsapp

import (
	"context"
	"log"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

// Group metadata: the subject, the roster and the admin rules of a group chat.
//
// # No cron, and no sweep
//
// A background job over every group of every connected instance would spend the
// budget of every number on the platform re-reading rosters nobody is looking
// at — on a channel where traffic that looks automated gets the customer's
// number banned. So this is a pull with exactly three triggers:
//
//  1. the first message from a group we have never read;
//  2. a `groups` webhook, which only marks the row stale — the re-read happens
//     on the next message, so a group nobody talks in is never read again;
//  3. an operator opening or refreshing the group panel.
//
// # The call budget, per inbound group message
//
//	group already read, still fresh ............. 0 provider calls  ← ~all traffic
//	first message from an unknown group ......... 1 /group/info + the avatar pair
//	after a `groups` webhook marked it stale .... 1 /group/info
//	any backfilled message ...................... 0 calls, always
//
// The provider offers no trustworthy push to replace any of this: its `groups`
// webhook carries a payload documented only as "a map, the shape varies", so it
// can say THAT something changed and never WHAT. Acting on a guess would write a
// roster that is confidently wrong, which is worse than one that is briefly
// stale — hence invalidate-then-re-read rather than apply-the-payload.

// groupMetadata reads a group's metadata and keeps its CRM subject in step.
//
// It owns both halves on purpose: a group's name lives on its subject row
// (because that is what the inbox renders) and its roster lives on the group
// row, and a sync that updated one without the other would leave the inbox
// showing a name the panel disagrees with.
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

// ensureFresh syncs a group when the cached row is missing, invalidated or old,
// and returns whatever we know either way.
//
// Never returns an error to the caller: this runs on the webhook path, and a
// group whose metadata could not be read is a group with a placeholder name, not
// a message that failed to arrive.
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

	// Was this an INVALIDATION rather than merely an expiry?
	//
	// It decides whether the subject's picture is re-read as well. /group/info
	// carries no image at all — the avatar lives on the chat-details endpoint —
	// so without this a `groups` webhook refreshed the name and the roster while
	// the picture sat behind its own week-long clock. That is exactly the
	// reported symptom: an operator changed the group photo and the CRM kept the
	// old one.
	invalidated := cached != nil && cached.StaleAt != nil

	synced, err := s.syncSubject(ctx, instance, groupJID, uw.GroupInfoOptions{}, invalidated)
	if err != nil {
		// The stale row is still the best answer available, and returning it
		// keeps the conversation rendering a real name through a provider blip.
		log.Printf("[unofficial-whatsapp] group sync failed for %s: %v", groupJID, err)
		return cached
	}
	return synced
}

// syncSubject reads the group AND brings its CRM subject into step.
//
// Every sync path goes through here rather than through sync() directly, because
// the two halves of a group's identity live in different rows: the roster and the
// admin rules on the group, the name and the picture on the subject the inbox
// renders. A path that refreshed only one leaves the panel and the conversation
// list disagreeing about what the group is called.
//
// `forcePicture` bypasses the picture's own staleness clock. /group/info carries
// no image, so the avatar always costs a second read — worth paying when
// something told us the group CHANGED (a `groups` webhook, an operator pressing
// refresh) and not worth paying on a routine expiry.
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

	// Absent until the group's first message creates it. Nothing to update yet,
	// and inventing a subject here would race the ingest path for it.
	subject, err := s.contacts.FindByJID(ctx, instance.ID, groupJID)
	if err != nil || subject == nil {
		return synced, nil
	}
	s.profiles.refreshGroupSubject(ctx, instance, subject, synced.Subject, forcePicture)
	return synced, nil
}

// sync reads a group from the provider and persists it.
//
// Returns an error, unlike ensureFresh: the operator-facing refresh needs to
// report a failure rather than silently show stale data.
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

	// Best-effort, and after the roster is committed: the link only powers
	// "open the direct conversation" from a member row, and a member we have
	// never spoken to legitimately has none.
	if err := s.groups.LinkParticipantContacts(ctx, group.ID, instance.ID); err != nil {
		log.Printf("[unofficial-whatsapp] participant contact link failed for %s: %v", group.JID, err)
	}
	return group, nil
}

// markStale records a `groups` webhook.
//
// One column, no payload parsing. The provider specifies no schema for that
// event, so the only thing it reliably says is "something about this group
// changed" — and acting on a guess at WHAT changed would write a roster that is
// confidently wrong, which is strictly worse than one that is briefly stale.
func (s groupMetadata) markStale(ctx context.Context, instanceID, groupJID string) error {
	if !s.enabled() || !uw.IsGroupJID(groupJID) {
		return nil
	}
	return s.groups.MarkStale(ctx, instanceID, groupJID, s.now())
}
