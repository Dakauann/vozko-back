package lead_usecase

import (
	"context"
	"errors"
	"slices"
	"strings"

	"vozko/domain/actor"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/leadimport"
	"vozko/domain/sheet"
	"vozko/domain/workspace"
)

type rowSource func(fn func(index int, row lead.ImportRow) error) error

func (i *Import) fileSource(ctx context.Context, job *leadimport.Job) (rowSource, error) {
	switch {
	case job.Settings == nil:
		return nil, leadimport.ErrNotReady
	case job.File.MediaID == "":
		return nil, leadimport.ErrInterrupted
	}
	data, err := i.deps.Files.Read(ctx, job.WorkspaceID, job.File.MediaID)
	if err != nil {
		return nil, err
	}
	settings := *job.Settings
	return func(fn func(int, lead.ImportRow) error) error {
		return leadimport.EachRow(data, func(index int, row sheet.Row) error { return fn(index, settings.RowOf(row)) })
	}, nil
}

func inBatches(src rowSource, skip int, replay func(lead.ImportRow), flush func(rows []lead.ImportRow) error) error {
	batch := make([]lead.ImportRow, 0, leadimport.BatchRows)
	err := src(func(index int, row lead.ImportRow) error {
		if index < skip {
			replay(row)
			return nil
		}
		batch = append(batch, row)
		if len(batch) < leadimport.BatchRows {
			return nil
		}
		err := flush(batch)
		batch = make([]lead.ImportRow, 0, leadimport.BatchRows)
		return err
	})
	if err != nil {
		return err
	}
	if len(batch) > 0 {
		return flush(batch)
	}
	return nil
}

type importPass struct {
	i        *Import
	job      *leadimport.Job
	defs     []*customfield.Definition
	viewer   customfield.Viewer
	preparer *lead.ImportPreparer
	owners   ownerResolver
}

func (i *Import) pass(job *leadimport.Job) (*importPass, error) {
	defs, err := i.definitions(job.WorkspaceID)
	if err != nil {
		return nil, err
	}
	a := Actor{UserID: job.RequestedBy, WorkspaceID: job.WorkspaceID, IsAdmin: job.RequestedByAdmin}
	return &importPass{
		i: i, job: job, defs: defs,
		viewer:   customfield.Viewer{ReadsSensitive: i.viewers.allowed(a, workspace.ActionReadSensitive)},
		preparer: lead.NewImportPreparer(job.WorkspaceID, defs, i.now()),
		owners:   ownerResolver{members: i.deps.Members, visibility: i.deps.Visibility, actor: a, resolved: map[string]ownerAnswer{}},
	}, nil
}

func (p *importPass) authorize() error {
	if p.job.Settings == nil {
		return leadimport.ErrNotReady
	}
	a := Actor{UserID: p.job.RequestedBy, WorkspaceID: p.job.WorkspaceID, IsAdmin: p.job.RequestedByAdmin}
	return p.i.permit(a, p.job.Settings.Requirements(p.defs)...)
}

func (p *importPass) decide(existing *lead.Lead, row lead.ImportRecord) (lead.ImportDecision, error) {
	return lead.ApplyImport(existing, row, lead.ImportRules{Policy: p.job.Settings.Policy, Definitions: p.defs, Viewer: p.viewer})
}

func (p *importPass) prepare(ctx context.Context, rows []lead.ImportRow) ([]leadimport.RowItem, []lead.ImportIssue, error) {
	records := make([]lead.ImportRecord, 0, len(rows))
	var issues []lead.ImportIssue
	for _, row := range rows {
		record, rowIssues, ok := p.preparer.Prepare(row)
		issues = append(issues, rowIssues...)
		if ok {
			records = append(records, record)
		}
	}
	ownerIssues, err := p.owners.resolve(ctx, records)
	if err != nil {
		return nil, nil, err
	}
	issues = append(issues, ownerIssues...)
	items, matchIssues, err := p.match(ctx, records)
	if err != nil {
		return nil, nil, err
	}
	return items, append(issues, matchIssues...), nil
}

