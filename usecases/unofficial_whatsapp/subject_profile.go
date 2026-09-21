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

const profileTTL = 7 * 24 * time.Hour

const avatarKeyPrefix = "contacts"

type subjectProfile struct {
	contacts    uw.ContactRepository
	servers     uw.ServerRepository
	messaging   uw.MessagingAPI
	assets      uw.RemoteAssetFetcher
	fileStorage media.FileStorage

	ttl  time.Duration
	now  func() time.Time
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

func nowUTC() time.Time { return time.Now().UTC() }

func (s subjectProfile) applyEventName(ctx context.Context, subject *uw.Contact, eventName string) {
	if s.contacts == nil || subject == nil {
		return
	}
	name := strings.TrimSpace(eventName)
	if name == "" || name == subject.Name {
		return
	}
	if subject.IsGroup {
		return
	}

	err := s.contacts.UpdateProfile(ctx, subject.ID, uw.ContactProfile{Name: name})
	if err != nil {
		log.Printf("[unofficial-whatsapp] name refresh failed for subject %s: %v", subject.ID, err)
		return
	}
	subject.Name = name
}

func (s subjectProfile) refresh(ctx context.Context, instance *uw.Instance, subject *uw.Contact, force bool) {
	if s.contacts == nil || s.messaging == nil || s.servers == nil || subject == nil {
		return
	}
	if !force && !subject.ProfileIsStale(s.now(), s.ttl) {
		return
	}
	if !force && !s.gate.allow(instance.ID) {
		log.Printf("[unofficial-whatsapp] instance %s: profile budget spent, deferring enrichment for subject %s",
			instance.ID, subject.ID)
		return
	}

	server, err := s.servers.FindByID(ctx, instance.ServerID)
	if err != nil {
		log.Printf("[unofficial-whatsapp] profile skipped, server unavailable: %v", err)
		return
	}
	ref := uw.RefFor(server, instance)

	profile := uw.ContactProfile{FetchedAt: s.now()}

	details, err := s.messaging.ChatDetails(ctx, ref, subject.ProfileRef())
	if err != nil {
		log.Printf("[unofficial-whatsapp] profile read failed for subject %s: %v", subject.ID, err)
	} else if details != nil {
		if details.PictureURL == "" {
			log.Printf("[unofficial-whatsapp] no profile picture available for subject %s (ref %q)",
				subject.ID, subject.ProfileRef())
		}
		profile.Name = details.Name
		profile.ContactName = details.ContactName
		profile.VerifiedName = details.VerifiedName
		profile.IsBusiness = details.IsBusiness
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
	if name != "" && name != subject.Name {
		if err := s.contacts.UpdateProfile(ctx, subject.ID, uw.ContactProfile{Name: name}); err != nil {
			log.Printf("[unofficial-whatsapp] group subject rename failed for %s: %v", subject.ID, err)
		} else {
			subject.Name = name
		}
	}

	s.refresh(ctx, instance, subject, force)
}

func (s subjectProfile) applyPushedPicture(ctx context.Context, subject *uw.Contact, remoteURL string) {
	if s.contacts == nil || subject == nil || strings.TrimSpace(remoteURL) == "" {
		return
	}
	if remoteURL == subject.PictureSourceURL {
		return
	}
	if !s.gate.allow(subject.InstanceID) {
		return
	}

	stored := s.storeAvatar(ctx, subject, remoteURL)
	if stored == "" {
		return
	}
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
