package advertising

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	ads "vozko/domain/advertising"
)

func readOnlyWorld() *world {
	w := newWorld()
	seedStructure(w)
	adSetWithBudget(w)
	w.accounts.byID["acc-1"].Tasks = []string{"ANALYZE"}
	return w
}

func TestEveryWriteToAReadOnlyAccountIsRefusedBeforeMeta(t *testing.T) {
	ctx := context.Background()
	creative := imageAd()
	name := "Novo nome"
	cap := int64(900_000)
	writes := map[string]func(w *world) error{
		"publish": func(w *world) error { _, err := publish(t, w, publishableDraft()); return err },
		"preflight": func(w *world) error {
			_, err := w.publisher().Preflight(ctx, "ws-1", publishableDraft())
			return err
		},
		"pause":    func(w *world) error { _, err := w.manager().SetStatus(ctx, "ws-1", "c-1", false); return err },
		"activate": func(w *world) error { _, err := w.manager().SetStatus(ctx, "ws-1", "c-1", true); return err },
		"budget":   func(w *world) error { _, err := w.manager().SetBudget(ctx, "ws-1", "s-1", 3000); return err },
		"edit": func(w *world) error {
			_, err := w.manager().Edit(ctx, "ws-1", "s-1", ads.ObjectEdit{Name: &name})
			return err
		},
		"creative": func(w *world) error {
			_, err := w.manager().Edit(ctx, "ws-1", "a-1", ads.ObjectEdit{Creative: &creative})
			return err
		},
		"copy": func(w *world) error { _, err := w.manager().Copy(ctx, "ws-1", "a-1", ads.CopyRequest{}); return err },
		"archive": func(w *world) error {
			_, err := w.manager().Lifecycle(ctx, "ws-1", "a-1", ads.LifecycleArchive)
			return err
		},
		"delete": func(w *world) error {
			_, err := w.manager().Lifecycle(ctx, "ws-1", "a-1", ads.LifecycleDelete)
			return err
		},
		"spend cap": func(w *world) error { _, err := w.manager().SetSpendCap(ctx, "ws-1", "acc-1", &cap); return err },
		"pixel": func(w *world) error {
			_, err := NewAssetsUseCase(w.sync, w.gateway, w.numbers).CreatePixel(ctx, "ws-1", "acc-1", "Site")
			return err
		},
		"customer list": func(w *world) error {
			_, err := audienceUseCase(w, nil, nil).CreateCustomerList(ctx, "ws-1", ads.CustomerListDraft{AdAccountID: "acc-1", Name: "x", Source: ads.SourceCRM})
			return err
		},
		"lookalike": func(w *world) error {
			_, err := audienceUseCase(w, nil, nil).CreateLookalike(ctx, "ws-1", ads.LookalikeDraft{AdAccountID: "acc-1", Name: "Parecidos", OriginAudienceID: "aud-1", Percent: 2})
			return err
		},
		"delete audience": func(w *world) error { return audienceUseCase(w, nil, nil).Delete(ctx, "ws-1", "acc-1", "aud-1") },
		"form": func(w *world) error {
			draft := ads.LeadFormDraft{AdAccountID: "acc-1", PageID: "page-1", Name: "Orçamento", PrivacyURL: "https://x.example.com/p", Questions: []ads.FormQuestion{{Type: ads.QuestionPhone}}, ThankYouTitle: "Obrigado", ThankYouURL: "https://x.example.com", ThankYouButtonText: "Visitar site"}
			_, uc, _, _, _ := formsWorldFrom(w)
			_, err := uc.Create(ctx, "ws-1", draft)
			return err
		},
		"rule": func(w *world) error {
			rule := ads.AutomatedRule{AdAccountID: "acc-1", Name: "Pausar", Entity: ads.RuleAdSet, ObjectIDs: []string{"s-1"},
				Conditions: []ads.RuleCondition{{Metric: ads.MetricSpent, Operator: ads.OperatorGreaterThan, Value: 50}}, Action: ads.RuleAction{Type: ads.RuleActionPause}}
			_, err := NewRulesUseCase(w.sync, w.gateway).Create(ctx, "ws-1", rule)
			return err
		},
		"rule status": func(w *world) error {
			return NewRulesUseCase(w.sync, w.gateway).SetEnabled(ctx, "ws-1", "acc-1", "rule-1", false)
		},
		"rule delete": func(w *world) error { return NewRulesUseCase(w.sync, w.gateway).Delete(ctx, "ws-1", "acc-1", "rule-1") },
		"split test": func(w *world) error {
			test := ads.SplitTest{AdAccountID: "acc-1", Name: "A x B", Level: ads.TestAdSets,
				Cells:   []ads.TestCell{{Name: "A", ObjectIDs: []string{"s-1"}}, {Name: "B", ObjectIDs: []string{"s-2"}}},
				StartAt: testNow.Add(time.Hour), EndAt: testNow.Add(8 * 24 * time.Hour)}
			_, err := NewSplitTestUseCase(w.sync, w.gateway).Create(ctx, "ws-1", test)
			return err
		},
		"dataset": func(w *world) error {
			_, uc, _ := conversionsWorldFrom(w, nil)
			_, err := uc.ConnectDataset(ctx, "ws-1", "phone-1")
			return err
		},
	}
	for name, write := range writes {
		w := readOnlyWorld()
		if err := write(w); !errors.Is(err, ads.ErrAccountReadOnly) {
			t.Fatalf("%s: got %v, want read only", name, err)
		}
		if len(w.gateway.calls) != 0 {
			t.Fatalf("%s: meta called %v", name, w.gateway.calls)
		}
		if len(w.fees.charged) != 0 {
			t.Fatalf("%s: charged", name)
		}
	}
}