func (p *importPass) match(ctx context.Context, records []lead.ImportRecord) ([]leadimport.RowItem, []lead.ImportIssue, error) {
	ws := p.job.WorkspaceID
	var identities []string
	for _, r := range records {
		if r.Number != "" {
			identities = append(identities, r.Number)
		}
	}
	byIdentity := map[string]*lead.Lead{}
	if len(identities) > 0 {
		found, err := p.i.deps.Lookup.ByIdentity(ctx, ws, identities)
		if err != nil {
			return nil, nil, err
		}
		byIdentity = found
	}
	contacts, err := p.contactHolders(ctx, records)
	if err != nil {
		return nil, nil, err
	}
	items := make([]leadimport.RowItem, 0, len(records))
	var issues []lead.ImportIssue
	used := map[string]bool{}
	for idx, r := range records {
		var existing *lead.Lead
		if r.Number != "" {
			existing, _ = lead.FindByNumber(byIdentity, r.Number)
		} else if holders, known := contacts.of(idx); !known {
			issues = append(issues, lead.ImportIssue{Line: r.Line, Reason: lead.ReasonContactAmbiguous, Field: lead.ImportFieldPhones, Rejected: true})
			continue
		} else {
			existing = lead.MatchByContact(r, holders)
		}
		if existing != nil {
			if used[existing.ID] {
				issues = append(issues, lead.ImportIssue{Line: r.Line, Reason: lead.ReasonDuplicate, Field: lead.ImportFieldName, Rejected: true})
				continue
			}
			used[existing.ID] = true
		}
		items = append(items, leadimport.RowItem{Record: r, Existing: existing})
	}
	return items, issues, nil
}

type contactLookup struct {
	perRow  map[int][]*lead.Lead
	unknown map[int]bool
}

func (c contactLookup) of(idx int) ([]*lead.Lead, bool) {
	if c.unknown[idx] {
		return nil, false
	}
	return c.perRow[idx], true
}

func (p *importPass) contactHolders(ctx context.Context, records []lead.ImportRecord) (contactLookup, error) {
	ws := p.job.WorkspaceID
	phones := make([][]string, len(records))
	var all []string
	for idx, r := range records {
		if r.Number != "" {
			continue
		}
		for _, phone := range r.Phones {
			phones[idx] = append(phones[idx], phone.Number)
			all = append(all, phone.Number)
		}
	}
	if len(all) == 0 {
		return contactLookup{}, nil
	}
	counts, err := p.i.deps.Lookup.PhoneHolderCounts(ctx, ws, all)
	if err != nil {
		return contactLookup{}, err
	}
	plan := leadimport.PlanContactLookups(phones, counts, leadimport.MaxContactHolders)
	lookup := contactLookup{perRow: map[int][]*lead.Lead{}, unknown: plan.Ambiguous}
	for g, group := range plan.Groups {
		found, err := p.i.deps.Lookup.HoldingPhones(ctx, ws, plan.PhonesOf(phones, g))
		if err != nil {
			return contactLookup{}, err
		}
		for _, idx := range group {
			lookup.perRow[idx] = found
		}
	}
	return lookup, nil
}

type ownerAnswer struct {
	id     string
	reason lead.RejectReason
}

type ownerResolver struct {
	members    leadimport.Members
	visibility MemberVisibility
	actor      Actor
	resolved   map[string]ownerAnswer
}

func (o *ownerResolver) resolve(ctx context.Context, records []lead.ImportRecord) ([]lead.ImportIssue, error) {
	var unknown []string
	for _, r := range records {
		if r.OwnerEmail == "" {
			continue
		}
		if _, ok := o.resolved[r.OwnerEmail]; !ok && !slices.Contains(unknown, r.OwnerEmail) {
			unknown = append(unknown, r.OwnerEmail)
		}
	}
	if len(unknown) > 0 {
		ids, err := o.members.UserIDsByEmail(ctx, o.actor.WorkspaceID, unknown)
		if err != nil {
			return nil, err
		}
		for _, email := range unknown {
			id, ok := ids[strings.ToLower(email)]
			if !ok {
				o.resolved[email] = ownerAnswer{reason: lead.ReasonOwnerNotFound}
				continue
			}
			visible, err := o.visibility.CanView(o.actor.UserID, id, o.actor.WorkspaceID, o.actor.IsAdmin)
			if err != nil {
				return nil, err
			}
			if !visible {
				o.resolved[email] = ownerAnswer{reason: lead.ReasonOwnerOutOfReach}
				continue
			}
			o.resolved[email] = ownerAnswer{id: actor.Join(id, actor.KindHuman)}
		}
	}
	var issues []lead.ImportIssue
	for idx := range records {
		r := &records[idx]
		if r.OwnerEmail == "" {
			continue
		}
		answer := o.resolved[r.OwnerEmail]
		if answer.id == "" {
			issues = append(issues, lead.ImportIssue{Line: r.Line, Reason: answer.reason, Field: lead.ImportFieldOwner})
			continue
		}
		r.Owner = answer.id
	}
	return issues, nil
}

func linkable(d lead.ImportDecision) bool {
	return d.Verdict == lead.ImportCreated || d.Verdict == lead.ImportEnriched || d.Verdict == lead.ImportUnchanged
}

