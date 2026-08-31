package unofficial_whatsapp_campaign

import (
	"context"
	"time"

	"vozko/domain/campaign"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

// numberCheckBatchSize is how many numbers go to the provider at once.
//
// Batched because a per-number round trip over a 40.000-row list is forty
// thousand HTTP calls; bounded because a single enormous request is the one the
// host times out on.
const numberCheckBatchSize = 200

// validateTargetsBudget caps one pre-flight run.
//
// A ceiling rather than "check everything": the send path re-checks anyway, so
// this is a convenience, and an unbounded pre-flight over a 150.000-row list
// would hold a provider connection for an hour.
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

// Execute checks pending numbers against WhatsApp and marks the dead ones.
//
// Optional — the consumer checks per send anyway — but an operator cleaning a
// purchased list before committing to it is the difference between a blast that
// is 30% dead on arrival and one that is not. Sending to unregistered numbers is
// the loudest spam signal a linked device can emit.
func (uc *validateTargetsUseCase) Execute(ctx context.Context, campaignID string) (*uwc.ValidateTargetsOutput, error) {
	camp, err := uc.repos.campaigns.FindByID(campaignID)
	if err != nil {
		return nil, err
	}
	instance, err := uc.instances.Instance(ctx, camp.InstanceID)
	if err != nil {
		return nil, err
	}
	// A dead session cannot answer the question. Refusing beats marking a whole
	// list as unreachable because our own connection was down.
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
			// Partial progress is kept: what has already been marked is correct,
			// and a second run picks up where this one stopped because a checked
			// entry is skipped by NeedsNumberCheck.
			return out, err
		}

		// Anything the provider did not answer for is left alone rather than
		// assumed dead: a missing answer is not a "no".
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
