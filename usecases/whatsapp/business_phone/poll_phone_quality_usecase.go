package businessphone_usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/messagestatus"
	"vozko/domain/whatsapp/quality"
)

type PollPhoneQualityUseCase struct {
	phones  businessphone.Repository
	meta    businessphone.MetaAPIService
	sending messagestatus.SendingPhones
	history quality.History
	changes quality.Recorder
	now     func() time.Time
}

func NewPollPhoneQualityUseCase(phones businessphone.Repository, meta businessphone.MetaAPIService, sending messagestatus.SendingPhones, history quality.History, changes quality.Recorder) *PollPhoneQualityUseCase {
	return &PollPhoneQualityUseCase{phones: phones, meta: meta, sending: sending, history: history, changes: changes, now: time.Now}
}

type accountToPoll struct {
	wabaID string
	token  string
}

func (uc *PollPhoneQualityUseCase) Execute(ctx context.Context) error {
	now := uc.now().UTC()
	sending, err := uc.sending.PhonesSendingSince(ctx, now.Add(-quality.PhonePollWindow))
	if err != nil {
		return err
	}
	if len(sending) == 0 {
		return nil
	}
	active := make(map[string]bool, len(sending))
	for _, id := range sending {
		active[id] = true
	}
	accounts, err := uc.accountsOf(active)
	if err != nil {
		return err
	}
	ratings, err := uc.history.LatestValues(quality.SubjectPhone, quality.SignalQuality, sending)
	if err != nil {
		return err
	}
	statuses, err := uc.history.LatestValues(quality.SubjectPhone, quality.SignalStatus, sending)
	if err != nil {
		return err
	}

	var errs []error
	var changes []quality.Change
	for _, account := range accounts {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		infos, err := uc.meta.ListPhoneNumbers(account.wabaID, account.token)
		if err != nil {
			errs = append(errs, fmt.Errorf("list phones of waba %s: %w", account.wabaID, err))
			continue
		}
		for _, info := range infos {
			if !active[info.ID] {
				continue
			}
			base := quality.Change{Subject: quality.SubjectPhone, ExternalID: info.ID, WABAID: account.wabaID, Name: info.DisplayPhoneNumber, Source: quality.SourcePoll, OccurredAt: now}
			rating := base
			rating.Signal = quality.SignalQuality
			if moved, ok := rating.Moved(ratings[info.ID], info.QualityRating); ok {
				changes = append(changes, moved)
			}
			status := base
			status.Signal = quality.SignalStatus
			if moved, ok := status.Moved(statuses[info.ID], info.Status); ok {
				changes = append(changes, moved)
			}
		}
	}
	if err := uc.changes.Record(changes...); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (uc *PollPhoneQualityUseCase) accountsOf(active map[string]bool) ([]accountToPoll, error) {
	phones, err := uc.phones.ListAll()
	if err != nil {
		return nil, fmt.Errorf("list phones to poll quality: %w", err)
	}
	seen := make(map[string]bool)
	var accounts []accountToPoll
	for _, phone := range phones {
		if !active[phone.MetaPhoneNumberID] || seen[phone.WABAId] || !phone.QualityPollable() {
			continue
		}
		seen[phone.WABAId] = true
		accounts = append(accounts, accountToPoll{wabaID: phone.WABAId, token: phone.AccessToken})
	}
	return accounts, nil
}
