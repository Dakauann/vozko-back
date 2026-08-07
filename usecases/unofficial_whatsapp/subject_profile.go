package unofficial_whatsapp

import (
	"context"
	"log"
	"path"
	"strings"
	"time"

	"vozko/domain/media"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

// Subject profile enrichment — the name and the avatar the CRM shows for a
// conversation.
//
// # There is no job here
//
// Nothing in this file polls, sweeps or runs on a schedule. It is called inline
// while a webhook is being ingested: a message arrives, and if we do not already
// have a picture for whoever sent it, we fetch one, store it in our own object
// storage, and stop asking. Every function below is a plain method the webhook
// usecase calls in-line with persisting the message.
//
// # The call budget, per inbound message
//
//	known subject, picture already stored ....... 0 provider calls  ← ~all traffic
//	first message from a new subject ............ 1 call + 1 CDN GET
//	subject whose picture is a week old ......... 1 call + 1 CDN GET
//	any backfilled message ...................... 0 calls, always
//
// Steady state is ZERO. An instance carrying a thousand messages a day from
// people it already knows makes no profile calls at all, because the answer is a
// column. That is the entire reason the avatar is stored rather than resolved:
// asking WhatsApp who someone is while the CRM renders a row would turn one
// inbox page into fifty calls on the same instance the send path is using, and a
// number that looks like it is hammering the API is a number that gets banned —
// this channel's real failure mode.
//
// Four rules hold that budget, and none of them is optional:
//
//  1. NEVER on a read path. Enrichment happens on ingest, never on render.
//  2. TTL-gated, so a chatty contact costs one call a week and not one a
//     message.
//  3. Never during a backfill. Connecting an instance replays up to seven days
//     of history in one burst, and enriching each event would be hundreds of
//     calls in a few seconds — the single worst thing this file could do.
//  4. The clock is stamped on EVERY attempt, including failures. A subject whose
//     picture 404s must not be retried on the next message and the one after
//     that, forever.
//
// Two messages from the same new subject landing on different consumer workers
// can both decide to fetch. That is one wasted call, once, and it closes itself:
// the storage key is deterministic so the second write is idempotent, and the
// first TTL stamp shuts the window. A cross-worker lock would cost more than the
// duplicate it prevents.
//
// One implementation serves people and groups, because "who is this" is one
// question and the provider answers it from one endpoint. Two copies would be
// two places for the name half and the picture half of an identity to drift
// apart, and the group half would be the one that rots.
//
// The bytes are re-hosted rather than linked. WhatsApp's avatar URLs are
// short-lived and unauthenticated: a stored link rots within hours and, while it
// works, hands the customer's photo to anyone holding the URL. This mirrors what
// the Telegram channel does with its own 1-hour file links.

// profileTTL bounds how stale a stored subject profile may get.
//
// A week. The only thing enrichment adds beyond what an event already carries is
// the picture and the saved-contact name, and neither changes often enough to
// justify spending more of an instance's budget than that.
const profileTTL = 7 * 24 * time.Hour

// avatarKeyPrefix namespaces stored avatars by channel, matching the layout the
// Telegram channel already uses for the same asset.
const avatarKeyPrefix = "contacts"

// subjectProfile refreshes conversation subjects.
//
// Its dependencies are all optional, and each absence degrades one step rather
// than failing: with no messaging port there is no enrichment at all, with no
// storage there are names but no pictures, with no fetcher there are names and
// whatever picture a previous run stored.
type subjectProfile struct {
	contacts    uw.ContactRepository
	servers     uw.ServerRepository
	messaging   uw.MessagingAPI
	assets      uw.RemoteAssetFetcher
	fileStorage media.FileStorage

	ttl time.Duration
	// now is injectable so a test can assert the staleness boundary without
	// sleeping through a week.
	now func() time.Time
	// gate is the per-instance ceiling on enrichment. The TTL bounds how often
	// ONE subject is read; this bounds how many are read at once, which twenty
	// concurrent webhook workers would otherwise leave unbounded.
	gate *profileGate
}

func newSubjectProfile(d HandleWebhookDeps) subjectProfile {
	return subjectProfile{
		contacts:    d.Contacts,
		servers:     d.Servers,
		messaging:   d.Messaging,
		assets:      d.Assets,
		fileStorage: d.FileStorage,
		ttl:         profileTTL,
		now:         nowUTC,
		gate:        sharedProfileGate,
	}
}

// nowUTC is the package's clock, replaced in tests that assert a staleness
// boundary without sleeping through a week.
func nowUTC() time.Time { return time.Now().UTC() }

// applyEventName records the display name an event already carried.
//
// Free: the name arrived in the payload, so this costs a comparison and, only
// when it actually changed, one UPDATE. It deliberately does NOT stamp the
// profile clock — it is not a profile read, and letting it stamp would starve
// the picture fetch below of every chance to run, which is precisely the bug
// this pair replaces. The two used to be one function whose first guard
// ("the event carried no new name") returned before the TTL was ever consulted,
// so the provider read never happened and picture_url was empty for every
// contact this channel has ever created.
func (s subjectProfile) applyEventName(ctx context.Context, subject *uw.Contact, eventName string) {
	if s.contacts == nil || subject == nil {
		return
	}
	name := strings.TrimSpace(eventName)
	if name == "" || name == subject.Name {
		return
	}
	// A group's subject comes from its metadata sync, not from whoever spoke.
	// The push name on a group message is the PARTICIPANT's, and writing it here
	// would rename the group after its most recent talker.
	if subject.IsGroup {
		return
	}

	// FetchedAt deliberately left zero: the repository reads that as "not a
	// profile read" and leaves the clock alone. Advancing it here would consume
	// the subject's weekly refresh budget on a free rename and suppress the
	// picture fetch that is due.
	err := s.contacts.UpdateProfile(ctx, subject.ID, uw.ContactProfile{Name: name})
	if err != nil {
		log.Printf("[unofficial-whatsapp] name refresh failed for subject %s: %v", subject.ID, err)
		return
	}
	subject.Name = name
}

// refresh re-reads a subject's profile from the provider when it is stale.
//
// `force` bypasses the staleness clock, and exists for one specific reason:
// something told us this subject CHANGED. A `groups` webhook, or an operator
// pressing refresh, is positive evidence that the cached copy is wrong, and
// making it wait out a week-long TTL is what produced the symptom where someone
// changed a group's picture and the CRM kept showing the old one for days.
//
// It is not a way around the budget. Forced refreshes come from invalidations
// and from explicit clicks, both of which are rare and both of which are already
// paying for a provider call; and the picture itself is still only downloaded
// when its source url actually changed.
//
// Best-effort throughout: enrichment is cosmetic, and a failure here must never
// fail the message that triggered it. Every exit stamps the clock.
func (s subjectProfile) refresh(ctx context.Context, instance *uw.Instance, subject *uw.Contact, force bool) {
	if s.contacts == nil || s.messaging == nil || s.servers == nil || subject == nil {
		return
	}
	if !force && !subject.ProfileIsStale(s.now(), s.ttl) {
		return
	}
	// The per-instance ceiling, checked AFTER the staleness gate so a fresh
	// subject costs no budget at all.
	//
	// A forced read is exempt: it comes from an operator's click or from an
	// invalidation the provider itself sent, both of which are rare, both of
	// which a person is waiting on, and neither of which is a burst.
	if !force && !s.gate.allow(instance.ID) {
		// No clock stamp: this subject was not read, so the next message must
		// try again. That is the retry queue, and it needs no queue.
		log.Printf("[unofficial-whatsapp] instance %s: profile budget spent, deferring enrichment for subject %s",
			instance.ID, subject.ID)
		return
	}

	server, err := s.servers.FindByID(ctx, instance.ServerID)
	if err != nil {
		// No clock stamp: the host row being unreadable says nothing about this
		// subject, and burning its TTL on our own outage would leave it
		// unenriched for a week after the outage ended.
		log.Printf("[unofficial-whatsapp] profile skipped, server unavailable: %v", err)
		return
	}
	ref := uw.RefFor(server, instance)

	// Stamped up front so every path below writes it. A read that fails, a
	// subject with no picture and a successful refresh all cost one TTL.
	profile := uw.ContactProfile{FetchedAt: s.now()}

	details, err := s.messaging.ChatDetails(ctx, ref, subject.ProfileRef())
	if err != nil {
		log.Printf("[unofficial-whatsapp] profile read failed for subject %s: %v", subject.ID, err)
	} else if details != nil {
		// Said out loud, because "we asked and WhatsApp has no photo for them"
		// and "we never asked" are indistinguishable from a blank avatar, and
		// only one of them is a bug.
		if details.PictureURL == "" {
			log.Printf("[unofficial-whatsapp] no profile picture available for subject %s (ref %q)",
				subject.ID, subject.ProfileRef())
		}
		profile.Name = details.Name
		profile.ContactName = details.ContactName
		profile.VerifiedName = details.VerifiedName
		profile.IsBusiness = details.IsBusiness
		// Compared before downloading. WhatsApp's avatar urls carry a content
		// id, so an unchanged url is an unchanged photo: a weekly refresh of a
		// contact who never changed their picture costs the read and nothing
		// else — no download, no upload, no write to the picture columns.
		if details.PictureURL != "" && details.PictureURL != subject.PictureSourceURL {
			if url := s.storeAvatar(ctx, subject, details.PictureURL); url != "" {
				profile.PictureURL = url
				profile.PictureSourceURL = details.PictureURL
			}
		}
	}

	if err := s.contacts.UpdateProfile(ctx, subject.ID, profile); err != nil {
		log.Printf("[unofficial-whatsapp] profile update failed for subject %s: %v", subject.ID, err)
		return
	}
	s.applyLocally(subject, profile)
}

// refreshGroupSubject writes a group's synced identity onto its subject row.
//
// The name comes from the group metadata rather than from /chat/details,
// because /group/info is what an operator's rename lands in first — but the
// PICTURE only exists on /chat/details, which is why a group is enriched by the
// same path a person is. Calling refresh separately would be a second provider
// round trip for the half this one cannot produce.
func (s subjectProfile) refreshGroupSubject(
	ctx context.Context,
	instance *uw.Instance,
	subject *uw.Contact,
	subjectName string,
	force bool,
) {
	if s.contacts == nil || subject == nil {
		return
	}

	name := strings.TrimSpace(subjectName)
	// The picture is TTL-gated on its own; a rename must land immediately, so
	// the two are not folded into one staleness check.
	if name != "" && name != subject.Name {
		if err := s.contacts.UpdateProfile(ctx, subject.ID, uw.ContactProfile{Name: name}); err != nil {
			log.Printf("[unofficial-whatsapp] group subject rename failed for %s: %v", subject.ID, err)
		} else {
			subject.Name = name
		}
	}

	s.refresh(ctx, instance, subject, force)
}

// applyPushedPicture re-hosts an avatar an event volunteered, with no provider
// call at all.
//
// The `chats` and `contacts` webhooks carry the vendor's Chat object, picture
// included, so a profile-photo change arrives as a PUSH. This is the cheapest
// path in the file and the one that makes a changed photo appear in seconds
// rather than at the end of a TTL — and because the source url is compared
// first, an event that merely re-states the current picture costs one string
// comparison.
func (s subjectProfile) applyPushedPicture(ctx context.Context, subject *uw.Contact, remoteURL string) {
	if s.contacts == nil || subject == nil || strings.TrimSpace(remoteURL) == "" {
		return
	}
	if remoteURL == subject.PictureSourceURL {
		return
	}
	// Costs no provider API call, but it IS a download and an upload per
	// contact, and the address-book sync that follows a connect can push a great
	// many of these at once. Same ceiling, same skip-rather-than-queue rule: the
	// next event carrying this url re-attempts it.
	if !s.gate.allow(subject.InstanceID) {
		return
	}

	stored := s.storeAvatar(ctx, subject, remoteURL)
	if stored == "" {
		return
	}
	// FetchedAt left zero: this was a push, not a profile read, so it must not
	// consume the subject's weekly budget for the fields a push does not carry.
	err := s.contacts.UpdateProfile(ctx, subject.ID, uw.ContactProfile{
		PictureURL:       stored,
		PictureSourceURL: remoteURL,
	})
	if err != nil {
		log.Printf("[unofficial-whatsapp] pushed avatar write failed for subject %s: %v", subject.ID, err)
		return
	}
	subject.PictureURL, subject.PictureSourceURL = stored, remoteURL
}

// storeAvatar re-hosts a provider-hosted picture and returns OUR URL.
//
// The key is deterministic, so a refresh overwrites in place instead of growing
// a graveyard of orphaned objects, one per week per contact.
func (s subjectProfile) storeAvatar(ctx context.Context, subject *uw.Contact, remoteURL string) string {
	if s.fileStorage == nil || s.assets == nil || strings.TrimSpace(remoteURL) == "" {
		return ""
	}

	data, contentType, err := s.assets.FetchAsset(ctx, remoteURL)
	if err != nil || len(data) == 0 {
		if err != nil {
			log.Printf("[unofficial-whatsapp] avatar fetch failed for subject %s: %v", subject.ID, err)
		}
		return ""
	}

	// WhatsApp serves avatars as JPEG and its CDN is not always explicit about
	// it. The stored value becomes the Content-Type the CDN replays, and an
	// avatar served as application/octet-stream renders as a broken image.
	if strings.TrimSpace(contentType) == "" {
		contentType = "image/jpeg"
	}

	key := path.Join(avatarKeyPrefix, string(shared.EntryTypeUnofficialWhatsApp),
		subject.ID, "avatar"+extensionFor(contentType, ""))
	if err := s.fileStorage.UploadFile(key, data, contentType); err != nil {
		log.Printf("[unofficial-whatsapp] avatar upload failed for subject %s: %v", subject.ID, err)
		return ""
	}
	return s.fileStorage.GetFileURL(key)
}

// applyLocally mirrors a persisted profile onto the in-memory subject, so the
// message written moments later carries the name and avatar that were just
// stored rather than the ones it was loaded with.
func (s subjectProfile) applyLocally(subject *uw.Contact, p uw.ContactProfile) {
	if p.Name != "" {
		subject.Name = p.Name
	}
	if p.ContactName != "" {
		subject.ContactName = p.ContactName
	}
	if p.VerifiedName != "" {
		subject.VerifiedName = p.VerifiedName
	}
	if p.PictureURL != "" {
		subject.PictureURL = p.PictureURL
	}
	if p.PictureSourceURL != "" {
		subject.PictureSourceURL = p.PictureSourceURL
	}
	if p.IsBusiness {
		subject.IsBusiness = true
	}
	if !p.FetchedAt.IsZero() {
		fetched := p.FetchedAt
		subject.ProfileFetchedAt = &fetched
	}
}
