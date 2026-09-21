package unofficial_whatsapp_campaign

import (
	"context"
	"time"

	"vozko/domain/campaign"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

const numberCheckBatchSize = 200

const validateTargetsBudget = 5000

type validateTargetsUseCase struct {
	repos     campaignRepos
	instances InstanceGateway
}

func NewValidateTargetsUseCase(
	campaigns uwc.Repository,
	entries uwc.EntryRepository,
	instances InstanceGateway,
) uwc.ValidateTargetsUseCase {
	return &validateTargetsUseCase{
		repos:     campaignRepos{campaigns: campaigns, entries: entries},
		instances: instances,
	}
}

func (uc *validateTargetsUseCase) Execute(ctx context.Context, campaignID string) (*uwc.ValidateTargetsOutput, error) {
	camp, err := uc.repos.campaigns.FindByID(campaignID)
	if err != nil {
		return nil, err
	}
	instance, err := uc.instances.Instance(ctx, camp.InstanceID)
	if err != nil {
		return nil, err
	}
	if _, err := instance.CanSend(time.Now().UTC()); err != nil {
		return nil, err
	}
	ref, err := uc.instances.Ref(ctx, instance)
	if err != nil {
		return nil, err
	}

	pending, err := uc.repos.entries.ListByStatus(
		campaignID, campaign.SendStatusPending, validateTargetsBudget)
	if err != nil {
		return nil, err
	}

	out := &uwc.ValidateTargetsOutput{CampaignID: campaignID}
	now := time.Now().UTC()

	for start := 0; start < len(pending); start += numberCheckBatchSize {
		end := start + numberCheckBatchSize
		if end > len(pending) {
			end = len(pending)
		}
		batch := pending[start:end]

		numbers := make([]string, 0, len(batch))
		byNumber := make(map[string]*uwc.Entry, len(batch))
		for i := range batch {
			entry := &batch[i]
			if !entry.NeedsNumberCheck(now) {
				continue
			}
			numbers = append(numbers, entry.Number)
			byNumber[entry.Number] = entry
		}
		if len(numbers) == 0 {
			continue
		}

		checks, err := uc.instances.CheckNumbers(ctx, ref, numbers)
		if err != nil {
			return out, err
		}

		for _, check := range checks {
			entry, ok := byNumber[check.Query]
			if !ok {
				continue
			}
			out.Checked++
			if check.IsOnWhatsApp {
				out.OnWhatsApp++
				_ = uc.repos.entries.RecordCheck(entry.ID, check.JID, now, true)
				continue
			}
			out.Skipped++
			_ = uc.repos.entries.RecordCheck(entry.ID, "", now, false)
		}
	}

	return out, nil
}