func (i *Import) analyze(ctx context.Context, job *leadimport.Job) error {
	p, err := i.pass(job)
	if err != nil {
		return err
	}
	if err := p.authorize(); err != nil {
		return err
	}
	src, err := i.fileSource(ctx, job)
	if err != nil {
		return err
	}
	counts := leadimport.NewCounts()
	identities := map[string]bool{}
	var relatives []pendingRelative
	processed := 0
	err = inBatches(src, 0, nil, func(rows []lead.ImportRow) error {
		items, issues, err := p.prepare(ctx, rows)
		if err != nil {
			return err
		}
		for _, issue := range issues {
			counts.Issue(issue)
		}
		for _, item := range items {
			d, err := p.decide(item.Existing, item.Record)
			if err != nil {
				return err
			}
			counts.RecordRow(item.Record, d, item.Existing)
			for _, format := range lead.NumberFormats(item.Record.Number) {
				identities[format] = true
			}
			if item.Record.Relative != nil && linkable(d) {
				relatives = append(relatives, pendingRelative{line: item.Record.Line, number: item.Record.Relative.Number})
				counts.LinksPlanned++
			}
		}
		processed += len(rows)
		return i.heartbeat(ctx, job, processed)
	})
	if err != nil {
		return err
	}
	unresolved, err := i.unresolvedRelatives(ctx, job.WorkspaceID, relatives, identities)
	if err != nil {
		return err
	}
	for _, issue := range unresolved {
		counts.LinksPlanned--
		counts.Issue(issue)
	}
	claim := job.Claim
	if err := job.Analyzed(counts, processed, i.now()); err != nil {
		return err
	}
	return i.deps.Jobs.Save(ctx, job, leadimport.Guard{From: []leadimport.Status{leadimport.StatusAnalyzing}, Claim: claim})
}

type pendingRelative struct {
	line   int
	number string
}

func (i *Import) heartbeat(ctx context.Context, job *leadimport.Job, processed int) error {
	at := i.now()
	job.Processed, job.HeartbeatAt, job.UpdatedAt = processed, &at, at
	return i.deps.Jobs.Save(ctx, job, leadimport.Guard{From: []leadimport.Status{job.Status}, Claim: job.Claim})
}

func (i *Import) unresolvedRelatives(ctx context.Context, workspaceID string, relatives []pendingRelative, inFile map[string]bool) ([]lead.ImportIssue, error) {
	var missing []pendingRelative
	for _, r := range relatives {
		if !inFile[r.number] {
			missing = append(missing, r)
		}
	}
	var issues []lead.ImportIssue
	for start := 0; start < len(missing); start += leadimport.BatchRows {
		chunk := missing[start:min(start+leadimport.BatchRows, len(missing))]
		numbers := make([]string, len(chunk))
		for idx, r := range chunk {
			numbers[idx] = r.number
		}
		ids, err := i.deps.Lookup.IdentityIDs(ctx, workspaceID, numbers)
		if err != nil {
			return nil, err
		}
		for _, r := range chunk {
			if _, found := lead.FindByNumber(ids, r.number); !found {
				issues = append(issues, lead.ImportIssue{Line: r.line, Reason: lead.ReasonRelativeNotFound, Field: lead.ImportFieldRelative})
			}
		}
	}
	return issues, nil
}

func (i *Import) importAll(ctx context.Context, job *leadimport.Job) error {
	p, err := i.pass(job)
	if err != nil {
		return err
	}
	if err := p.authorize(); err != nil {
		return err
	}
	src, err := i.fileSource(ctx, job)
	if err != nil {
		return err
	}
	if job.Result == nil {
		job.Result = &leadimport.Counts{}
	}
	if job.Stage == "" || job.Stage == leadimport.StageRows {
		if err := i.writeRows(ctx, p, src); err != nil {
			return err
		}
		if err := i.advance(ctx, job, leadimport.StageLinks); err != nil {
			return err
		}
	}
	if job.Stage == leadimport.StageLinks {
		if err := i.writeLinks(ctx, job); err != nil {
			return err
		}
		if err := i.advance(ctx, job, leadimport.StageSeed); err != nil {
			return err
		}
	}
	if err := i.seed(ctx, job, src); err != nil {
		return err
	}
	claim := job.Claim
	if err := job.Finish(job.Seed, i.now()); err != nil {
		return err
	}
	if err := i.deps.Jobs.Save(ctx, job, leadimport.Guard{From: []leadimport.Status{leadimport.StatusImporting}, Claim: claim}); err != nil {
		return err
	}
	i.finished(job)
	return nil
}

