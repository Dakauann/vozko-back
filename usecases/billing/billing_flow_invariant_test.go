package billing_usecase

import (
	"math"
	"sort"
	"testing"
	"time"

	"vozko/domain/invoice"
	"vozko/domain/workspace"
	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type memInvoiceStore struct {
	invoice.Repository
	byID map[string]*invoice.Invoice
}

func newMemInvoiceStore() *memInvoiceStore {
	return &memInvoiceStore{byID: map[string]*invoice.Invoice{}}
}

func (s *memInvoiceStore) Create(inv *invoice.Invoice) error { s.byID[inv.ID] = inv; return nil }

func (s *memInvoiceStore) GetByIdempotencyKey(key string) (*invoice.Invoice, error) {
	for _, inv := range s.byID {
		if inv.IdempotencyKey == key {
			return inv, nil
		}
	}
	return nil, nil
}

func (s *memInvoiceStore) MarkPaid(id string, _ int64) (bool, error) {
	inv := s.byID[id]
	if inv == nil || inv.Status == invoice.StatusPaid {
		return false, nil
	}
	inv.Status = invoice.StatusPaid
	return true, nil
}

func (s *memInvoiceStore) UpdateStatus(id string, status invoice.Status) error {
	if inv := s.byID[id]; inv != nil {
		inv.Status = status
	}
	return nil
}

func (s *memInvoiceStore) ListUnpaidByPurpose(p invoice.Purpose, afterID string, limit int) ([]invoice.Invoice, error) {
	var out []invoice.Invoice
	for _, inv := range s.byID {
		if inv.Purpose.Normalize() != p.Normalize() {
			continue
		}
		if inv.Status != invoice.StatusPending && inv.Status != invoice.StatusOverdue {
			continue
		}
		if inv.ID > afterID {
			out = append(out, *inv)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type storeBackedCreateInvoice struct{ store *memInvoiceStore }

func (c *storeBackedCreateInvoice) Execute(in invoice.CreateInvoiceInput) (*invoice.CreateInvoiceOutput, error) {
	if existing, _ := c.store.GetByIdempotencyKey(in.IdempotencyKey); existing != nil {
		return &invoice.CreateInvoiceOutput{Invoice: existing}, nil
	}
	inv := &invoice.Invoice{
		ID:             "inv-" + in.IdempotencyKey,
		WorkspaceID:    in.WorkspaceID,
		Purpose:        in.Purpose,
		Status:         invoice.StatusPending,
		IdempotencyKey: in.IdempotencyKey,
		AmountBRL:      in.AmountBRL,
		AmountUSD:      int64(math.Round(in.AmountBRL / 6 * 1_000_000)),
		CreditableUSD:  int64(math.Round(in.CreditableBRL / 6 * 1_000_000)),
	}
	_ = c.store.Create(inv)
	return &invoice.CreateInvoiceOutput{Invoice: inv}, nil
}

func channelAddonFor(ws string) *workspace_addon.AddonSubscription {
	a := channelAddon(25_000_000, 1)
	a.ID = "addon-" + ws
	a.WorkspaceID = ws
	return a
}

func TestBillingFlow_InvariantNoUncollectedChannelBilled(t *testing.T) {
	for _, year := range []int{2026, 2024} {
		t.Run(time.Month(2).String()+"-"+itoa(year), func(t *testing.T) {
			workspaces := []string{"ws-1", "ws-2", "ws-3"}
			paid := map[string]bool{"ws-1": true, "ws-2": true}

			store := newMemInvoiceStore()
			addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{}}
			wsMap := map[string]*workspace.Workspace{}
			var subList []*workspace_plan.WorkspaceSubscription
			for _, ws := range workspaces {
				subList = append(subList, activeSub(ws, "plan-1"))
				addons.byWS[ws] = []*workspace_addon.AddonSubscription{channelAddonFor(ws)}
				wsMap[ws] = owner(ws, "user-"+ws)
			}
			plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-1": {ID: "plan-1", BasePriceBRLCents: 50_000}}}

			emit := NewEmitMonthlyInvoicesUseCase(&fakeSubs{subs: subList}, plans, addons, &fakeWorkspaces{byWS: wsMap}, &fakePricing{rate: 6.0}, &storeBackedCreateInvoice{store: store})
			emit.now = func() time.Time { return time.Date(year, time.February, 18, 12, 0, 0, 0, time.UTC) }
			if n, err := emit.Execute(); err != nil || n != 3 {
				t.Fatalf("emit: n=%d err=%v, want 3 invoices", n, err)
			}

			for _, inv := range store.byID {
				if paid[inv.WorkspaceID] {
					if _, err := store.MarkPaid(inv.ID, inv.AmountUSD); err != nil {
						t.Fatalf("mark paid: %v", err)
					}
				}
			}

			onReduced := &fakeEntitlementHandler{}
			sweep := NewCancelSweepUseCase(store, &fakeSubs{}, addons, onReduced, &fakeAlerter{})
			sweep.now = func() time.Time { return time.Date(year, time.February, 27, 12, 0, 0, 0, time.UTC) }
			if _, err := sweep.Execute(); err != nil {
				t.Fatalf("sweep: %v", err)
			}

			for _, inv := range store.byID {
				if inv.Status == invoice.StatusPending || inv.Status == invoice.StatusOverdue {
					t.Fatalf("workspace %s left with an unresolved invoice (%s) after the sweep", inv.WorkspaceID, inv.Status)
				}
			}

			cancelled := map[string]bool{}
			for _, ws := range onReduced.reduced {
				cancelled[ws] = true
			}
			for _, ws := range workspaces {
				if paid[ws] && cancelled[ws] {
					t.Fatalf("PAID workspace %s must NOT be cancelled", ws)
				}
				if !paid[ws] && !cancelled[ws] {
					t.Fatalf("UNPAID workspace %s must be cancelled (else the vendor bills an uncollected channel = LEAK)", ws)
				}
			}

			for _, ws := range workspaces {
				if cancelled[ws] {
					continue
				}
				if invForWorkspace(store, ws).Status != invoice.StatusPaid {
					t.Fatalf("active channel for %s is not covered by a PAID invoice: LEAK", ws)
				}
			}

			for _, a := range addons.updated {
				if paid[a.WorkspaceID] {
					t.Fatalf("PAID workspace %s should not have a lapsed addon", a.WorkspaceID)
				}
			}
		})
	}
}

func invForWorkspace(s *memInvoiceStore, ws string) *invoice.Invoice {
	for _, inv := range s.byID {
		if inv.WorkspaceID == ws {
			return inv
		}
	}
	return &invoice.Invoice{}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
