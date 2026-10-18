package buttonaction

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/lead"
)

func TestNewButtonActionTakesTheLabelFromTheTemplate(t *testing.T) {
	action, err := NewButtonAction(promoTemplate(), Input{ButtonIndex: 1, Kind: KindOptOut, Confirmation: "  Entendido.  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action.Template != (TemplateRef{Name: "promo_outubro", Language: "pt_BR"}) {
		t.Errorf("template = %+v", action.Template)
	}
	if action.ButtonText != "SAIR" || action.ButtonIndex != 1 || action.Confirmation != "Entendido." {
		t.Errorf("action = %+v", action)
	}
}

func TestValidatePerKind(t *testing.T) {
	cases := []struct {
		name   string
		kind   Kind
		params Params
		want   error
	}{
		{"opt out needs nothing", KindOptOut, Params{}, nil},
		{"opt out takes no params", KindOptOut, Params{WorkflowID: "wf-1"}, ErrParamsInvalid},
		{"consent grant purpose is optional", KindConsentGrant, Params{}, nil},
		{"consent grant with a purpose", KindConsentGrant, Params{Purpose: "receber ofertas"}, nil},
		{"consent grant purpose at the consent limit", KindConsentGrant, Params{Purpose: strings.Repeat("a", lead.MaxConsentPurposeLength)}, nil},
		{"consent grant purpose too long", KindConsentGrant, Params{Purpose: strings.Repeat("a", lead.MaxConsentPurposeLength+1)}, ErrParamsInvalid},
		{"hand off to a team", KindHandOff, Params{DepartmentID: "dep-1"}, nil},
		{"hand off to a member", KindHandOff, Params{UserID: "user-1"}, nil},
		{"hand off needs a target", KindHandOff, Params{}, ErrParamsInvalid},
		{"hand off takes one target", KindHandOff, Params{DepartmentID: "dep-1", UserID: "user-1"}, ErrParamsInvalid},
		{"workflow needs its id", KindStartWorkflow, Params{}, ErrParamsInvalid},
		{"workflow with id", KindStartWorkflow, Params{WorkflowID: "wf-1"}, nil},
		{"call list needs its id", KindCallList, Params{}, ErrParamsInvalid},
		{"call list with id", KindCallList, Params{CallListID: "cl-1"}, nil},
		{"classify needs field and option", KindClassify, Params{FieldKey: "interesse"}, ErrParamsInvalid},
		{"classify with field and option", KindClassify, Params{FieldKey: "interesse", Option: "alto"}, nil},
		{"deal needs pipeline and stage", KindCreateDeal, Params{PipelineID: "p-1"}, ErrParamsInvalid},
		{"deal with pipeline and stage", KindCreateDeal, Params{PipelineID: "p-1", StageID: "s-1"}, nil},
		{"deal refuses foreign params", KindCreateDeal, Params{PipelineID: "p-1", StageID: "s-1", Purpose: "x"}, ErrParamsInvalid},
		{"unknown kind", Kind("send_template"), Params{}, ErrKindUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewButtonAction(promoTemplate(), Input{ButtonIndex: 0, Kind: tc.kind, Params: tc.params})
			if !errors.Is(err, tc.want) && !(tc.want == nil && err == nil) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidateRefusesAButtonThatIsNotAQuickReply(t *testing.T) {
	for _, index := range []int{-1, 2, 3, 10} {
		if _, err := NewButtonAction(promoTemplate(), Input{ButtonIndex: index, Kind: KindOptOut}); !errors.Is(err, ErrButtonNotQuickReply) {
			t.Errorf("index %d: got %v, want ErrButtonNotQuickReply", index, err)
		}
	}
}

func TestValidateRefusesAMissingTemplate(t *testing.T) {
	if _, err := NewButtonAction(nil, Input{ButtonIndex: 0, Kind: KindOptOut}); !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("got %v, want ErrTemplateNotFound", err)
	}
}

func TestValidateRefusesALongConfirmation(t *testing.T) {
	_, err := NewButtonAction(promoTemplate(), Input{ButtonIndex: 1, Kind: KindOptOut, Confirmation: strings.Repeat("a", MaxConfirmationLength+1)})
	if !errors.Is(err, ErrConfirmationTooLong) {
		t.Fatalf("got %v, want ErrConfirmationTooLong", err)
	}
}

func TestParamsAreTrimmed(t *testing.T) {
	action, err := NewButtonAction(promoTemplate(), Input{ButtonIndex: 0, Kind: KindHandOff, Params: Params{DepartmentID: "  dep-1 "}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action.Params.DepartmentID != "dep-1" {
		t.Fatalf("params = %+v", action.Params)
	}
}

func TestValidateOfAStoredActionChecksItAgainstTheTemplateAsStored(t *testing.T) {
	action, err := NewButtonAction(promoTemplate(), Input{ButtonIndex: 1, Kind: KindOptOut})
	if err != nil {
		t.Fatal(err)
	}
	if err := action.Validate(promoTemplate()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	withoutButtons := templateWith("Oi", "")
	if err := action.Validate(withoutButtons); !errors.Is(err, ErrButtonNotQuickReply) {
		t.Fatalf("got %v, want ErrButtonNotQuickReply", err)
	}
}

func TestAConsentGrantKeepsThePurposeTheWayTheConsentRecordStoresIt(t *testing.T) {
	action, err := NewButtonAction(promoTemplate(), Input{ButtonIndex: 0, Kind: KindConsentGrant, Params: Params{Purpose: "  receber   ofertas  "}})
	if err != nil {
		t.Fatal(err)
	}
	if action.Params.Purpose != "receber ofertas" {
		t.Fatalf("purpose = %q", action.Params.Purpose)
	}
}
