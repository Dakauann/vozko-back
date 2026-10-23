package carriermessaging_usecase

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
)

var ErrDependencyMissing = errors.New("rcs: a required dependency is missing")

type Deps struct {
	Repos               Repositories
	Provider            carriermessaging.Provider
	SMS                 carriermessaging.SMSProvider
	Charges             *Charges
	Balance             BalanceReader
	Spend               SpendReader
	Links               Links
	Leads               LeadResolver
	Transcript          Transcript
	TranscriptStatus    TranscriptStatus
	Announcer           StatusAnnouncer
	Timezones           WorkspaceTimezones
	WhatsApp            WhatsAppStarter
	Notices             Notices
	Inbox               Inbox
	Documents           DocumentValidator
	Library             MediaLibrary
	RemoteMedia         RemoteMedia
	MediaURLs           *conversation.MediaURLResolver
	AutomationRefs      AutomationRefs
	PlatformName        string
	PlatformEmailDomain string
	SMSSharedNumber     string
	Now                 func() time.Time
	NewID               func() string
}

type Service struct {
	Deps
	launched func(carriermessaging.Channel) bool
}

func NewService(d Deps) (*Service, error) {
	required := []any{
		d.Repos.Senders, d.Repos.Messages, d.Repos.Events, d.Repos.Suppressions, d.Repos.Settings,
		d.Repos.Contacts, d.Repos.RCSConversations, d.Repos.SMSConversations, d.Repos.Routes, d.Provider, d.SMS, d.Charges, d.Balance, d.Spend,
		d.Links, d.Leads, d.Transcript, d.TranscriptStatus, d.Announcer, d.Timezones, d.WhatsApp, d.Notices, d.Inbox, d.Documents, d.Library, d.RemoteMedia, d.AutomationRefs, d.MediaURLs,
	}
	for _, dep := range required {
		if isNil(dep) {
			return nil, ErrDependencyMissing
		}
	}
	if d.PlatformName == "" || d.PlatformEmailDomain == "" || d.SMSSharedNumber == "" {
		return nil, ErrDependencyMissing
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.NewID == nil {
		d.NewID = uuid.NewString
	}
	return &Service{Deps: d, launched: carriermessaging.Channel.Launched}, nil
}

func isNil(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case *Charges:
		return x == nil
	case *conversation.MediaURLResolver:
		return x == nil
	}
	return false
}
