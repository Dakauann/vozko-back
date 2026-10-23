package campaignaudiencewiring

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"vozko/domain/cache"
	"vozko/domain/emailhygiene"
	campaignaudience_repository "vozko/infra/repositories/campaignaudience"
	lead_repository "vozko/infra/repositories/lead"
	"vozko/infra/sheetfile"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
)

const (
	countLease = 2 * time.Minute
	countPoll  = 100 * time.Millisecond
	countKeep  = 30 * time.Second
)

type Adapters struct {
	Selections  campaignaudience_usecase.Selections
	Leads       lead_repository.Repository
	Definitions campaignaudience_usecase.Definitions
	Templates   campaignaudience_usecase.TemplateReader
	Gate        cache.Gate
	Shared      cache.SharedState
	EmailsSeen  emailhygiene.AddressSeen
}

type Module struct {
	Service     *campaignaudience_usecase.Service
	Suggestions *campaignaudience_repository.Suggestions
}

func New(db *gorm.DB, a Adapters) (*Module, error) {
	if db == nil || a.Leads == nil {
		return nil, fmt.Errorf("campaign audience wiring: the database and the lead repository are required")
	}
	phones, err := lead_repository.NewImports(a.Leads)
	if err != nil {
		return nil, fmt.Errorf("campaign audience wiring: %w", err)
	}
	flight, err := cache.NewSharedFlight(a.Shared, countLease, countPoll, countKeep)
	if err != nil {
		return nil, fmt.Errorf("campaign audience wiring: %w", err)
	}
	suggestions := campaignaudience_repository.NewSuggestions(db)
	deps := campaignaudience_usecase.Deps{
		Selections: a.Selections, Lists: campaignaudience_repository.New(db), Sheets: sheetfile.Reader{},
		Phones: phones, Emails: a.Leads, EmailLeads: a.Leads, EmailsSeen: a.EmailsSeen, Definitions: a.Definitions,
		Suggestions: suggestions, Gate: a.Gate, Flight: flight,
	}
	if a.Templates != nil {
		deps.Templates = a.Templates
	}
	service, err := campaignaudience_usecase.New(deps)
	if err != nil {
		return nil, err
	}
	return &Module{Service: service, Suggestions: suggestions}, nil
}
