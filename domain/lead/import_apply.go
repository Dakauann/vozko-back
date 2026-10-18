package lead

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"vozko/domain/customfield"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
)

const (
	EventImported       = recordevent.Kind("imported")
	EventImportEnriched = recordevent.Kind("import_enriched")
)

type ImportVerdict string

const (
	ImportCreated   ImportVerdict = "created"
	ImportEnriched  ImportVerdict = "enriched"
	ImportUnchanged ImportVerdict = "unchanged"
	ImportSkipped   ImportVerdict = "skipped"
	ImportRejected  ImportVerdict = "rejected"
)

type ImportRules struct {
	Policy      ExistingPolicy
	Job         string
	RequestedBy string
	Definitions []*customfield.Definition
	Viewer      customfield.Viewer
}

type ImportDecision struct {
	Verdict       ImportVerdict
	Lead          *Lead
	Changed       []string
	Conflicts     []string
	Issues        []ImportIssue
	NewPhones     []ContactPhone
	NewAddress    *Address
	FilledAddress *Address
}

func (d ImportDecision) Writes() bool {
	return d.Verdict == ImportCreated || d.Verdict == ImportEnriched
}

func (d ImportDecision) Event(actorID string, before *Lead, defs []*customfield.Definition) recordevent.Event {
	kind := EventImportEnriched
	if d.Verdict == ImportCreated {
		kind, before = EventImported, nil
	}
	return Changes(kind, actorID, before, d.Lead, defs)
}

func ApplyImport(existing *Lead, row ImportRecord, rules ImportRules) (ImportDecision, error) {
	if existing == nil {
		return createImported(row, rules)
	}
	if !existing.IsAggregate() {
		return ImportDecision{}, ErrAggregateNotLoaded
	}
	if rules.Policy != PolicyFillEmpty {
		return ImportDecision{Verdict: ImportSkipped}, nil
	}
	f := newGapFill(existing, row.Line)
	f.name(SourceImport, row.Name)
	f.text(FieldNickname, existing.Nickname, row.Nickname, func(n *Lead) { n.Nickname = row.Nickname })
	f.text(FieldEmail, existing.Email, row.Email, func(n *Lead) { n.Email = row.Email })
	f.text(FieldOwner, existing.Owner, row.Owner, func(n *Lead) { n.Owner = row.Owner })
	f.birthDate(row.BirthDate)
	if err := f.consent(row.consentChange(rules)); err != nil {
		return ImportDecision{}, err
	}
	if patch := f.customFields(row.CustomFields); len(patch) > 0 {
		if f.next.CustomFields == nil {
			f.next.CustomFields = map[string]any{}
		}
		maps.Copy(f.next.CustomFields, patch)
	}
	f.phones(row.Phones)
	if row.Address != nil {
		if err := f.address(*row.Address); err != nil {
			f.issues = append(f.issues, ImportIssue{Line: row.Line, Reason: ReasonAddressInvalid, Field: ImportFieldAddress})
		}
	}
	if err := f.next.ValidateRecord(); err != nil {
		return rejected(row.Line, ReasonRecordInvalid), nil
	}
	decision := ImportDecision{Verdict: ImportUnchanged, Conflicts: sortedFields(f.conflicts), Issues: f.issues}
	if len(f.changed) == 0 {
		return decision, nil
	}
	decision.Verdict, decision.Lead, decision.Changed = ImportEnriched, f.next, sortedFields(f.changed)
	decision.NewPhones, decision.NewAddress, decision.FilledAddress = f.newPhones, f.newAddress, f.filledAddress
	return decision, nil
}

func createImported(row ImportRecord, rules ImportRules) (ImportDecision, error) {
	l := &Lead{
		WorkspaceID: strings.TrimSpace(row.WorkspaceID), Number: row.Number, Source: SourceImport, Version: 1,
		Nickname: row.Nickname, Email: row.Email, Owner: row.Owner,
		CustomFields: maps.Clone(row.CustomFields), Phones: slices.Clone(row.Phones), Addresses: []Address{}, Relations: []Relation{},
	}
	if l.Phones == nil {
		l.Phones = []ContactPhone{}
	}
	if row.Name != "" {
		l.Name, l.NameSource = row.Name, SourceImport
	}
	if row.BirthDate != nil {
		date := *row.BirthDate
		l.BirthDate = &date
	}
	if row.Address != nil {
		l.Addresses = []Address{*row.Address}
	}
	if !l.HasIdentity() && l.RealName() == "" {
		return rejected(row.Line, ReasonIdentityRequired), nil
	}
	if grant := row.consentChange(rules); grant != nil {
		if _, err := l.ApplyConsentChange(*grant); err != nil {
			return ImportDecision{}, err
		}
	}
	if l.Validate() != nil {
		return rejected(row.Line, ReasonRecordInvalid), nil
	}
	decision := ImportDecision{Verdict: ImportCreated, Lead: l, Changed: sortedFields(l.recordFieldNames())}
	for _, key := range customfield.MissingRequired(rules.Definitions, rules.Viewer, l.CustomFields) {
		decision.Issues = append(decision.Issues, ImportIssue{Line: row.Line, Reason: ReasonCustomFieldRequired, Field: ImportCustomField(key)})
	}
	return decision, nil
}

func (r ImportRecord) consentChange(rules ImportRules) *ConsentChange {
	if r.Consent == nil {
		return nil
	}
	by := ConsentActor{Type: ConsentActorSystem}
	if id := strings.TrimSpace(rules.RequestedBy); id != "" {
		by = ConsentActor{Type: ConsentActorUser, ID: id}
	}
	return &ConsentChange{
		Kind: ConsentGrant, Source: ConsentImport, Actor: by, Purpose: r.Consent.Purpose, At: r.Consent.GrantedAt,
		Evidence: ConsentEvidence{Type: EvidenceImportLine, Ref: importLineRef(rules.Job, r.Line)},
	}
}

func importLineRef(job string, line int) string {
	job = strings.TrimSpace(job)
	if job == "" {
		return ""
	}
	return job + ":" + strconv.Itoa(line)
}

func rejected(line int, reason RejectReason) ImportDecision {
	return ImportDecision{Verdict: ImportRejected, Issues: []ImportIssue{{Line: line, Reason: reason, Rejected: true}}}
}

func (l *Lead) recordFieldNames() []string {
	return slices.Collect(maps.Keys(l.RecordFields()))
}

func MatchByContact(row ImportRecord, holders []*Lead) *Lead {
	if row.Number != "" || len(row.Phones) == 0 {
		return nil
	}
	name := shared.FoldForMatch(NormalizeName(row.Name))
	if name == "" {
		return nil
	}
	contact := &Lead{Phones: row.Phones}
	var match *Lead
	for _, h := range holders {
		if h == nil || shared.FoldForMatch(h.RealName()) != name || !contact.sharesAPhoneWith(h) {
			continue
		}
		if match != nil && match.ID != h.ID {
			return nil
		}
		match = h
	}
	return match
}
