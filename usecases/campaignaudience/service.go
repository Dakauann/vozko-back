package campaignaudience_usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/cache"
	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	"vozko/domain/emailhygiene"
	"vozko/domain/lead"
	"vozko/domain/selection"
)

const (
	rowBatch    = 1000
	sweepBatch  = 200
	countPrefix = "campaign-audience:count:"
)

var ErrPageInvalid = errors.New("campaign audience: the page must be 1 or more and hold 1 to 200 rows")

type Deps struct {
	Selections  Selections
	Lists       Lists
	Sheets      Sheets
	Phones      Phones
	Emails      Emails
	EmailLeads  EmailLeads
	EmailsSeen  emailhygiene.AddressSeen
	Definitions Definitions
	Suggestions Suggestions
	Templates   TemplateReader
	Channels    []Channel
	Gate        cache.Gate
	Flight      cache.Flight
	Now         func() time.Time
	NewID       func() string
}

type Service struct {
	deps     Deps
	channels map[campaignaudience.Channel]Channel
}

func New(deps Deps) (*Service, error) {
	missing := map[string]bool{
		"selections": deps.Selections == nil, "lists": deps.Lists == nil, "sheets": deps.Sheets == nil,
		"phones": deps.Phones == nil, "emails": deps.Emails == nil, "email leads": deps.EmailLeads == nil, "emails seen": deps.EmailsSeen == nil,
		"definitions": deps.Definitions == nil, "gate": deps.Gate == nil, "flight": deps.Flight == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", campaignaudience.ErrUnavailable, name)
		}
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.NewID == nil {
		deps.NewID = uuid.NewString
	}
	s := &Service{deps: deps, channels: map[campaignaudience.Channel]Channel{}}
	for _, ch := range deps.Channels {
		if ch != nil {
			s.channels[ch.Name()] = ch
		}
	}
	return s, nil
}

func (s *Service) AddChannel(ch Channel) {
	if ch != nil {
		s.channels[ch.Name()] = ch
	}
}

func (s *Service) now() time.Time {
	return s.deps.Now().UTC()
}

func (s *Service) channel(name campaignaudience.Channel) (Channel, error) {
	if !name.Valid() {
		return nil, fmt.Errorf("%w: %q", campaignaudience.ErrChannelInvalid, name)
	}
	ch, ok := s.channels[name]
	if !ok {
		return nil, fmt.Errorf("%w: channel %s", campaignaudience.ErrUnavailable, name)
	}
	return ch, nil
}

type walk struct {
	actor   Actor
	channel campaignaudience.Channel
	params  Params
	spec    campaignaudience.Spec
	owner   string
	confirm bool
}

type visitor func(c campaignaudience.Candidate, d campaignaudience.Decision) error