func TestReadOnlyAccountStillSyncsReportsAndReads(t *testing.T) {
	ctx := context.Background()
	w := readOnlyWorld()
	w.gateway.accounts[0].Tasks = []string{"ANALYZE"}
	if _, err := w.sync.Sync(ctx, "ws-1", "acc-1"); err != nil {
		t.Fatalf("sync refused: %v", err)
	}
	if _, err := w.manager().Detail(ctx, "ws-1", "s-1"); err != nil {
		t.Fatalf("detail refused: %v", err)
	}
	if _, err := NewRulesUseCase(w.sync, w.gateway).List(ctx, "ws-1", "acc-1"); err != nil {
		t.Fatalf("rules refused: %v", err)
	}
	if _, err := NewAssetsUseCase(w.sync, w.gateway, w.numbers).Pixels(ctx, "ws-1", "acc-1"); err != nil {
		t.Fatalf("pixels refused: %v", err)
	}
	if _, err := NewAssetsUseCase(w.sync, w.gateway, w.numbers).Pages(ctx, "ws-1", "acc-1"); err != nil {
		t.Fatalf("pages refused: %v", err)
	}
}

func TestSyncPicksUpARoleChangeAtMeta(t *testing.T) {
	w := readOnlyWorld()
	w.gateway.accounts[0].Tasks = []string{"ADVERTISE", "ANALYZE"}
	account, err := w.sync.Sync(context.Background(), "ws-1", "acc-1")
	if err != nil {
		t.Fatal(err)
	}
	if account.Role() != ads.RoleAdvertiser || account.CanManage() != nil {
		t.Fatalf("role %s", account.Role())
	}
}

func TestOnlyAnAdminChangesTheSpendCap(t *testing.T) {
	w := newWorld()
	w.accounts.byID["acc-1"].Tasks = []string{"ADVERTISE", "ANALYZE"}
	cap := int64(900_000)
	if _, err := w.manager().SetSpendCap(context.Background(), "ws-1", "acc-1", &cap); !errors.Is(err, ads.ErrAccountAdminRequired) {
		t.Fatalf("err %v", err)
	}
	if len(w.gateway.calls) != 0 {
		t.Fatalf("meta called %v", w.gateway.calls)
	}
}

func TestConnectSubscribesWebhooksOnlyForAccountsTheProfileCanManage(t *testing.T) {
	w := newWorld()
	w.accounts.byID = map[string]*ads.AdAccount{}
	w.gateway.accounts = append(w.gateway.accounts, ads.RemoteAdAccount{MetaAccountID: "222", Name: "Analista", Currency: "BRL", Timezone: "America/Sao_Paulo", Tasks: []string{"ANALYZE"}})
	out, err := connectFlow(t, w, fullDebug())
	if err != nil {
		t.Fatal(err)
	}
	if out.ConnectedCount() != 2 {
		t.Fatalf("accounts %+v", out.Accounts)
	}
	if !slices.Equal(w.gateway.subscribed, []string{"111"}) {
		t.Fatalf("subscribed %v", w.gateway.subscribed)
	}
}
