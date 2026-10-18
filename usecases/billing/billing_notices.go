package billing_usecase

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"vozko/brand"
	billing "vozko/domain/billing"
	"vozko/domain/invoice"
	"vozko/domain/notification"
	"vozko/domain/shared"
	"vozko/domain/workspace"
	workspace_addon "vozko/domain/workspace/workspace_addon"
)

const (
	invoicesPath   = "/dashboard/invoices"
	billingPlans   = "/dashboard/plans"
	noticeDateForm = "02/01/2006"
)

var ErrNoticesUnavailable = errors.New("billing: the billing notices need a publisher and the add-on names")

type BillingNotices interface {
	InvoiceIssued(inv *invoice.Invoice) error
	CutoffNear(inv *invoice.Invoice, cutoff time.Time) error
	AccessCut(inv *invoice.Invoice) error
}

type AddonNames interface {
	GetByKey(key string) (*workspace_addon.AddonDefinition, error)
}

type InvoiceNoticeLine struct {
	Label  string
	Amount string
}

type Notices struct {
	publisher    notification.Publisher
	addons       AddonNames
	dashboardURL string
}

var _ BillingNotices = (*Notices)(nil)

func NewNotices(publisher notification.Publisher, addons AddonNames, dashboardURL string) (*Notices, error) {
	if publisher == nil || addons == nil {
		return nil, ErrNoticesUnavailable
	}
	return &Notices{publisher: publisher, addons: addons, dashboardURL: strings.TrimRight(dashboardURL, "/")}, nil
}

func (n *Notices) InvoiceIssued(inv *invoice.Invoice) error {
	lines, err := n.lines(inv)
	if err != nil {
		return err
	}
	amount, due := shared.FormatBRL(inv.AmountBRL), dueDateOf(inv)
	return n.publish(notification.Draft{
		WorkspaceID: inv.WorkspaceID,
		Category:    notification.CategoryBilling,
		Severity:    notification.SeverityInfo,
		Title:       "Sua fatura do mês está disponível",
		Body:        "A fatura de " + amount + " do plano e dos complementos vence em " + due + ". O pagamento é por PIX.",
		ActionURL:   invoicesPath,
		ActionLabel: "Ver fatura",
		Audience:    notification.Holding(workspace.ResourceBalance, workspace.ActionRead),
		Email:       true,
		EmailCopy: &notification.EmailCopy{
			Template: "monthly_invoice_issued.html",
			Subject:  "Sua fatura do mês - " + brand.Active().Name,
			Placeholders: map[string]interface{}{
				"Amount":       amount,
				"DueDate":      due,
				"Lines":        lines,
				"PayURL":       n.payURL(inv),
				"DashboardURL": notification.AbsoluteLink(n.dashboardURL, invoicesPath),
			},
		},
		DedupKey: "monthly_invoice:" + inv.ID,
		Source:   notification.SourceMonthlyInvoice,
	})
}

func (n *Notices) CutoffNear(inv *invoice.Invoice, cutoff time.Time) error {
	amount, due := shared.FormatBRL(inv.AmountBRL), dueDateOf(inv)
	deadline := cutoff.In(billing.LocationBRT()).Format(noticeDateForm)
	return n.publish(notification.Draft{
		WorkspaceID: inv.WorkspaceID,
		Category:    notification.CategoryBilling,
		Severity:    notification.SeverityWarning,
		Title:       "Pague a fatura até " + deadline + " para manter o acesso",
		Body:        "A fatura de " + amount + " venceu em " + due + ". Se não for paga até " + deadline + ", o plano e os complementos são encerrados e os números de WhatsApp incluídos são desconectados.",
		ActionURL:   invoicesPath,
		ActionLabel: "Pagar fatura",
		Audience:    notification.Holding(workspace.ResourceBalance, workspace.ActionRead),
		Email:       true,
		EmailCopy: &notification.EmailCopy{
			Template: "billing_cutoff_warning.html",
			Subject:  "Pague a fatura até " + deadline + " - " + brand.Active().Name,
			Placeholders: map[string]interface{}{
				"Amount":     amount,
				"DueDate":    due,
				"CutoffDate": deadline,
				"PayURL":     n.payURL(inv),
			},
		},
		DedupKey: "billing_cutoff_warning:" + inv.ID,
		Source:   notification.SourceBillingCutoff,
	})
}

func (n *Notices) AccessCut(inv *invoice.Invoice) error {
	amount, due := shared.FormatBRL(inv.AmountBRL), dueDateOf(inv)
	return n.publish(notification.Draft{
		WorkspaceID: inv.WorkspaceID,
		Category:    notification.CategoryBilling,
		Severity:    notification.SeverityCritical,
		Title:       "Plano e complementos encerrados por falta de pagamento",
		Body:        "A fatura de " + amount + ", vencida em " + due + ", não foi paga até o prazo. O plano, os complementos e os números de WhatsApp incluídos foram encerrados. Contrate um plano para voltar a usar.",
		ActionURL:   billingPlans,
		ActionLabel: "Contratar plano",
		Audience:    notification.Holding(workspace.ResourceBalance, workspace.ActionRead),
		Email:       true,
		EmailCopy: &notification.EmailCopy{
			Template: "billing_access_cut.html",
			Subject:  "Plano e complementos encerrados - " + brand.Active().Name,
			Placeholders: map[string]interface{}{
				"Amount":   amount,
				"DueDate":  due,
				"PlansURL": notification.AbsoluteLink(n.dashboardURL, billingPlans),
			},
		},
		DedupKey: "billing_access_cut:" + inv.ID,
		Source:   notification.SourceBillingCutoff,
	})
}

func (n *Notices) publish(d notification.Draft) error {
	_, err := n.publisher.Publish(context.Background(), d)
	return err
}

func (n *Notices) payURL(inv *invoice.Invoice) string {
	if inv.InvoiceUrl != nil && strings.TrimSpace(*inv.InvoiceUrl) != "" {
		return strings.TrimSpace(*inv.InvoiceUrl)
	}
	return notification.AbsoluteLink(n.dashboardURL, invoicesPath)
}

func (n *Notices) lines(inv *invoice.Invoice) ([]InvoiceNoticeLine, error) {
	out := make([]InvoiceNoticeLine, 0, len(inv.LineItems))
	for _, item := range inv.LineItems {
		label, err := n.lineLabel(item)
		if err != nil {
			return nil, err
		}
		out = append(out, InvoiceNoticeLine{Label: label, Amount: shared.FormatBRL(item.AmountBRL)})
	}
	return out, nil
}

func (n *Notices) lineLabel(item invoice.InvoiceLineItem) (string, error) {
	if item.Kind != invoice.LineItemChannel {
		return item.Label, nil
	}
	label := item.Label
	def, err := n.addons.GetByKey(item.Label)
	switch {
	case errors.Is(err, workspace_addon.ErrAddonNotFound):
	case err != nil:
		return "", fmt.Errorf("name of add-on %s: %w", item.Label, err)
	case def != nil && strings.TrimSpace(def.Name) != "":
		label = def.Name
	}
	if item.Quantity > 1 {
		label = strconv.Itoa(item.Quantity) + " x " + label
	}
	return label, nil
}

func dueDateOf(inv *invoice.Invoice) string {
	if inv.DueDate == nil {
		return ""
	}
	return inv.DueDate.In(billing.LocationBRT()).Format(noticeDateForm)
}
