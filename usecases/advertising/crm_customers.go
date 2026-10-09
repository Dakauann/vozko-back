package advertising

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ads "vozko/domain/advertising"
	"vozko/domain/conversation"
	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/selection"
	"vozko/domain/workspace"
)

const crmCustomerPage = 5000

var errCRMCustomersIncomplete = errors.New("ads: the CRM customer directory is missing a dependency")

type Requester = conversation.Viewer

type CustomerQuery struct {
	Filter     crmfilter.Filter
	SnapshotID string
}

type LeadSelection interface {
	Resolve(ctx context.Context, scope selection.Scope, s selection.Selection, after string, limit int) ([]selection.Ref, error)
	Snapshot(ctx context.Context, workspaceID, snapshotID, after string, limit int) ([]string, error)
}

type LeadsByID interface {
	FindByIDs(workspaceID string, ids []string) ([]*lead.Lead, error)
}

type LeadDefinitions interface {
	ListByObject(workspaceID string, objectType customfield.ObjectType) ([]*customfield.Definition, error)
}

type CRMCustomerDeps struct {
	Selection   LeadSelection
	Leads       LeadsByID
	Contacts    lead.ContactDetails
	Definitions LeadDefinitions
	Permissions workspace.PermissionChecker
}

type crmCustomers struct {
	deps CRMCustomerDeps
	page int
}

func NewCRMCustomers(deps CRMCustomerDeps) (CustomerDirectory, error) {
	if deps.Selection == nil || deps.Leads == nil || deps.Contacts == nil || deps.Definitions == nil || deps.Permissions == nil {
		return nil, errCRMCustomersIncomplete
	}
	return &crmCustomers{deps: deps, page: crmCustomerPage}, nil
}

func (c *crmCustomers) allowed(a Requester, action workspace.Action) bool {
	return workspace.HoldsAll(c.deps.Permissions, a.WorkspaceID, a.UserID, a.IsAdmin,
		[]workspace.PermissionEntry{{Resource: workspace.ResourceLeads, Action: action}})
}

