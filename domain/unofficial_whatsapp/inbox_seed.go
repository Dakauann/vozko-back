package unofficial_whatsapp

import (
	"errors"
	"strings"
)

var ErrSeedNoTargets = errors.New("unofficial whatsapp: seed request has no usable targets")

const (
	SeedExchange = "unofficial_whatsapp_seed_exchange"

	SeedTopic = "unofficial_whatsapp_inbox_seed"
)

const SeedBatchSize = 500

const minSeedPhoneDigits = 8

type SeedTarget struct {
	Number string `json:"number"`
	Name   string `json:"name,omitempty"`
}

type SeedRequest struct {
	WorkspaceID string       `json:"workspaceId"`
	Targets     []SeedTarget `json:"targets"`

	Script *SeedScript `json:"script,omitempty"`
}

func (r *SeedRequest) Normalize() {
	r.WorkspaceID = strings.TrimSpace(r.WorkspaceID)

	r.Script.Normalize()
	if r.Script != nil && len(r.Script.Bodies) == 0 {
		r.Script = nil
	}

	out := make([]SeedTarget, 0, len(r.Targets))
	seen := make(map[string]struct{}, len(r.Targets))

	for _, target := range r.Targets {
		raw := strings.TrimSpace(target.Number)
		if IsGroupJID(raw) || IsNewsletterJID(raw) {
			continue
		}
		number := NormalizePhone(raw)
		if len(number) < minSeedPhoneDigits {
			continue
		}
		if _, duplicate := seen[number]; duplicate {
			continue
		}
		seen[number] = struct{}{}
		out = append(out, SeedTarget{
			Number: number,
			Name:   strings.TrimSpace(target.Name),
		})
	}

	r.Targets = out
}

func (r *SeedRequest) Validate() error {
	if r.WorkspaceID == "" {
		return ErrWorkspaceIDRequired
	}
	if len(r.Targets) == 0 {
		return ErrSeedNoTargets
	}
	return r.Script.Validate()
}

func (r SeedRequest) ScriptedCount() int {
	if r.Script == nil {
		return 0
	}
	if len(r.Targets) < MaxScriptedTargets {
		return len(r.Targets)
	}
	return MaxScriptedTargets
}

type SeedQueued struct {
	Targets  int
	Scripted int
}

func (r *SeedRequest) Split() []SeedRequest {
	if len(r.Targets) == 0 {
		return nil
	}
	if r.Script == nil {
		return r.splitPlain(r.Targets)
	}

	cut := len(r.Targets)
	if cut > MaxScriptedTargets {
		cut = MaxScriptedTargets
	}

	batches := make([]SeedRequest, 0,
		(cut+ScriptedSeedBatchSize-1)/ScriptedSeedBatchSize+
			(len(r.Targets)-cut+SeedBatchSize-1)/SeedBatchSize)

	for start := 0; start < cut; start += ScriptedSeedBatchSize {
		end := start + ScriptedSeedBatchSize
		if end > cut {
			end = cut
		}
		batches = append(batches, SeedRequest{
			WorkspaceID: r.WorkspaceID,
			Targets:     r.Targets[start:end],
			Script:      r.Script,
		})
	}
	return append(batches, r.splitPlain(r.Targets[cut:])...)
}

func (r *SeedRequest) splitPlain(targets []SeedTarget) []SeedRequest {
	if len(targets) == 0 {
		return nil
	}
	batches := make([]SeedRequest, 0, (len(targets)+SeedBatchSize-1)/SeedBatchSize)
	for start := 0; start < len(targets); start += SeedBatchSize {
		end := start + SeedBatchSize
		if end > len(targets) {
			end = len(targets)
		}
		batches = append(batches, SeedRequest{
			WorkspaceID: r.WorkspaceID,
			Targets:     targets[start:end],
		})
	}
	return batches
}
