package analytics

import (
	"time"

	wsc "vozko/domain/workspace_config"
)

const microsPerUnit = 1_000_000

type CostRates struct {
	USDToBRLMicros int64 `json:"usdToBrlMicros"`
}

type Money struct {
	USDMicros int64  `json:"usdMicros"`
	BRLMicros *int64 `json:"brlMicros"`
}

func (r CostRates) Money(usdMicros int64) Money {
	money := Money{USDMicros: usdMicros}
	if r.USDToBRLMicros > 0 {
		brl := usdMicros * r.USDToBRLMicros / microsPerUnit
		money.BRLMicros = &brl
	}
	return money
}

type MetaAnswers struct {
	Charged  int64 `json:"charged"`
	Free     int64 `json:"free"`
	NoAnswer int64 `json:"noAnswer"`
}

func (t MetaServiceMessageCostTotals) SplitAnswers() MetaAnswers {
	return MetaAnswers{
		Charged:  t.MetaConfirmed,
		Free:     max(t.MetaAnswered-t.MetaConfirmed, 0),
		NoAnswer: max(t.ServiceMessages-t.MetaAnswered, 0),
	}
}

type ServiceCharge struct {
	WorkspaceID string `json:"w"`
	MetaPayer   string `json:"p"`
	Charged     int64  `json:"c"`
}

type ServiceMessageCosts map[string]int64

func (c ServiceMessageCosts) Cost(workspaceID string, charged int64) (int64, bool) {
	if charged <= 0 {
		return 0, true
	}
	rate := c[workspaceID]
	if rate <= 0 {
		return 0, false
	}
	return charged * rate, true
}

func (r CostRates) optionalMoney(usdMicros int64, known bool) *Money {
	if !known {
		return nil
	}
	money := r.Money(usdMicros)
	return &money
}

type WorkspaceEconomics struct {
	MetaPayer           string `json:"metaPayer"`
	PaidByClient        Money  `json:"paidByClient"`
	TemplateCost        Money  `json:"templateCost"`
	ServiceCost         *Money `json:"serviceCost"`
	VozkoMetaCost       *Money `json:"vozkoMetaCost"`
	RealMargin          *Money `json:"realMargin"`
	ServiceExceedsPrice bool   `json:"serviceExceedsPrice"`
}

func NewWorkspaceEconomics(payer wsc.MetaPayer, paidMicros, templateCostMicros int64, serviceCostMicros *int64, rates CostRates) WorkspaceEconomics {
	serviceKnown := serviceCostMicros != nil
	serviceCost := int64(0)
	if serviceKnown {
		serviceCost = *serviceCostMicros
	}
	e := WorkspaceEconomics{
		PaidByClient: rates.Money(paidMicros),
		TemplateCost: rates.Money(templateCostMicros),
		ServiceCost:  rates.optionalMoney(serviceCost, serviceKnown),
	}
	if payer == wsc.MetaPayerClient {
		e.MetaPayer = string(wsc.MetaPayerClient)
		e.VozkoMetaCost = rates.optionalMoney(0, true)
		e.RealMargin = rates.optionalMoney(paidMicros, true)
		return e
	}
	vozkoCost := templateCostMicros + serviceCost
	e.MetaPayer = string(wsc.MetaPayerVozko)
	e.VozkoMetaCost = rates.optionalMoney(vozkoCost, serviceKnown)
	e.RealMargin = rates.optionalMoney(paidMicros-vozkoCost, serviceKnown)
	e.ServiceExceedsPrice = serviceKnown && serviceCost > 0 && paidMicros-vozkoCost < 0
	return e
}

type NumberState string

const (
	NumberCharging NumberState = "charging"
	NumberFree     NumberState = "free"
	NumberNoAnswer NumberState = "no_answer"
)

type NumberMetaCost struct {
	PhoneID            string      `json:"phoneId"`
	DisplayPhoneNumber string      `json:"displayPhoneNumber"`
	Provider           string      `json:"provider"`
	WorkspaceName      string      `json:"workspaceName"`
	ServiceMessages    int64       `json:"serviceMessages"`
	Answered           int64       `json:"answered"`
	Charged            int64       `json:"charged"`
	FirstChargedAt     *time.Time  `json:"firstChargedAt,omitempty"`
	State              NumberState `json:"state"`
}

func (n NumberMetaCost) BillingState() NumberState {
	switch {
	case n.Charged > 0:
		return NumberCharging
	case n.Answered > 0:
		return NumberFree
	}
	return NumberNoAnswer
}

type UnlinkedNumber struct {
	PhoneNumberID      string `json:"phoneNumberId"`
	DisplayPhoneNumber string `json:"displayPhoneNumber"`
	Messages           int64  `json:"messages"`
}

func (t MetaServiceMessageCostTotals) ChargedWorkspaceIDs() []string {
	ids := make([]string, 0, len(t.ServiceCharges))
	for _, c := range t.ServiceCharges {
		ids = append(ids, c.WorkspaceID)
	}
	return ids
}

func (r *MetaServiceMessageCostReport) ApplyRates(rates CostRates, costs ServiceMessageCosts) {
	r.Rates = rates
	t := &r.Totals
	t.Answers = t.SplitAnswers()
	t.PaidByClients = rates.Money(t.PaidMicros)

	var serviceTotal, vozkoService int64
	serviceKnown, vozkoKnown := true, true
	t.ServiceCostMissing = 0
	for _, c := range t.ServiceCharges {
		cost, ok := costs.Cost(c.WorkspaceID, c.Charged)
		if !ok {
			t.ServiceCostMissing++
			serviceKnown = false
		}
		serviceTotal += cost
		if wsc.MetaPayer(c.MetaPayer) != wsc.MetaPayerClient {
			vozkoKnown = vozkoKnown && ok
			vozkoService += cost
		}
	}
	vozkoCost := t.VozkoTemplateCostMicros + vozkoService
	t.ConfirmedServiceCost = rates.optionalMoney(serviceTotal, serviceKnown)
	t.VozkoMetaCost = rates.optionalMoney(vozkoCost, vozkoKnown)
	t.RealMargin = rates.optionalMoney(t.PaidMicros-vozkoCost, vozkoKnown)

	if r.Workspaces != nil {
		for _, w := range r.Workspaces.Items {
			var serviceCost *int64
			if cost, ok := costs.Cost(w.WorkspaceID, w.MetaConfirmed); ok {
				serviceCost = &cost
			}
			w.Economics = NewWorkspaceEconomics(wsc.MetaPayer(w.MetaPayer), w.PaidMicros, w.TemplateCostMicros, serviceCost, rates)
		}
	}
	for _, n := range r.Numbers {
		n.State = n.BillingState()
	}
}

func (r *MetaServiceMessageCostReport) AttachDetails(numbers []*NumberMetaCost, unlinked []*UnlinkedNumber) {
	r.Numbers = numbers
	r.Unlinked = unlinked
	r.Totals.UnlinkedServiceMessages = 0
	for _, u := range unlinked {
		r.Totals.UnlinkedServiceMessages += u.Messages
	}
}
