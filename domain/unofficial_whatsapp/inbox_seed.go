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
}

// Normalize reduces every number to digits and drops what cannot be addressed.
//
// Dropping rather than failing: an import is a hand-managed spreadsheet, and
// one unparseable row must not cost the other ninety-nine thousand their inbox
// entries. The rows themselves were already reported to the operator by the
// import's own rejection list, so nothing disappears silently.
func (r *SeedRequest) Normalize() {
	r.WorkspaceID = strings.TrimSpace(r.WorkspaceID)

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
	return nil
}

// Split cuts the request into publishable batches, covering every target
// exactly once.
func (r *SeedRequest) Split() []SeedRequest {
	if len(r.Targets) == 0 {
		return nil
	}
	batches := make([]SeedRequest, 0, (len(r.Targets)+SeedBatchSize-1)/SeedBatchSize)
	for start := 0; start < len(r.Targets); start += SeedBatchSize {
		end := start + SeedBatchSize
		if end > len(r.Targets) {
			end = len(r.Targets)
		}
		batches = append(batches, SeedRequest{
			WorkspaceID: r.WorkspaceID,
			Targets:     r.Targets[start:end],
		})
	}
	return batches
}
