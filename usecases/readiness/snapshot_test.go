package readiness_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/readiness"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	tmpl "vozko/domain/whatsapp/template"
	"vozko/domain/workspace"
	"vozko/domain/workspace/workspace_addon"
	"vozko/domain/workspace/workspace_plan"
	uw "vozko/domain/unofficial_whatsapp"
)

var owner = readiness.Person{WorkspaceID: "ws1", UserID: "u1"}

type accessRules map[workspace.Resource]bool

func (a accessRules) Execute(_ string, _ string, resource workspace.Resource, _ workspace.Action) error {
	if a[resource] {
		return nil
	}
	return workspace.ErrInsufficientPermissions
}

type subscription struct{ err error }

func (s subscription) Execute(string) (*workspace_plan.WorkspaceSubscription, error) {
	return &workspace_plan.WorkspaceSubscription{}, s.err
}

type balanceOf int64

func (b balanceOf) GetBalance(string) (int64, error) { return int64(b), nil }

type phones struct{ count int }

func (p phones) List(string, businessphone.ListInput) (*shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber], error) {
	return &shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber]{TotalItems: int64(p.count)}, nil
}

type entitlements struct{ phones int }

func (e entitlements) Execute(string) ([]workspace_addon.WorkspaceEntitlement, error) {
	return []workspace_addon.WorkspaceEntitlement{{Kind: workspace_addon.EntitlementWhatsAppBusinessPhones, Total: e.phones}}, nil
}

type gate struct {
	ok  bool
	err error
}

func (g gate) CanProvisionPhone(string) (bool, error) { return g.ok, g.err }

type templates struct{ approved int }

func (t templates) List(_ string, in tmpl.ListInput) (*shared.PaginatedResult[*tmpl.Template], error) {
	if in.Status != tmpl.TemplateStatusApproved {
		return nil, errors.New("must count approved templates only")
	}
	return &shared.PaginatedResult[*tmpl.Template]{TotalItems: int64(t.approved)}, nil
}
func (templates) Get(string, string) (*tmpl.Template, error) { return nil, nil }
func (templates) Create(string, string, tmpl.CreateTemplateInput) (*tmpl.CreateTemplateOutput, error) {
	return nil, nil
}

type allowance uw.InstanceAllowance

func (a allowance) AllowanceFor(context.Context, string) (uw.InstanceAllowance, error) {
	return uw.InstanceAllowance(a), nil
}

func snapshotWith(access accessRules, phoneCount, phoneQuota int, canProvision bool, approved int) readiness.SnapshotUseCase {
	return NewSnapshotUseCase(SnapshotDeps{
		Subscription: subscription{},
		Balance:      balanceOf(2_500_000),
		Probes: []readiness.Probe{
			NewOfficialWhatsAppProbe(OfficialWhatsAppDeps{Phones: phones{phoneCount}, Entitlements: entitlements{phoneQuota}, Gate: gate{ok: canProvision}, Access: access}),
			NewTemplatesProbe(TemplatesDeps{Templates: templates{approved}, Phones: phones{phoneCount}, Access: access}),
			NewUnofficialWhatsAppProbe(allowance{Limit: 2, Used: 2}, access),
			NewCountProbe(readiness.KnowledgeBases, workspace.ResourceKnowledgeBases, func(context.Context, string) (int, error) { return 1, nil }, 5, access),
			NewCountProbe(readiness.Instagram, workspace.ResourceInstagramAccounts, func(context.Context, string) (int, error) { return 0, errors.New("db down") }, 0, access),
		},
	})
}

func status(t *testing.T, snap *readiness.Snapshot, c readiness.Capability) readiness.Status {
	t.Helper()
	s, ok := snap.Get(c)
	if !ok {
		t.Fatalf("%s missing from %+v", c, snap.Capabilities)
	}
	return s
}

func TestSnapshotTellsAWorkspaceWithoutANumberWhatItNeeds(t *testing.T) {
	all := accessRules{workspace.ResourceBusinessPhones: true, workspace.ResourceWhatsAppTemplates: true, workspace.ResourceKnowledgeBases: true, workspace.ResourceUnofficialWhatsAppInstances: true}
	snap, err := snapshotWith(all, 0, 1, true, 0).Snapshot(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.SubscriptionActive || snap.BalanceMicros != 2_500_000 {
		t.Fatalf("snapshot = %+v", snap)
	}
	official := status(t, snap, readiness.OfficialWhatsApp)
	if official.Count != 0 || !official.CanAdd || official.Usage == nil || official.Usage.Total != 1 {
		t.Fatalf("official = %+v", official)
	}
	if tpl := status(t, snap, readiness.ApprovedTemplates); tpl.CanAdd || tpl.Blocker != readiness.BlockerNeedsOfficial {
		t.Fatalf("templates without a number = %+v", tpl)
	}
	if uw := status(t, snap, readiness.UnofficialWhatsApp); uw.CanAdd || uw.Blocker != readiness.BlockerAtLimit || uw.Usage.Used != 2 {
		t.Fatalf("unofficial at limit = %+v", uw)
	}
	if kb := status(t, snap, readiness.KnowledgeBases); kb.Count != 1 || !kb.CanAdd || kb.Usage.Total != 5 {
		t.Fatalf("knowledge = %+v", kb)
	}
	if ig := status(t, snap, readiness.Instagram); ig.CanAdd || ig.Blocker != readiness.BlockerUnavailable {
		t.Fatalf("a failing probe must never offer an action: %+v", ig)
	}
}

func TestSnapshotRespectsQuotaAndPermission(t *testing.T) {
	full, err := snapshotWith(accessRules{workspace.ResourceBusinessPhones: true}, 1, 1, false, 3).Snapshot(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if s := status(t, full, readiness.OfficialWhatsApp); s.CanAdd || s.Blocker != readiness.BlockerAtLimit || s.Usage.Used != 1 {
		t.Fatalf("at quota = %+v", s)
	}
	denied, _ := snapshotWith(accessRules{}, 0, 1, true, 0).Snapshot(context.Background(), owner)
	if s := status(t, denied, readiness.OfficialWhatsApp); s.CanAdd || s.Blocker != readiness.BlockerNoPermission {
		t.Fatalf("no permission = %+v", s)
	}
	admin := owner
	admin.SystemAdmin = true
	asAdmin, _ := snapshotWith(accessRules{}, 0, 1, true, 0).Snapshot(context.Background(), admin)
	if s := status(t, asAdmin, readiness.OfficialWhatsApp); !s.CanAdd {
		t.Fatalf("system admin = %+v", s)
	}
}

func TestSnapshotWithoutSubscription(t *testing.T) {
	uc := NewSnapshotUseCase(SnapshotDeps{Subscription: subscription{err: errors.New("expired")}, Balance: balanceOf(0)})
	snap, err := uc.Snapshot(context.Background(), owner)
	if err != nil || snap.SubscriptionActive {
		t.Fatalf("snapshot %+v err %v", snap, err)
	}
	if _, err := uc.Snapshot(context.Background(), readiness.Person{}); err == nil {
		t.Fatal("a snapshot without a workspace was built")
	}
}
