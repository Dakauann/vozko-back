package smartcampaign_usecase

import (
	"context"
	"log"
	"sync/atomic"

	wce "vozko/domain/whatsapp_campaign_entry"
)

type FailureSink interface {
	EntryFailed(ctx context.Context, campaignID string, code int) error
}

type ObservedEntries struct {
	wce.Repository
	sink atomic.Pointer[FailureSink]
}

func ObserveFailures(entries wce.Repository) *ObservedEntries {
	return &ObservedEntries{Repository: entries}
}

func (o *ObservedEntries) Attach(sink FailureSink) {
	o.sink.Store(&sink)
}

func (o *ObservedEntries) UpdateStatus(entryID string, status wce.SendStatus, messageID string, errorCode int, errorMessage string) error {
	if err := o.Repository.UpdateStatus(entryID, status, messageID, errorCode, errorMessage); err != nil {
		return err
	}
	if sink := o.sink.Load(); sink != nil && status == wce.SendStatusFailed {
		go o.report(*sink, entryID, errorCode)
	}
	return nil
}

func (o *ObservedEntries) report(sink FailureSink, entryID string, code int) {
	entry, err := o.Repository.FindByID(entryID)
	if err != nil || entry == nil {
		log.Printf("[smart-campaign] failed entry %s could not be traced to its campaign: %v", entryID, err)
		return
	}
	if err := sink.EntryFailed(context.Background(), entry.CampaignID, code); err != nil {
		log.Printf("[smart-campaign] live check of %s after a failure: %v", entry.CampaignID, err)
	}
}