func (c *crmCustomers) Customers(ctx context.Context, a Requester, q CustomerQuery, limit int) ([]ads.Customer, error) {
	if !c.allowed(a, workspace.ActionRead) {
		return nil, lead.ErrLeadForbidden
	}
	defs, err := c.deps.Definitions.ListByObject(a.WorkspaceID, customfield.ObjectLead)
	if err != nil {
		return nil, fmt.Errorf("ads: the lead fields of workspace %s: %w", a.WorkspaceID, err)
	}
	if err := RefuseSensitiveAudience(q.Filter, defs); err != nil {
		return nil, err
	}
	page, err := c.pager(a, q)
	if err != nil {
		return nil, err
	}
	withZip := c.allowed(a, workspace.ActionReadAddresses)
	var out []ads.Customer
	after := ""
	for len(out) < limit {
		ids, err := page(ctx, after)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			break
		}
		customers, err := c.customersOf(ctx, a.WorkspaceID, ids, withZip)
		if err != nil {
			return nil, err
		}
		out = append(out, customers...)
		after = ids[len(ids)-1]
		if len(ids) < c.page {
			break
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (c *crmCustomers) pager(a Requester, q CustomerQuery) (func(ctx context.Context, after string) ([]string, error), error) {
	if snapshotID := strings.TrimSpace(q.SnapshotID); snapshotID != "" {
		return func(ctx context.Context, after string) ([]string, error) {
			return c.deps.Selection.Snapshot(ctx, a.WorkspaceID, snapshotID, after, c.page)
		}, nil
	}
	s, err := AudienceSelection(selection.ForFilter(q.Filter))
	if err != nil {
		return nil, err
	}
	scope := selection.Scope{WorkspaceID: a.WorkspaceID, ActorID: a.UserID, IsAdmin: a.IsAdmin}
	return func(ctx context.Context, after string) ([]string, error) {
		refs, err := c.deps.Selection.Resolve(ctx, scope, s, after, c.page)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(refs))
		for _, ref := range refs {
			ids = append(ids, ref.ID)
		}
		return ids, nil
	}, nil
}

func RefuseSensitiveAudience(filter crmfilter.Filter, defs []*customfield.Definition) error {
	if _, err := customfield.BindFilter(filter, defs, customfield.Viewer{}); errors.Is(err, customfield.ErrFilterSensitive) {
		return fmt.Errorf("%w: %w", ads.ErrSensitiveAudience, err)
	}
	return nil
}

func (c *crmCustomers) customersOf(ctx context.Context, workspaceID string, ids []string, withZip bool) ([]ads.Customer, error) {
	leads, err := c.deps.Leads.FindByIDs(workspaceID, ids)
	if err != nil {
		return nil, err
	}
	if err := c.deps.Contacts.AttachContactDetails(ctx, workspaceID, leads); err != nil {
		return nil, err
	}
	byID := make(map[string]*lead.Lead, len(leads))
	for _, l := range leads {
		if l != nil {
			byID[l.ID] = l
		}
	}
	out := make([]ads.Customer, 0, len(ids))
	for _, id := range ids {
		if l, ok := byID[id]; ok {
			out = append(out, customerOf(l, withZip))
		}
	}
	return out, nil
}

func customerOf(l *lead.Lead, withZip bool) ads.Customer {
	first, last := l.SplitName()
	phone := l.Number
	if phone == "" && len(l.Phones) > 0 {
		phone = l.Phones[0].Number
	}
	customer := ads.Customer{ads.MatchPhone: phone, ads.MatchFirstName: first, ads.MatchLastName: last}
	if primary := l.PrimaryAddress(); primary != nil {
		customer[ads.MatchCity] = primary.Postal.City
		customer[ads.MatchState] = primary.Postal.State
		if withZip {
			customer[ads.MatchZip] = primary.Postal.ZipCode
		}
	}
	return customer
}

func audienceRequire() *crmfilter.Filter {
	return &crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsFalse},
	}}}}
}

func AudienceSelection(s selection.Selection) (selection.Selection, error) {
	switch s.Mode {
	case selection.ModeIDs, selection.ModeAllMatching, selection.ModeEveryone:
	default:
		return selection.Selection{}, fmt.Errorf("%w: a Meta audience takes picked leads, a filter or the whole base", selection.ErrModeUnsupported)
	}
	s.Require = audienceRequire()
	return s, nil
}

func AudienceFilter(s selection.Selection) (crmfilter.Filter, error) {
	if _, err := AudienceSelection(s); err != nil {
		return crmfilter.Filter{}, err
	}
	if s.Mode == selection.ModeIDs {
		return crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
			{Field: crmfilter.FieldID, Operator: crmfilter.OpIn, Values: append([]string{}, s.IDs...)},
		}}}}, nil
	}
	groups := append([]crmfilter.Group{}, s.EffectiveFilter().Groups...)
	if len(s.ExcludeIDs) > 0 {
		groups = append(groups, crmfilter.Group{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
			{Field: crmfilter.FieldID, Operator: crmfilter.OpNotIn, Values: append([]string{}, s.ExcludeIDs...)},
		}})
	}
	return crmfilter.Filter{Groups: groups}, nil
}

type libraryFiles struct {
	media   mediaReader
	fetcher fileFetcher
}

func NewLibraryFiles(media mediaReader, fetcher fileFetcher) RawFiles {
	return &libraryFiles{media: media, fetcher: fetcher}
}

func (f *libraryFiles) Bytes(ctx context.Context, workspaceID, mediaID string) ([]byte, error) {
	m, err := f.media.GetMedia(workspaceID, strings.TrimSpace(mediaID))
	if err != nil {
		return nil, err
	}
	if m == nil || strings.TrimSpace(m.URL) == "" {
		return nil, ErrCustomerFileUnreadable
	}
	return f.fetcher.Fetch(ctx, m.URL)
}
