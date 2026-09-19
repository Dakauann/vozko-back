package unofficial_whatsapp

import (
	"errors"
	"strings"
)

// Seeding opens a conversation with someone who has never written in and who
// nobody has written to, so a lead that arrived through a CSV is answerable
// from the inbox instead of only from the leads page.
//
// It is the cold-outbound flow minus the outbound: same contact, same lead
// bridge, same conversation, and no provider call at all. Nothing here spends
// the instance's send budget or reaches WhatsApp, which is what makes it safe
// to run over a list the size of an import.

// ErrSeedNoTargets means every row was rejected by Normalize.
//
// Distinct from a successful seed of nothing: the caller asked for work and
// there is none to do, and saying so beats publishing a message that wakes a
// consumer to discover the same thing.
var ErrSeedNoTargets = errors.New("unofficial whatsapp: seed request has no usable targets")

const (
	// SeedExchange is seeding's OWN exchange, not the campaign one. A single
	// lead import can enqueue two hundred batches, and that backlog has no
	// business sharing a topology with the queue that sends to customers.
	SeedExchange = "unofficial_whatsapp_seed_exchange"

	// SeedTopic is the queue topic one batch of seeding travels on.
	SeedTopic = "unofficial_whatsapp_inbox_seed"
)

// SeedBatchSize is how many numbers ride in one queue message.
//
// A lead import accepts MaxImportRows (100.000) rows, and one message carrying
// all of them is the message the broker refuses. Bounded rather than unbounded
// for the same reason numberCheckBatchSize is: the failure of a single enormous
// request is total, while the failure of one batch of five hundred is
// recoverable and leaves the rest of the import seeded.
const SeedBatchSize = 500

// minSeedPhoneDigits is the shortest string that could address a real
// international number.
//
// Below this the row is a mis-mapped column rather than a person. Seeding it
// would create a contact nobody can reach and an inbox row nobody can close,
// which is worse than not seeding it: the operator has to work out, one at a
// time, which of their new conversations are real.
const minSeedPhoneDigits = 8

// SeedTarget is one person to open a conversation with.
type SeedTarget struct {
	// Number in whatever shape the import had it. Normalized to digits here.
	Number string `json:"number"`
	// Name is used only when the contact is new, matching ResolveInput: a number
	// already known keeps the name WhatsApp gave it.
	Name string `json:"name,omitempty"`
}

// SeedRequest is one batch of seeding for one workspace.
type SeedRequest struct {
	WorkspaceID string       `json:"workspaceId"`
	Targets     []SeedTarget `json:"targets"`

	// Script, when set, makes these conversations open with the operator's
	// message and the short exchange that followed it instead of a blank
	// placeholder. Nil is the ordinary empty-chat seeding this feature started
	// as, and every path below stays exactly as it was when it is nil.
	//
	// It rides IN the payload rather than on a second topic: the work is the
	// same work on the same targets, and a separate exchange would mean two
	// consumers racing to resolve the same numbers.
	Script *SeedScript `json:"script,omitempty"`
}

// Normalize reduces every number to digits and drops what cannot be addressed.
//
// Dropping rather than failing: an import is a hand-managed spreadsheet, and
// one unparseable row must not cost the other ninety-nine thousand their inbox
// entries. The rows themselves were already reported to the operator by the
// import's own rejection list, so nothing disappears silently.
func (r *SeedRequest) Normalize() {
	r.WorkspaceID = strings.TrimSpace(r.WorkspaceID)

	r.Script.Normalize()
	// A script that normalises down to nothing is DROPPED rather than left to
	// fail validation. The operator ticked a box and typed only whitespace;
	// their leads still import and their conversations still open, plain.
	if r.Script != nil && len(r.Script.Bodies) == 0 {
		r.Script = nil
	}

	out := make([]SeedTarget, 0, len(r.Targets))
	seen := make(map[string]struct{}, len(r.Targets))

	for _, target := range r.Targets {
		raw := strings.TrimSpace(target.Number)
		// A group id survives NormalizePhone as the digits of its subject line,
		// which is addressable nonsense. Refuse it by its shape, before the
		// digits are taken, exactly as PhoneFromJID does.
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

// Validate reports whether this request can be acted on at all.
func (r *SeedRequest) Validate() error {
	if r.WorkspaceID == "" {
		return ErrWorkspaceIDRequired
	}
	if len(r.Targets) == 0 {
		return ErrSeedNoTargets
	}
	// The script's error is the request's error rather than a field quietly
	// ignored: a caller that sent variants using different variables has a bug,
	// and seeding plain instead would hide it until an operator found a raw
	// "{{2}}" in a customer conversation.
	return r.Script.Validate()
}

// ScriptedCount is how many of these targets will actually get a written
// thread.
//
// The truth, never the ask. It is what the import response tells the operator,
// and "500 conversas de exemplo" over a 500-row import would be a lie by three
// hundred: only the first MaxScriptedTargets cost anything.
func (r SeedRequest) ScriptedCount() int {
	if r.Script == nil {
		return 0
	}
	if len(r.Targets) < MaxScriptedTargets {
		return len(r.Targets)
	}
	return MaxScriptedTargets
}

// SeedQueued is what one publish accepted.
//
// Two numbers, because the import response has to say two things and one
// number cannot say both: every imported conversation is queued, and only the
// first MaxScriptedTargets of them will carry a written thread. "500 na fila"
// on its own leaves the operator expecting five hundred example conversations.
//
// It lives beside SeedRequest rather than with the publisher so the HTTP layer
// can name it without importing a channel use case.
type SeedQueued struct {
	// Targets is how many numbers reached the queue, scripted or not.
	Targets int
	// Scripted is how many of those will have a thread written for them.
	Scripted int
}

// Split cuts the request into publishable batches, covering every target
// exactly once.
//
// With a script, the split carries the whole money story:
//
//   - The FIRST MaxScriptedTargets targets are cut into ScriptedSeedBatchSize
//     batches that carry the script. Small, because each one costs model
//     latency on top of the database work and a batch is the unit of retry.
//   - Every target after that is cut into ordinary SeedBatchSize batches that
//     do NOT. Nothing is dropped: row 201 still lands in the inbox, as today's
//     plain empty chat.
//   - Scripted batches are published FIRST, so the visible part of the import
//     happens first.
//
// The cap is applied HERE, in the domain, so it cannot be bypassed by a caller
// that builds its own batches and publishes them itself.
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

// splitPlain is the original, unscripted split: whole batches of SeedBatchSize
// carrying no script. It is the whole of Split when nothing was scripted, and
// the tail of it when something was.
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