func (s *Service) Count(ctx context.Context, a Actor, ch campaignaudience.Channel, params Params, spec campaignaudience.Spec) (campaignaudience.Count, error) {
	key, err := countKey(a, ch, params, spec)
	if err != nil {
		return campaignaudience.Count{}, err
	}
	raw, err := s.deps.Flight.Share(ctx, key, func(ctx context.Context) ([]byte, error) {
		var count campaignaudience.Count
		err := cache.Gated(ctx, s.deps.Gate, func(ctx context.Context) error {
			_, counted, err := s.walk(ctx, walk{actor: a, channel: ch, params: params, spec: spec}, nil)
			count = counted
			return err
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(count)
	})
	if errors.Is(err, cache.ErrSharedRunUnavailable) {
		return campaignaudience.Count{}, fmt.Errorf("%w: %w", campaignaudience.ErrUnavailable, err)
	}
	if err != nil {
		return campaignaudience.Count{}, err
	}
	var count campaignaudience.Count
	if err := json.Unmarshal(raw, &count); err != nil {
		return campaignaudience.Count{}, fmt.Errorf("%w: %w", campaignaudience.ErrUnavailable, err)
	}
	return count, nil
}

func countKey(a Actor, ch campaignaudience.Channel, params Params, spec campaignaudience.Spec) (string, error) {
	raw, err := json.Marshal(struct {
		Actor   Actor
		Channel campaignaudience.Channel
		Params  Params
		Spec    campaignaudience.Spec
	}{a, ch, params, spec.Normalized()})
	if err != nil {
		return "", fmt.Errorf("%w: %w", campaignaudience.ErrUnavailable, err)
	}
	sum := sha256.Sum256(raw)
	return countPrefix + hex.EncodeToString(sum[:]), nil
}

func (s *Service) Walk(ctx context.Context, a Actor, ch campaignaudience.Channel, params Params, spec campaignaudience.Spec, owner string, visit func(campaignaudience.Candidate, campaignaudience.Decision) error) (campaignaudience.Count, error) {
	_, count, err := s.walk(ctx, walk{actor: a, channel: ch, params: params, spec: spec, owner: owner}, visit)
	return count, err
}

func (s *Service) Recipients(ctx context.Context, a Actor, ch campaignaudience.Channel, params Params, spec campaignaudience.Spec) ([]Recipient, error) {
	spec = spec.Normalized()
	if err := spec.ValidateForCreate(); err != nil {
		return nil, err
	}
	var recipients []Recipient
	err := cache.Gated(ctx, s.deps.Gate, func(ctx context.Context) error {
		recipients = nil
		tally, _, err := s.walk(ctx, walk{actor: a, channel: ch, params: params, spec: spec, confirm: true}, func(c campaignaudience.Candidate, d campaignaudience.Decision) error {
			recipients = append(recipients, recipientOf(spec.Source, c, d))
			return nil
		})
		if err != nil {
			return err
		}
		return tally.Creatable()
	})
	if err != nil {
		return nil, err
	}
	return recipients, nil
}

func recipientOf(source campaignaudience.Source, c campaignaudience.Candidate, d campaignaudience.Decision) Recipient {
	if source == campaignaudience.SourceList {
		values := d.Values
		if values == nil {
			values = c.Values
		}
		return Recipient{Number: c.Contact, Name: c.Name, Variables: values, Metadata: c.Metadata}
	}
	r := Recipient{LeadID: c.LeadID}
	switch d.Outcome {
	case campaignaudience.OutcomeSend:
		r.Variables = d.Values
	case campaignaudience.OutcomeOut:
		r.Skip, r.Missing = campaign.SkipReason(d.Reason), d.Missing
	}
	return r
}

func (s *Service) walk(ctx context.Context, w walk, visit visitor) (*campaignaudience.Tally, campaignaudience.Count, error) {
	w.spec = w.spec.Normalized()
	if err := w.spec.Validate(); err != nil {
		return nil, campaignaudience.Count{}, err
	}
	ch, err := s.channel(w.channel)
	if err != nil {
		return nil, campaignaudience.Count{}, err
	}
	var list *campaignaudience.List
	if w.spec.Source == campaignaudience.SourceList {
		if list, err = s.readable(ctx, w.actor, w.channel, w.spec.ListID, w.owner); err != nil {
			return nil, campaignaudience.Count{}, err
		}
	}
	prepared, err := ch.Prepare(ctx, w.actor, w.params)
	if err != nil {
		return nil, campaignaudience.Count{}, err
	}
	bound, err := campaignaudience.Bind(prepared.Slots, w.spec.Variables)
	if err != nil {
		return nil, campaignaudience.Count{}, err
	}
	if prepared.Open == nil {
		return nil, campaignaudience.Count{}, campaignaudience.ErrUnavailable
	}
	judge, err := prepared.Open(ctx, bound, w.spec.Source)
	if err != nil {
		return nil, campaignaudience.Count{}, err
	}
	tally := campaignaudience.NewTally(w.channel, w.spec.Source, bound).Requiring(prepared.Metadata).Awaiting(prepared.Awaits)
	add := func(c campaignaudience.Candidate) error {
		d := tally.Add(c)
		if visit == nil {
			return nil
		}
		return visit(c, d)
	}
	if w.spec.Source == campaignaudience.SourceList {
		defs, err := s.definitions(w.actor.WorkspaceID)
		if err != nil {
			return nil, campaignaudience.Count{}, err
		}
		matched, err := s.walkList(ctx, *list, judge, campaign.MapList(list.Layout, list.Channel.Contact(), campaignaudience.Fields(bound), defs), add)
		if err != nil {
			return nil, campaignaudience.Count{}, err
		}
		count := tally.Count()
		count.List = &matched
		return tally, count, nil
	}
	selected, err := s.walkLeads(ctx, w, judge, add)
	if err != nil {
		return nil, campaignaudience.Count{}, err
	}
	count := tally.Count()
	count.Selection = &selected
	return tally, count, nil
}

func (s *Service) walkLeads(ctx context.Context, w walk, judge Judge, add func(campaignaudience.Candidate) error) (campaignaudience.SelectionCount, error) {
	scope := scopeOf(w.actor)
	sel := *w.spec.Selection
	selected, err := s.deps.Selections.Selected(ctx, scope, sel)
	if err != nil {
		return campaignaudience.SelectionCount{}, err
	}
	if w.confirm {
		if err := sel.ConfirmCount(selected); err != nil {
			return campaignaudience.SelectionCount{}, err
		}
	}
	out := campaignaudience.SelectionCount{Selected: selected, ConfirmCount: sel.ConfirmableCount(selected)}
	if sel.Mode != selection.ModeIDs {
		out.Fingerprint = selection.Fingerprint(sel.EffectiveFilter())
	}
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return campaignaudience.SelectionCount{}, err
		}
		refs, err := s.deps.Selections.Resolve(ctx, scope, sel, after, selection.Chunk)
		if err != nil {
			return campaignaudience.SelectionCount{}, err
		}
		if len(refs) == 0 {
			return out, nil
		}
		ids := make([]string, len(refs))
		for i, ref := range refs {
			ids[i] = ref.ID
		}
		candidates, err := judge.Leads(ctx, ids)
		if err != nil {
			return campaignaudience.SelectionCount{}, err
		}
		for _, c := range candidates {
			if err := add(c); err != nil {
				return campaignaudience.SelectionCount{}, err
			}
		}
		if len(refs) < selection.Chunk {
			return out, nil
		}
		after = refs[len(refs)-1].ID
	}
}