func (i *Import) advance(ctx context.Context, job *leadimport.Job, stage leadimport.Stage) error {
	job.Stage = stage
	return i.heartbeat(ctx, job, job.Processed)
}

func (i *Import) writeRows(ctx context.Context, p *importPass, src rowSource) error {
	job := p.job
	replay := func(row lead.ImportRow) { p.preparer.Prepare(row) }
	return inBatches(src, job.Processed, replay, func(rows []lead.ImportRow) error {
		items, issues, err := p.prepare(ctx, rows)
		if err != nil {
			return err
		}
		var written *leadimport.Checkpoint
		err = i.deps.Writer.WriteRows(ctx, leadimport.RowBatch{
			Job: job, ActorID: job.RequestedBy, Definitions: p.defs, Items: items, Decide: p.decide,
			Checkpoint: func(outcomes []leadimport.RowOutcome) leadimport.Checkpoint {
				cp := checkpointOf(*job.Result, job.Processed+len(rows), issues, outcomes)
				written = &cp
				return cp
			},
		})
		if err != nil {
			return err
		}
		if written == nil {
			return errors.New("lead import: the writer did not record the batch")
		}
		i.countRows(*job.Result, written.Result)
		at := i.now()
		job.Result, job.Processed, job.HeartbeatAt, job.UpdatedAt = &written.Result, written.Processed, &at, at
		return nil
	})
}

func checkpointOf(before leadimport.Counts, processed int, prepared []lead.ImportIssue, outcomes []leadimport.RowOutcome) leadimport.Checkpoint {
	result := before.Clone()
	issues := slices.Clone(prepared)
	for _, issue := range prepared {
		result.Issue(issue)
	}
	var links []leadimport.PendingLink
	for _, o := range outcomes {
		result.RecordRow(o.Record, o.Decision, o.Matched)
		issues = append(issues, o.Decision.Issues...)
		if o.Record.Relative != nil && o.LeadID != "" && linkable(o.Decision) {
			links = append(links, leadimport.PendingLink{Line: o.Record.Line, LeadID: o.LeadID, RelativeNumber: o.Record.Relative.Number, Kind: o.Record.Relative.Kind})
			result.LinksPlanned++
		}
	}
	slices.SortStableFunc(issues, func(a, b lead.ImportIssue) int { return a.Line - b.Line })
	return leadimport.Checkpoint{Processed: processed, Result: result, Issues: issues, Links: links}
}

func (i *Import) writeLinks(ctx context.Context, job *leadimport.Job) error {
	for rounds := 0; rounds <= job.TotalRows/leadimport.BatchRows+1; rounds++ {
		pending, err := i.deps.Writer.PendingLinks(ctx, job.ID, leadimport.BatchRows)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			return nil
		}
		numbers := make([]string, len(pending))
		for idx, l := range pending {
			numbers[idx] = l.RelativeNumber
		}
		ids, err := i.deps.Lookup.IdentityIDs(ctx, job.WorkspaceID, numbers)
		if err != nil {
			return err
		}
		writes := make([]leadimport.LinkWrite, 0, len(pending))
		for _, l := range pending {
			relativeID, _ := lead.FindByNumber(ids, l.RelativeNumber)
			writes = append(writes, linkWrite(l, relativeID, job.RequestedBy))
		}
		var result *leadimport.Counts
		err = i.deps.Writer.WriteLinks(ctx, leadimport.LinkBatch{Job: job, Links: writes,
			Checkpoint: func(created int, failed []lead.ImportIssue) leadimport.Counts {
				counts := job.Result.Clone()
				counts.LinksCreated += created
				for _, issue := range failed {
					counts.Issue(issue)
				}
				result = &counts
				return counts
			}})
		if err != nil {
			return err
		}
		if result == nil {
			return errors.New("lead import: the writer did not record the family links")
		}
		job.Result = result
	}
	return errors.New("lead import: the family links kept coming back")
}

func linkWrite(l leadimport.PendingLink, relativeID, actorID string) leadimport.LinkWrite {
	fail := func(reason lead.RejectReason) leadimport.LinkWrite {
		return leadimport.LinkWrite{Link: l, Issue: &lead.ImportIssue{Line: l.Line, Reason: reason, Field: lead.ImportFieldRelative}}
	}
	if relativeID == "" {
		return fail(lead.ReasonRelativeNotFound)
	}
	if relativeID == l.LeadID {
		return fail(lead.ReasonRelationSelf)
	}
	rel, err := lead.NewRelation(relativeID, l.LeadID, l.Kind, actorID)
	if err != nil {
		return fail(lead.ReasonRelationKindInvalid)
	}
	return leadimport.LinkWrite{Link: l, Relation: &rel}
}
