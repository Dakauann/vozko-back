package savedview_usecase

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/savedview"
)

type grants map[string]bool

func (g grants) HasWorkspacePermission(_, _, resource, action string, _ bool) bool {
	return g[resource+":"+action]
}

type leadFilterCheck struct {
	refuse error
	seen   []crmfilter.Filter
}

func (c *leadFilterCheck) CheckLeadFilter(_ savedview.Actor, f crmfilter.Filter) error {
	c.seen = append(c.seen, f)
	return c.refuse
}

type perReaderCheck map[string]error

func (c perReaderCheck) CheckLeadFilter(a savedview.Actor, _ crmfilter.Filter) error {
	return c[a.UserID]
}

func actor(userID string) savedview.Actor {
	return savedview.Actor{UserID: userID, WorkspaceID: "ws"}
}

func access(perms grants, filters LeadFilters) Access {
	return Access{Permissions: perms, LeadFilters: filters}
}

func leadView(columns ...string) *savedview.SavedView {
	return &savedview.SavedView{
		Name: "Centro", ObjectType: savedview.ObjectLead, Columns: columns,
		Filter: crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
			{Field: crmfilter.FieldDistrict, Operator: crmfilter.OpIn, Values: []string{"sp:sao paulo/centro"}},
		}}}},
	}
}

func TestLeadViewsFollowTheLeadReaders(t *testing.T) {
	leadsReader := grants{"leads:read": true}
	conversationsOnly := grants{"conversations:read": true, "conversations:create": true, "conversations:update": true, "conversations:delete": true}

	if _, err := NewCreateSavedViewUseCase(&memoryViews{views: map[string]*savedview.SavedView{}}, access(conversationsOnly, &leadFilterCheck{})).Execute(actor("owner"), leadView()); !errors.Is(err, savedview.ErrForbidden) {
		t.Fatalf("a conversations member creating a lead view = %v, want ErrForbidden", err)
	}
	if _, err := NewListSavedViewsUseCase(sharedView(), access(conversationsOnly, &leadFilterCheck{})).Execute(actor("owner"), savedview.ObjectLead); !errors.Is(err, savedview.ErrForbidden) {
		t.Fatalf("a conversations member listing lead views = %v, want ErrForbidden", err)
	}
	if _, err := NewListSavedViewsUseCase(sharedView(), access(leadsReader, &leadFilterCheck{})).Execute(actor("owner"), savedview.ObjectLead); err != nil {
		t.Fatalf("a lead reader lists lead views: %v", err)
	}
	if _, err := NewListSavedViewsUseCase(sharedView(), access(leadsReader, &leadFilterCheck{})).Execute(actor("owner"), savedview.ObjectConversation); !errors.Is(err, savedview.ErrForbidden) {
		t.Fatalf("a lead reader listing conversation views = %v, want ErrForbidden", err)
	}
	if err := NewDeleteSavedViewUseCase(sharedView(), access(conversationsOnly, &leadFilterCheck{})).Execute(actor("owner"), "v1"); !errors.Is(err, savedview.ErrForbidden) {
		t.Fatalf("a conversations member deleting a lead view = %v, want ErrForbidden", err)
	}
}