func (s *Service) walkList(ctx context.Context, list campaignaudience.List, judge Judge, mapping campaign.ListMapping, add func(campaignaudience.Candidate) error) (campaignaudience.ListCount, error) {
	out := campaignaudience.ListCount{ID: list.ID}
	err := s.eachRowBatch(ctx, list, func(rows []campaignaudience.Row, existing map[string]string) error {
		candidates := make([]RowCandidate, 0, len(rows))
		for _, row := range rows {
			leadID := existing[row.Contact]
			if leadID != "" {
				out.Existing++
			} else {
				out.New++
			}
			out.Rows++
			candidates = append(candidates, RowCandidate{Row: row, LeadID: leadID, Values: mapping.Cells(row.ListRow), Metadata: mapping.Metadata(row.ListRow)})
		}
		judged, err := judge.Rows(ctx, candidates)
		if err != nil {
			return err
		}
		for _, c := range judged {
			if err := add(c); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

func (s *Service) eachRowBatch(ctx context.Context, list campaignaudience.List, fn func(rows []campaignaudience.Row, existing map[string]string) error) error {
	after := campaignaudience.RowCursor{Line: -1}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := s.deps.Lists.RowsAfter(ctx, list.ID, after, rowBatch)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		existing, err := s.existing(ctx, list, rows)
		if err != nil {
			return err
		}
		if err := fn(rows, existing); err != nil {
			return err
		}
		if len(rows) < rowBatch {
			return nil
		}
		last := rows[len(rows)-1]
		after = campaignaudience.RowCursor{Line: last.Line, ID: last.ID}
	}
}

func (s *Service) existing(ctx context.Context, list campaignaudience.List, rows []campaignaudience.Row) (map[string]string, error) {
	contacts := make([]string, 0, len(rows))
	for _, r := range rows {
		contacts = append(contacts, r.Contact)
	}
	out := make(map[string]string, len(rows))
	if list.Channel.Contact() == campaign.ContactEmail {
		found, err := s.deps.Emails.IDsByEmails(ctx, list.WorkspaceID, contacts)
		if err != nil {
			return nil, err
		}
		for _, c := range contacts {
			if id := found[c]; id != "" {
				out[c] = id
			}
		}
		return out, nil
	}
	found, err := s.deps.Phones.IdentityIDs(ctx, list.WorkspaceID, contacts)
	if err != nil {
		return nil, err
	}
	for _, c := range contacts {
		if id, ok := lead.FindByNumber(found, c); ok {
			out[c] = id
		}
	}
	return out, nil
}

func (s *Service) readable(ctx context.Context, a Actor, ch campaignaudience.Channel, listID, owner string) (*campaignaudience.List, error) {
	list, err := s.deps.Lists.Get(ctx, a.WorkspaceID, strings.TrimSpace(listID))
	if err != nil {
		return nil, err
	}
	if owner != "" {
		if err := list.OwnedBy(a.WorkspaceID, owner, ch); err != nil {
			return nil, err
		}
		return list, nil
	}
	if err := list.ReadableBy(a.WorkspaceID, a.UserID, ch, s.now()); err != nil {
		return nil, err
	}
	return list, nil
}
