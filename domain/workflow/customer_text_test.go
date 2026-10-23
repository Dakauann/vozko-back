package workflow

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func customerState(vars map[string]interface{}) *RunState {
	state := NewRunState()
	for k, v := range vars {
		state.Set(k, v)
	}
	return &state
}

func TestCustomerTextRendersAnUnresolvedReferenceEmptyAndNamesIt(t *testing.T) {
	state := customerState(map[string]interface{}{"input": map[string]interface{}{"nome": "Ana"}})

	text, unresolved := RenderCustomerText("Olá {{input.nome}}, seu fone {{ input.telefone }}.", state)

	if text != "Olá Ana, seu fone ." {
		t.Fatalf("text = %q, want no template syntax left for the customer", text)
	}
	if !reflect.DeepEqual(unresolved, []string{"input.telefone"}) {
		t.Fatalf("unresolved = %v", unresolved)
	}
}

func TestCustomerTextNeverCarriesASecretToTheCustomer(t *testing.T) {
	text, unresolved := RenderCustomerText("token {{secret.api_key}}", customerState(nil))
	if text != "token " || !reflect.DeepEqual(unresolved, []string{"secret.api_key"}) {
		t.Fatalf("text = %q, unresolved = %v", text, unresolved)
	}
}

func TestCustomerValuesReadAsPortugueseText(t *testing.T) {
	cases := []struct {
		name  string
		value interface{}
		want  string
	}{
		{"true reads as a word", true, "Sim"},
		{"false reads as a word", false, "Não"},
		{"an ISO date reads day first", "2026-10-22", "22/10/2026"},
		{"an ISO timestamp keeps its own clock", "2026-10-22T13:05:00-03:00", "22/10/2026 13:05"},
		{"a fraction uses a decimal comma", 1234.5, "1234,5"},
		{"a whole JSON number never turns into an exponent", 52998224725.0, "52998224725"},
		{"an int stays plain", 3, "3"},
		{"plain text is kept", "texto 2026-10-22 dentro", "texto 2026-10-22 dentro"},
		{"nothing reads as empty", nil, ""},
		{"a list of words joins with commas", []interface{}{"a", true, 2.5}, "a, Sim, 2,5"},
		{"an object stays JSON", map[string]interface{}{"a": 1.0}, `{"a":1}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CustomerValueText(c.value); got != c.want {
				t.Fatalf("CustomerValueText(%#v) = %q, want %q", c.value, got, c.want)
			}
		})
	}
}

func TestCustomerTextFormatsTheValuesItInserts(t *testing.T) {
	state := customerState(map[string]interface{}{"pago": true, "vencimento": "2026-11-05", "total": 99.9})
	text, unresolved := RenderCustomerText("Pago: {{pago}}. Vence {{var.vencimento}}. Total {{total}}", state)
	if text != "Pago: Sim. Vence 05/11/2026. Total 99,9" || len(unresolved) != 0 {
		t.Fatalf("text = %q, unresolved = %v", text, unresolved)
	}
}

func TestTheSystemDateReachesTheCustomerDayFirst(t *testing.T) {
	text, _ := RenderCustomerText("{{sys.date}}", customerState(nil))
	if want := time.Now().UTC().Format("02/01/2006"); text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
}

func TestNonCustomerInterpolationKeepsRawValuesForConditionsAndRequests(t *testing.T) {
	state := customerState(map[string]interface{}{"pago": true, "vencimento": "2026-11-05", "total": 99.9})
	if got := Interpolate("{{pago}}|{{vencimento}}|{{total}}|{{falta}}", state, nil); got != "true|2026-11-05|99.9|{{falta}}" {
		t.Fatalf("Interpolate = %q, want raw values and the unresolved template kept", got)
	}
}

func TestCustomerTextOnANodeRecordsTheUnresolvedRefsOnTheStepOnce(t *testing.T) {
	trace := &StepTrace{}
	ctx := &NodeContext{State: customerState(nil), Trace: trace}

	CustomerText(ctx, "{{lead_name}} {{input.cpf}}")
	CustomerText(ctx, "{{lead_name}}")

	if got := trace.Unresolved(); !reflect.DeepEqual(got, []string{"lead_name", "input.cpf"}) {
		t.Fatalf("Unresolved() = %v", got)
	}
}

func TestTheStepKeepsABoundedListOfUnresolvedRefs(t *testing.T) {
	trace := &StepTrace{}
	for i := 0; i < MaxUnresolvedRefs+10; i++ {
		trace.RecordUnresolved(fmt.Sprintf("campo_%d", i))
	}
	trace.RecordUnresolved(strings.Repeat("x", 500))
	refs := trace.Unresolved()
	if len(refs) != MaxUnresolvedRefs {
		t.Fatalf("len = %d, want the cap %d", len(refs), MaxUnresolvedRefs)
	}
}

func TestAStepWithoutATraceStillRendersForTheCustomer(t *testing.T) {
	ctx := &NodeContext{State: customerState(nil)}
	if got := CustomerText(ctx, "Oi {{nome}}"); got != "Oi " {
		t.Fatalf("got %q", got)
	}
}
