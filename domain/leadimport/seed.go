package leadimport

import "vozko/domain/unofficial_whatsapp"

func SeedBatches(workspaceID string, targets []unofficial_whatsapp.SeedTarget, script *unofficial_whatsapp.SeedScript) []unofficial_whatsapp.SeedRequest {
	req := unofficial_whatsapp.SeedRequest{WorkspaceID: workspaceID, Targets: targets, Script: script}
	req.Normalize()
	return req.Split()
}

func (o *SeedOutcome) Resume(batches []unofficial_whatsapp.SeedRequest) int {
	if o.Sending > o.Batches {
		for _, b := range batches[min(o.Batches, len(batches)):min(o.Sending, len(batches))] {
			o.Unconfirmed += len(b.Targets)
		}
		o.Batches = o.Sending
	}
	o.Batches = min(o.Batches, len(batches))
	o.Sending = o.Batches
	return o.Batches
}

func (o *SeedOutcome) Begin() {
	o.Sending = o.Batches + 1
}

func (o *SeedOutcome) Sent(queued unofficial_whatsapp.SeedQueued) {
	o.Batches = o.Sending
	o.Queued += queued.Targets
	o.ScriptedQueued += queued.Scripted
}

func (o *SeedOutcome) Failed() {
	o.Error = SeedFailed
	o.Sending = o.Batches
}

func (o SeedOutcome) Done(batches []unofficial_whatsapp.SeedRequest) bool {
	return o.Error != "" || o.Batches >= len(batches)
}

type Placement struct {
	OnMap         int
	Approximate   int
	Pending       int
	NotFound      int
	QuotaExceeded int
	Refused       int
}
