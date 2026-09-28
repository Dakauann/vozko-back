package opportunity_usecase

import (
	"errors"

	"vozko/domain/customfield"
	"vozko/domain/opportunity"
)

type Refusal string

const (
	RefusalWonWithoutValue     Refusal = "won_without_value"
	RefusalNoOpenDeal          Refusal = "no_open_deal"
	RefusalLostReasonMissing   Refusal = "lost_reason_missing"
	RefusalTitleMissing        Refusal = "title_missing"
	RefusalStageInvalid        Refusal = "stage_invalid"
	RefusalAmountInvalid       Refusal = "amount_invalid"
	RefusalCurrencyUnsupported Refusal = "currency_unsupported"
	RefusalPipelineInvalid     Refusal = "pipeline_invalid"
	RefusalRequiredFields      Refusal = "required_fields"
	RefusalAmbiguousDeal       Refusal = "ambiguous_deal"
	RefusalDealNotLinked       Refusal = "deal_not_linked"
	RefusalDealClosed          Refusal = "deal_closed"
	RefusalDealIDOnNewDeal     Refusal = "deal_id_on_new_deal"
	RefusalDealChanged         Refusal = "deal_changed"
)

var refusalCauses = []struct {
	refusal Refusal
	causes  []error
}{
	{RefusalWonWithoutValue, []error{opportunity.ErrWonWithoutValue}},
	{RefusalNoOpenDeal, []error{ErrNoOpenDeal}},
	{RefusalLostReasonMissing, []error{opportunity.ErrLostReasonMissing}},
	{RefusalTitleMissing, []error{opportunity.ErrTitleOrLead}},
	{RefusalStageInvalid, []error{opportunity.ErrStageRequired, opportunity.ErrStageOutsidePipeline, ErrStageNotFound}},
	{RefusalAmountInvalid, []error{opportunity.ErrInvalidAmount, opportunity.ErrNegativeValue}},
	{RefusalCurrencyUnsupported, []error{opportunity.ErrUnsupportedCurrency}},
	{RefusalPipelineInvalid, []error{
		ErrPipelineNotFound, ErrNotOpportunityPipeline, opportunity.ErrPipelineRequired,
		ErrPipelineHasNoOpenStage, ErrPipelineHasNoWonStage, ErrPipelineHasNoLostStage,
	}},
	{RefusalRequiredFields, []error{customfield.ErrValueRequired}},
	{RefusalAmbiguousDeal, []error{opportunity.ErrAmbiguousDeal}},
	{RefusalDealNotLinked, []error{opportunity.ErrDealNotLinked}},
	{RefusalDealClosed, []error{opportunity.ErrDealClosed}},
	{RefusalDealIDOnNewDeal, []error{ErrDealIDOnNewDeal}},
	{RefusalDealChanged, []error{opportunity.ErrStaleDeal}},
}

func Refusals() []Refusal {
	out := make([]Refusal, 0, len(refusalCauses))
	for _, entry := range refusalCauses {
		out = append(out, entry.refusal)
	}
	return out
}

func RefusalOf(err error) (Refusal, bool) {
	for _, entry := range refusalCauses {
		for _, cause := range entry.causes {
			if errors.Is(err, cause) {
				return entry.refusal, true
			}
		}
	}
	return "", false
}
