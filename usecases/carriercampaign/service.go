package carriercampaign_usecase

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/actor"
	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/lead"
	"vozko/domain/selection"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
	carriermessaging_usecase "vozko/usecases/carriermessaging"
)

var (
	ErrDependencyMissing = errors.New("carrier campaign: a required dependency is missing")
	ErrQueueMissing      = errors.New("carrier campaign: the send queue is not attached")
	ErrActorRequired     = errors.New("carrier campaign: the person acting is required")
	ErrOnlyDraftDeletes  = errors.New("carrier campaign: only a draft or a ready campaign can be deleted")
	ErrAudienceEmpty     = errors.New("carrier campaign: the audience has nobody to send to")
	ErrVariantIndex      = errors.New("carrier campaign: the campaign has no such variant")
	ErrRunAlreadyExists  = errors.New("carrier campaign: this occurrence already has a run")
)

type Deps struct {
	Campaigns     cc.CampaignRepository
	Runs          cc.RunRepository
	Recipients    cc.RecipientRepository
	Funnels       cc.FunnelReader
	Selections    Selections
	Leads         Leads
	Contacts      lead.ContactDetails
	Names         actor.Namer
	Definitions   Definitions
	Suppressions  Suppressions
	Senders       Senders
	Routes        Routes
	Messages      Messages
	Carrier       Deliverer
	SenderPauser  SenderPauser
	Settings      Settings
	Prices        Prices
	Balance       carriermessaging_usecase.BalanceReader
	Spend         carriermessaging_usecase.SpendReader
	Enqueuer      Enqueuer
	Attempts      Attempts
	Stats         CampaignStats
	Timeline      campaign.TimelineUseCase
	RecipientRows RecipientReader
	Overview      OverviewReader
	WhatsAppSpend WhatsAppSpend
	Timezones     carriermessaging_usecase.WorkspaceTimezones
	Now           func() time.Time
	NewID         func() string
	Logf          func(format string, args ...any)
}

type Service struct {
	deps     Deps
	queue    Queue
	launched func(carriermessaging.Channel) bool
	audience *campaignaudience_usecase.Service
}

func New(d Deps) (*Service, error) {
	missing := map[string]bool{
		"campaigns":      d.Campaigns == nil,
		"runs":           d.Runs == nil,
		"recipients":     d.Recipients == nil,
		"funnels":        d.Funnels == nil,
		"selections":     d.Selections == nil,
		"leads":          d.Leads == nil,
		"contacts":       d.Contacts == nil,
		"names":          d.Names == nil,
		"definitions":    d.Definitions == nil,
		"suppressions":   d.Suppressions == nil,
		"senders":        d.Senders == nil,
		"routes":         d.Routes == nil,
		"messages":       d.Messages == nil,
		"carrier":        d.Carrier == nil,
		"sender pauser":  d.SenderPauser == nil,
		"settings":       d.Settings == nil,
		"prices":         d.Prices == nil,
		"balance":        d.Balance == nil,
		"spend":          d.Spend == nil,
		"enqueuer":       d.Enqueuer == nil,
		"attempts":       d.Attempts == nil,
		"stats":          d.Stats == nil,
		"timeline":       d.Timeline == nil,
		"recipient rows": d.RecipientRows == nil,
		"overview":       d.Overview == nil,
		"whatsapp spend": d.WhatsAppSpend == nil,
		"timezones":      d.Timezones == nil,
	}
	var absent []string
	for name, gone := range missing {
		if gone {
			absent = append(absent, name)
		}
	}
	if len(absent) > 0 {
		sort.Strings(absent)
		return nil, fmt.Errorf("%w: %s", ErrDependencyMissing, strings.Join(absent, ", "))
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.NewID == nil {
		d.NewID = uuid.NewString
	}
	if d.Logf == nil {
		d.Logf = log.Printf
	}
	return &Service{deps: d, launched: carriermessaging.Channel.Launched}, nil
}

func (s *Service) AttachQueue(q Queue) {
	s.queue = q
}

func (s *Service) now() time.Time {
	return s.deps.Now().UTC()
}

type Actor struct {
	UserID       string
	IsAdmin      bool
	DepartmentID string
}

func scopeOf(workspaceID string, a Actor) selection.Scope {
	return selection.Scope{WorkspaceID: workspaceID, ActorID: a.UserID, DepartmentID: a.DepartmentID, IsAdmin: a.IsAdmin}
}

func creatorOf(c cc.Campaign) Actor {
	return Actor{UserID: c.CreatedBy}
}