func TestALeadViewIsCheckedForTheSaverAndKeepsItsColumns(t *testing.T) {
	repo := &memoryViews{views: map[string]*savedview.SavedView{}}
	check := &leadFilterCheck{}
	created, err := NewCreateSavedViewUseCase(repo, access(grants{"leads:read": true}, check)).Execute(actor("owner"), leadView("name", "district", "owner"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(check.seen) != 1 {
		t.Fatalf("the filter must be checked for the saver once, got %d", len(check.seen))
	}
	if !reflect.DeepEqual(created.Columns, []string{"name", "district", "owner"}) {
		t.Fatalf("columns = %v, want them saved", created.Columns)
	}

	refused := &leadFilterCheck{refuse: &customfield.FilterError{Key: "classificacao", Err: customfield.ErrFilterSensitive}}
	before := len(repo.views)
	if _, err := NewCreateSavedViewUseCase(repo, access(grants{"leads:read": true}, refused)).Execute(actor("owner"), leadView()); !errors.Is(err, customfield.ErrFilterSensitive) {
		t.Fatalf("a sensitive filter the saver cannot read = %v, want ErrFilterSensitive", err)
	}
	if len(repo.views) != before {
		t.Fatal("a refused view must not be saved")
	}

	patch := leadView("name")
	if _, err := NewUpdateSavedViewUseCase(repo, access(grants{"leads:read": true}, refused)).Execute(actor("owner"), created.ID, patch); !errors.Is(err, customfield.ErrFilterSensitive) {
		t.Fatalf("an update is checked like a create, got %v", err)
	}
}

func TestSavedViewsFailClosedWithoutTheirPorts(t *testing.T) {
	repo := &memoryViews{views: map[string]*savedview.SavedView{}}
	if _, err := NewCreateSavedViewUseCase(repo, access(grants{"leads:read": true}, nil)).Execute(actor("owner"), leadView()); !errors.Is(err, savedview.ErrForbidden) {
		t.Fatalf("a lead view without the filter check = %v, want ErrForbidden", err)
	}
	conversation := &savedview.SavedView{Name: "Abertos", ObjectType: savedview.ObjectConversation}
	if _, err := NewCreateSavedViewUseCase(repo, Access{}).Execute(actor("owner"), conversation); !errors.Is(err, savedview.ErrForbidden) {
		t.Fatalf("without permissions = %v, want ErrForbidden", err)
	}
	if _, err := NewCreateSavedViewUseCase(repo, access(grants{"conversations:create": true}, nil)).Execute(actor("owner"), conversation); err != nil {
		t.Fatalf("a conversation view needs no lead filter check: %v", err)
	}
	if _, err := NewListSavedViewsUseCase(repo, access(grants{"leads:read": true}, nil)).Execute(actor(""), savedview.ObjectLead); !errors.Is(err, savedview.ErrForbidden) {
		t.Fatalf("an anonymous list = %v, want ErrForbidden", err)
	}
}

func classifiedViews() *memoryViews {
	sensitive := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldCustom, Key: "classificacao", Operator: crmfilter.OpEquals, Values: []string{"Opositor"}},
	}}}}
	return &memoryViews{views: map[string]*savedview.SavedView{
		"shared": {ID: "shared", WorkspaceID: "ws", OwnerID: "owner", ObjectType: savedview.ObjectLead, Name: "Opositores",
			GroupBy: savedview.GroupByNone, Visibility: savedview.VisibilityShared, SortDir: savedview.SortDesc, Filter: sensitive},
		"mine": {ID: "mine", WorkspaceID: "ws", OwnerID: "reader", ObjectType: savedview.ObjectLead, Name: "Meus",
			GroupBy: savedview.GroupByNone, Visibility: savedview.VisibilityPrivate, SortDir: savedview.SortDesc, Filter: sensitive},
	}}
}

func idsOf(views []*savedview.SavedView) map[string]bool {
	out := map[string]bool{}
	for _, v := range views {
		out[v.ID] = true
	}
	return out
}

func TestASharedLeadViewNeverShowsASensitiveFilterToAReaderWithoutAccess(t *testing.T) {
	checks := perReaderCheck{"reader": customfield.ErrFilterSensitive}
	list := NewListSavedViewsUseCase(classifiedViews(), access(grants{"leads:read": true}, checks))

	readerViews, err := list.Execute(actor("reader"), savedview.ObjectLead)
	if err != nil {
		t.Fatalf("reader List() error = %v", err)
	}
	if got := idsOf(readerViews); got["shared"] || !got["mine"] {
		t.Fatalf("reader sees %v, want only their own view", got)
	}

	ownerViews, err := list.Execute(actor("owner"), savedview.ObjectLead)
	if err != nil {
		t.Fatalf("owner List() error = %v", err)
	}
	if got := idsOf(ownerViews); !got["shared"] {
		t.Fatalf("owner sees %v, want their shared view", got)
	}
}

func TestListingSharedLeadViewsFailsClosed(t *testing.T) {
	if _, err := NewListSavedViewsUseCase(classifiedViews(), access(grants{"leads:read": true}, nil)).Execute(actor("reader"), savedview.ObjectLead); !errors.Is(err, savedview.ErrForbidden) {
		t.Fatalf("without a filter check = %v, want ErrForbidden", err)
	}
	broken := errors.New("definitions unavailable")
	if _, err := NewListSavedViewsUseCase(classifiedViews(), access(grants{"leads:read": true}, perReaderCheck{"reader": broken})).Execute(actor("reader"), savedview.ObjectLead); !errors.Is(err, broken) {
		t.Fatalf("a failing filter check = %v, want the failure", err)
	}
}
