package lead_usecase

import (
	"context"

	"vozko/domain/lead"
	"vozko/domain/leadimport"
	"vozko/domain/unofficial_whatsapp"
)

func (i *Import) seed(ctx context.Context, job *leadimport.Job, src rowSource) error {
	s := job.Settings
	if s == nil || !s.SeedInbox {
		job.Seed = nil
		return nil
	}
	switch {
	case !job.Grants.Seed:
		job.Seed = &leadimport.SeedOutcome{Error: leadimport.SeedForbidden}
		return nil
	case i.deps.Seeder == nil:
		job.Seed = &leadimport.SeedOutcome{Error: leadimport.SeedUnavailable}
		return nil
	}
	targets, err := i.seedTargets(job, src)
	if err != nil {
		return err
	}
	out := job.Seed
	if out == nil {
		out = &leadimport.SeedOutcome{}
	}
	script := s.Script
	if script != nil && !job.Grants.Script {
		out.ScriptError, script = leadimport.SeedScriptForbidden, nil
	}
	batches := leadimport.SeedBatches(job.WorkspaceID, targets, script)
	unconfirmed := out.Unconfirmed
	if out.Resume(batches); out.Unconfirmed > unconfirmed {
		i.log(job).Warn("lead import: a seed batch may or may not have reached the queue before a restart and is not sent again",
			"targets", out.Unconfirmed-unconfirmed)
	}
	job.Seed = out
	for !out.Done(batches) {
		out.Begin()
		if err := i.heartbeat(ctx, job, job.Processed); err != nil {
			out.Sending = out.Batches
			return err
		}
		queued, err := i.deps.Seeder.Publish(batches[out.Batches])
		if err != nil {
			out.Failed()
			i.log(job).Error("lead import: the inbox seed stopped at a queue failure", "batch", out.Batches, "error", err)
			break
		}
		out.Sent(queued)
	}
	return nil
}

func (i *Import) seedTargets(job *leadimport.Job, src rowSource) ([]unofficial_whatsapp.SeedTarget, error) {
	preparer := lead.NewImportPreparer(job.WorkspaceID, nil, i.now())
	var targets []unofficial_whatsapp.SeedTarget
	err := src(func(_ int, row lead.ImportRow) error {
		row.CustomFields = nil
		if record, _, ok := preparer.Prepare(row); ok && record.Number != "" {
			targets = append(targets, unofficial_whatsapp.SeedTarget{Number: record.Number, Name: record.Name})
		}
		return nil
	})
	return targets, err
}
