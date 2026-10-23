package businessphone_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/messagestatus"
	"vozko/domain/whatsapp/quality"
)

var ErrPhoneNotListed = errors.New("meta does not list this phone number on its account")

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
	accounts, polled, err := uc.accountsOf(active)
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
			if active[info.ID] {
				changes = append(changes, moved(info, account.wabaID, now, ratings[info.ID], statuses[info.ID])...)
				errs = append(errs, uc.keepRating(polled[info.ID], info))
			}
		}
	}
	if err := uc.changes.Record(changes...); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (uc *PollPhoneQualityUseCase) RefreshPhone(ctx context.Context, phone *businessphone.WhatsAppBusinessPhoneNumber) (businessphone.MetaPhoneNumberInfo, error) {
	if err := ctx.Err(); err != nil {
		return businessphone.MetaPhoneNumberInfo{}, err
	}
	infos, err := uc.meta.ListPhoneNumbers(phone.WABAId, phone.AccessToken)
	if err != nil {
		return businessphone.MetaPhoneNumberInfo{}, fmt.Errorf("list phones of waba %s: %w", phone.WABAId, err)
	}
	for _, info := range infos {
		if info.ID != phone.MetaPhoneNumberID {
			continue
		}
		ids := []string{info.ID}
		ratings, err := uc.history.LatestValues(quality.SubjectPhone, quality.SignalQuality, ids)
		if err != nil {
			return info, err
		}
		statuses, err := uc.history.LatestValues(quality.SubjectPhone, quality.SignalStatus, ids)
		if err != nil {
			return info, err
		}
		if err := uc.changes.Record(moved(info, phone.WABAId, uc.now().UTC(), ratings[info.ID], statuses[info.ID])...); err != nil {
			return info, err
		}
		return info, uc.keepRating(phone, info)
	}
	return businessphone.MetaPhoneNumberInfo{}, fmt.Errorf("%w: %s", ErrPhoneNotListed, phone.MetaPhoneNumberID)
}

func (uc *PollPhoneQualityUseCase) keepRating(phone *businessphone.WhatsAppBusinessPhoneNumber, info businessphone.MetaPhoneNumberInfo) error {
	rating := businessphone.QualityRating(strings.ToUpper(strings.TrimSpace(info.QualityRating)))
	if phone == nil || rating == "" || rating == phone.QualityRating {
		return nil
	}
	if err := uc.phones.UpdateQualityRating(phone.ID, rating); err != nil {
		return fmt.Errorf("keep quality of phone %s: %w", phone.ID, err)
	}
	phone.QualityRating = rating
	return nil
}

func moved(info businessphone.MetaPhoneNumberInfo, wabaID string, now time.Time, previousRating, previousStatus string) []quality.Change {
	base := quality.Change{Subject: quality.SubjectPhone, ExternalID: info.ID, WABAID: wabaID, Name: info.DisplayPhoneNumber, Source: quality.SourcePoll, OccurredAt: now}
	var changes []quality.Change
	rating := base
	rating.Signal = quality.SignalQuality
	if change, ok := rating.Moved(previousRating, info.QualityRating); ok {
		changes = append(changes, change)
	}
	status := base
	status.Signal = quality.SignalStatus
	if change, ok := status.Moved(previousStatus, info.Status); ok {
		changes = append(changes, change)
	}
	return changes
}

func (uc *PollPhoneQualityUseCase) accountsOf(active map[string]bool) ([]accountToPoll, map[string]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	phones, err := uc.phones.ListAll()
	if err != nil {
		return nil, nil, fmt.Errorf("list phones to poll quality: %w", err)
	}
	seen := make(map[string]bool)
	polled := make(map[string]*businessphone.WhatsAppBusinessPhoneNumber)
	var accounts []accountToPoll
	for _, phone := range phones {
		if !active[phone.MetaPhoneNumberID] || !phone.QualityPollable() {
			continue
		}
		polled[phone.MetaPhoneNumberID] = phone
		if seen[phone.WABAId] {
			continue
		}
		seen[phone.WABAId] = true
		accounts = append(accounts, accountToPoll{wabaID: phone.WABAId, token: phone.AccessToken})
	}
	return accounts, polled, nil
}
