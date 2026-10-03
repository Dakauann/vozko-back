package advertising

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	ads "vozko/domain/advertising"
)

const (
	conversionLookback = 7 * 24 * time.Hour
	conversionBatch    = 500
	recentConversions  = 50
)

type signalGateway interface {
	ListPixels(ctx context.Context, token, metaAccountID string) ([]ads.Pixel, error)
	DatasetForWABA(ctx context.Context, token, wabaID string) (string, error)
	SendEvents(ctx context.Context, token string, events []ads.ConversionEvent) error
}

type WABADirectory interface {
	WABAOf(ctx context.Context, workspaceID, businessPhoneID string) (string, error)
}

type ConversionsUseCase struct {
	access   accountAccess
	gateway  signalGateway
	settings ads.ConversionSettingsRepository
	outbox   ads.ConversionOutbox
	wabas    WABADirectory
}

func NewConversionsUseCase(sync *SyncUseCase, gateway signalGateway, settings ads.ConversionSettingsRepository, outbox ads.ConversionOutbox, wabas WABADirectory) *ConversionsUseCase {
	return &ConversionsUseCase{access: sync.access, gateway: gateway, settings: settings, outbox: outbox, wabas: wabas}
}

func (uc *ConversionsUseCase) Settings(ctx context.Context, workspaceID string) (*ads.ConversionSettings, error) {
	s, err := uc.settings.Get(ctx, workspaceID)
	if errors.Is(err, ads.ErrSettingsNotFound) {
		return &ads.ConversionSettings{WorkspaceID: workspaceID, SendLeads: true, SendPurchases: true}, nil
	}
	return s, err
}

func (uc *ConversionsUseCase) prepareSave(ctx context.Context, workspaceID string, s *ads.ConversionSettings) (*ads.Pixel, error) {
	s.WorkspaceID = workspaceID
	if err := s.Validate(); err != nil {
		return nil, err
	}
	account, token, err := uc.access.open(ctx, workspaceID, s.AdAccountID, ads.UseWrite)
	if err != nil {
		return nil, err
	}
	if s.PixelID == "" {
		return nil, nil
	}
	pixels, err := uc.gateway.ListPixels(ctx, token, account.MetaAccountID)
	if err != nil {
		return nil, uc.access.failed(ctx, account, err)
	}
	for i := range pixels {
		if pixels[i].MetaID == s.PixelID && !pixels[i].Unavailable {
			return &pixels[i], nil
		}
	}
	return nil, ads.FieldError("pixelId", "not_available")
}

func (uc *ConversionsUseCase) CheckSave(ctx context.Context, workspaceID string, s ads.ConversionSettings) (*ads.Pixel, error) {
	return uc.prepareSave(ctx, workspaceID, &s)
}

