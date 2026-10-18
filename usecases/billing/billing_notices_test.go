package billing_usecase

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	billing "vozko/domain/billing"
	"vozko/domain/invoice"
	"vozko/domain/notification"
	"vozko/domain/workspace"
	workspace_addon "vozko/domain/workspace/workspace_addon"
	"vozko/usecases/notification/notificationtest"
)

type addonNames map[string]string

func (a addonNames) GetByKey(key string) (*workspace_addon.AddonDefinition, error) {
	name, ok := a[key]
	if !ok {
		return nil, workspace_addon.ErrAddonNotFound
	}
	return &workspace_addon.AddonDefinition{Key: key, Name: name}, nil
}

type brokenAddonNames struct{}

func (brokenAddonNames) GetByKey(string) (*workspace_addon.AddonDefinition, error) {
	return nil, errors.New("db down")
}

func issuedInvoice() *invoice.Invoice {
	due := time.Date(2026, 11, 23, 0, 0, 0, 0, billing.LocationBRT())
	payURL := "https://pay.example.com/i/abc"
	return &invoice.Invoice{
		ID: "inv-9", WorkspaceID: "ws-1", Purpose: invoice.PurposeMonthlyBilling, AmountBRL: 497.9, DueDate: &due, InvoiceUrl: &payURL,
		LineItems: []invoice.InvoiceLineItem{
			{Kind: invoice.LineItemPlan, Label: "Plano Growth", AmountBRL: 397, Creditable: true},
			{Kind: invoice.LineItemChannel, Label: "whatsapp_number", AmountBRL: 100.9, Quantity: 2},
		},
	}
}

func noticesFor(t *testing.T, publisher *notificationtest.Publisher) *Notices {
	t.Helper()
	notices, err := NewNotices(publisher, addonNames{"whatsapp_number": "Número de WhatsApp"}, "https://app.vozko.test/")
	if err != nil {
		t.Fatal(err)
	}
	return notices
}

func TestTheIssuedInvoiceTellsTheBillingTeamWhatToPayAndWhen(t *testing.T) {
	publisher := &notificationtest.Publisher{}

	if err := noticesFor(t, publisher).InvoiceIssued(issuedInvoice()); err != nil {
		t.Fatal(err)
	}

	d := publisher.Only(t)
	if d.WorkspaceID != "ws-1" || d.Category != notification.CategoryBilling || d.Severity != notification.SeverityInfo || !d.Email {
		t.Fatalf("draft = %+v", d)
	}
	if !d.Audience.Equal(notification.Holding(workspace.ResourceBalance, workspace.ActionRead)) {
		t.Fatalf("audience = %+v, want the people who see the invoices", d.Audience)
	}
	if !strings.Contains(d.Body, "R$ 497,90") || !strings.Contains(d.Body, "23/11/2026") || d.ActionURL != "/dashboard/invoices" ||
		d.DedupKey != "monthly_invoice:inv-9" || d.Source != notification.SourceMonthlyInvoice {
		t.Fatalf("body %q link %q dedup %q source %q", d.Body, d.ActionURL, d.DedupKey, d.Source)
	}
	copy := d.EmailCopy
	if copy == nil || copy.Template != "monthly_invoice_issued.html" || copy.Placeholders["Amount"] != "R$ 497,90" || copy.Placeholders["DueDate"] != "23/11/2026" ||
		copy.Placeholders["PayURL"] != "https://pay.example.com/i/abc" || copy.Placeholders["DashboardURL"] != "https://app.vozko.test/dashboard/invoices" {
		t.Fatalf("email = %+v", copy)
	}
	want := []InvoiceNoticeLine{{Label: "Plano Growth", Amount: "R$ 397,00"}, {Label: "2 x Número de WhatsApp", Amount: "R$ 100,90"}}
	if !reflect.DeepEqual(copy.Placeholders["Lines"], want) {
		t.Fatalf("lines = %+v", copy.Placeholders["Lines"])
	}
}

func TestWithoutAPaymentLinkTheEmailOpensTheInvoicesPage(t *testing.T) {
	publisher := &notificationtest.Publisher{}
	inv := issuedInvoice()
	inv.InvoiceUrl = nil

	if err := noticesFor(t, publisher).InvoiceIssued(inv); err != nil {
		t.Fatal(err)
	}

	if got := publisher.Only(t).EmailCopy.Placeholders["PayURL"]; got != "https://app.vozko.test/dashboard/invoices" {
		t.Fatalf("pay url = %v", got)
	}
}

func TestAnAddonWhoseNameCannotBeReadHoldsTheNoticeForTheNextRun(t *testing.T) {
	publisher := &notificationtest.Publisher{}
	notices, err := NewNotices(publisher, brokenAddonNames{}, "https://app.vozko.test")
	if err != nil {
		t.Fatal(err)
	}

	if err := notices.InvoiceIssued(issuedInvoice()); err == nil || len(publisher.Drafts()) != 0 {
		t.Fatalf("err = %v, drafts = %d", err, len(publisher.Drafts()))
	}
}

func TestAnUnpaidInvoiceNearTheCutoffWarnsWhatStops(t *testing.T) {
	publisher := &notificationtest.Publisher{}
	cutoff := billing.CancelCutoff(*issuedInvoice().DueDate, billing.LocationBRT(), billing.DefaultCancelDeadlineDay)

	if err := noticesFor(t, publisher).CutoffNear(issuedInvoice(), cutoff); err != nil {
		t.Fatal(err)
	}

	d := publisher.Only(t)
	if d.Severity != notification.SeverityWarning || !strings.Contains(d.Title, "27/11/2026") || !strings.Contains(d.Body, "números de WhatsApp") ||
		d.DedupKey != "billing_cutoff_warning:inv-9" || d.ActionURL != "/dashboard/invoices" || d.Source != notification.SourceBillingCutoff {
		t.Fatalf("draft = %+v", d)
	}
	copy := d.EmailCopy
	if copy == nil || copy.Template != "billing_cutoff_warning.html" || copy.Placeholders["CutoffDate"] != "27/11/2026" ||
		copy.Placeholders["Amount"] != "R$ 497,90" || copy.Placeholders["PayURL"] != "https://pay.example.com/i/abc" {
		t.Fatalf("email = %+v", copy)
	}
}

func TestCuttingAccessTellsTheWorkspaceHowToComeBack(t *testing.T) {
	publisher := &notificationtest.Publisher{}

	if err := noticesFor(t, publisher).AccessCut(issuedInvoice()); err != nil {
		t.Fatal(err)
	}

	d := publisher.Only(t)
	if d.Severity != notification.SeverityCritical || d.ActionURL != "/dashboard/plans" || d.DedupKey != "billing_access_cut:inv-9" || d.Source != notification.SourceBillingCutoff {
		t.Fatalf("draft = %+v", d)
	}
	copy := d.EmailCopy
	if copy == nil || copy.Template != "billing_access_cut.html" || copy.Placeholders["PlansURL"] != "https://app.vozko.test/dashboard/plans" || copy.Placeholders["Amount"] != "R$ 497,90" {
		t.Fatalf("email = %+v", copy)
	}
}

func TestNoticesNeedAPublisherAndTheAddonNames(t *testing.T) {
	if _, err := NewNotices(nil, addonNames{}, "https://app.vozko.test"); err == nil {
		t.Fatal("notices without a publisher must not be built")
	}
	if _, err := NewNotices(&notificationtest.Publisher{}, nil, "https://app.vozko.test"); err == nil {
		t.Fatal("notices without the add-on names must not be built")
	}
}
