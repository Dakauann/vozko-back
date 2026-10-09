package crmfilter

import (
	"fmt"

	"vozko/domain/crmfilter"
)

type OpportunityDescriptor struct {
	Alias string
}

func NewOpportunityDescriptor() OpportunityDescriptor {
	return OpportunityDescriptor{Alias: "o"}
}

func (d OpportunityDescriptor) Object() string { return "opportunity" }

func (d OpportunityDescriptor) CustomFieldsColumn() string { return d.alias() + ".custom_fields" }

func (d OpportunityDescriptor) alias() string {
	if d.Alias == "" {
		return "o"
	}
	return d.Alias
}

func (d OpportunityDescriptor) Field(field crmfilter.Field) (FieldMapping, error) {
	a := d.alias()
	col := func(name string) string { return a + "." + name }

	switch field {
	case crmfilter.FieldOwner:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindIDSet, Expr: col("owner_id")}, nil
	case crmfilter.FieldCarteira:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindIDSet, Expr: col("carteira_id")}, nil
	case crmfilter.FieldPipeline:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindIDSet, Expr: col("pipeline_id")}, nil
	case crmfilter.FieldStage:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindIDSet, Expr: col("stage_id")}, nil
	case crmfilter.FieldLostReason:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindIDSet, Expr: col("lost_reason_id")}, nil

	case crmfilter.FieldStatus:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindEnum, Expr: col("status")}, nil
	case crmfilter.FieldSource:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindEnum, Expr: col("source")}, nil

	case crmfilter.FieldValue:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindNumber, Expr: col("value_cents")}, nil

	case crmfilter.FieldCreatedAt:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindDate, Expr: col("created_at")}, nil
	case crmfilter.FieldUpdatedAt:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindDate, Expr: col("updated_at")}, nil
	case crmfilter.FieldCloseDate:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindDate, Expr: col("close_date")}, nil

	case crmfilter.FieldQuery:
		return FieldMapping{Style: StyleText, Kind: crmfilter.KindText, Template: col("title") + " ILIKE ?", Params: 1}, nil

	default:
		return FieldMapping{}, fmt.Errorf("%w: %q on %s", ErrUnsupportedField, field, d.Object())
	}
}