func (uc *ConversionsUseCase) Save(ctx context.Context, workspaceID string, s ads.ConversionSettings) (*ads.ConversionSettings, error) {
	if _, err := uc.prepareSave(ctx, workspaceID, &s); err != nil {
		return nil, err
	}
	s.UpdatedAt = uc.access.now()
	if err := uc.settings.Save(ctx, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

type datasetConnection struct {
	settings *ads.ConversionSettings
	account  *ads.AdAccount
	token    string
	waba     string
}

func (uc *ConversionsUseCase) prepareConnect(ctx context.Context, workspaceID, businessPhoneID string) (*datasetConnection, error) {
	current, err := uc.Settings(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if current.AdAccountID == "" {
		return nil, ads.FieldError("adAccountId", "required")
	}
	waba, err := uc.wabas.WABAOf(ctx, workspaceID, businessPhoneID)
	if err != nil {
		return nil, err
	}
	account, token, err := uc.access.open(ctx, workspaceID, current.AdAccountID, ads.UseWrite)
	if err != nil {
		return nil, err
	}
	return &datasetConnection{settings: current, account: account, token: token, waba: waba}, nil
}

func (uc *ConversionsUseCase) CheckConnectDataset(ctx context.Context, workspaceID, businessPhoneID string) (*ads.AdAccount, error) {
	c, err := uc.prepareConnect(ctx, workspaceID, businessPhoneID)
	if err != nil {
		return nil, err
	}
	return c.account, nil
}

func (uc *ConversionsUseCase) ConnectDataset(ctx context.Context, workspaceID, businessPhoneID string) (*ads.ConversionSettings, error) {
	c, err := uc.prepareConnect(ctx, workspaceID, businessPhoneID)
	if err != nil {
		return nil, err
	}
	dataset, err := uc.gateway.DatasetForWABA(ctx, c.token, c.waba)
	if err != nil {
		return nil, uc.access.failed(ctx, c.account, err)
	}
	current := c.settings
	current.DatasetID = dataset
	current.UpdatedAt = uc.access.now()
	if err := uc.settings.Save(ctx, current); err != nil {
		return nil, err
	}
	return current, nil
}

func (uc *ConversionsUseCase) Recent(ctx context.Context, workspaceID string) ([]ads.ConversionRecord, error) {
	return uc.outbox.Recent(ctx, workspaceID, recentConversions)
}

func (uc *ConversionsUseCase) DispatchAll(ctx context.Context) error {
	enabled, err := uc.settings.ListEnabled(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, s := range enabled {
		if err := uc.dispatch(ctx, *s); err != nil {
			log.Printf("[ads] conversions of workspace %s not sent: %v", s.WorkspaceID, err)
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("ads: %d workspace(s) failed to send conversions: %w", len(failures), errors.Join(failures...))
	}
	return nil
}

func (uc *ConversionsUseCase) dispatch(ctx context.Context, s ads.ConversionSettings) error {
	now := uc.access.now()
	pending, err := uc.outbox.Pending(ctx, s.WorkspaceID, now.Add(-conversionLookback), conversionBatch)
	if err != nil || len(pending) == 0 {
		return err
	}
	account, token, err := uc.access.open(ctx, s.WorkspaceID, s.AdAccountID, ads.UseWrite)
	if err != nil {
		return err
	}
	var events []ads.ConversionEvent
	for _, p := range pending {
		event, skip := ads.ConversionFor(s, p.Signal, now)
		if event == nil {
			uc.record(ctx, s.WorkspaceID, p.Signal.OpportunityID, eventNameOf(p.Signal.Event), ads.ConversionSkipped, string(skip), now)
			continue
		}
		events = append(events, *event)
	}
	events = uc.claim(ctx, s.WorkspaceID, events, now)
	if len(events) == 0 {
		return nil
	}
	sendErr := uc.gateway.SendEvents(ctx, token, events)
	status, reason := ads.ConversionSent, ""
	if sendErr != nil {
		status, reason = ads.ConversionFailed, ads.FailureCode(sendErr)
	}
	for _, e := range events {
		uc.record(ctx, s.WorkspaceID, e.OpportunityID, e.Name, status, reason, now)
	}
	return uc.access.failed(ctx, account, sendErr)
}

func eventNameOf(e ads.DealEvent) string {
	if e == ads.DealWon {
		return ads.EventNamePurchase
	}
	return ads.EventNameLead
}

func (uc *ConversionsUseCase) record(ctx context.Context, workspaceID, opportunityID, name string, status ads.ConversionStatus, reason string, now time.Time) {
	r := ads.ConversionRecord{OpportunityID: opportunityID, EventName: name, WorkspaceID: workspaceID, Status: status, Reason: reason, UpdatedAt: now}
	if status == ads.ConversionSent {
		r.SentAt = &now
	}
	if err := uc.outbox.Record(ctx, r); err != nil {
		log.Printf("[ads] conversion %s/%s outcome not recorded: %v", opportunityID, name, err)
	}
}

func (uc *ConversionsUseCase) claim(ctx context.Context, workspaceID string, events []ads.ConversionEvent, now time.Time) []ads.ConversionEvent {
	claimed := make([]ads.ConversionEvent, 0, len(events))
	for _, e := range events {
		r := ads.ConversionRecord{OpportunityID: e.OpportunityID, EventName: e.Name, WorkspaceID: workspaceID, Status: ads.ConversionSending, UpdatedAt: now}
		if err := uc.outbox.Record(ctx, r); err != nil {
			log.Printf("[ads] conversion %s not claimed, not sent: %v", e.EventID, err)
			continue
		}
		claimed = append(claimed, e)
	}
	return claimed
}
