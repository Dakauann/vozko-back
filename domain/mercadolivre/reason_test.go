package mercadolivre

import (
	"errors"
	"strings"
	"testing"
)

func reasonOptions() []ReasonOption {
	return []ReasonOption{
		{ID: ReasonOther, Enabled: true, Actionable: true, CharLimit: 350, CapAvailable: 1},
		{ID: ReasonRequestVariants, Enabled: true, Actionable: true, CapAvailable: 1, Templates: []ReasonTemplate{
			{ID: "TEMPLATE_VARIANT", Vars: []ReasonVar{{ID: "DAYS", Type: "int"}}},
		}},
		{ID: ReasonDeliveryPromise, Enabled: false, Actionable: true, CapAvailable: 1},
	}
}

func TestCapsFromMercadoLivreOverrideTheOptionCaps(t *testing.T) {
	merged := WithCaps(reasonOptions(), []ReasonCap{{OptionID: ReasonOther, CapAvailable: 0}, {OptionID: "UNKNOWN", CapAvailable: 9}})
	if len(merged) != 3 || merged[0].CapAvailable != 0 || merged[1].CapAvailable != 1 {
		t.Fatalf("merged = %+v", merged)
	}
	if reasonOptions()[0].CapAvailable != 1 {
		t.Fatal("WithCaps must not change its input")
	}
}

func TestAReasonSendIsCheckedAgainstTheOfferedOptions(t *testing.T) {
	cases := []struct {
		name string
		send ReasonSend
		opts []ReasonOption
		want error
	}{
		{"free text", ReasonSend{OptionID: ReasonOther, Text: "Olá, seu pedido sai amanhã"}, reasonOptions(), nil},
		{"template with its vars", ReasonSend{OptionID: ReasonRequestVariants, TemplateID: "TEMPLATE_VARIANT", Vars: []ReasonVarValue{{ID: "DAYS", Value: 2}}}, reasonOptions(), nil},
		{"unknown option", ReasonSend{OptionID: "SURPRISE", Text: "oi"}, reasonOptions(), ErrReasonUnknown},
		{"disabled option", ReasonSend{OptionID: ReasonDeliveryPromise, Text: "oi"}, reasonOptions(), ErrReasonUnavailable},
		{"cap spent", ReasonSend{OptionID: ReasonOther, Text: "oi"}, WithCaps(reasonOptions(), []ReasonCap{{OptionID: ReasonOther}}), ErrReasonCapReached},
		{"no content", ReasonSend{OptionID: ReasonOther}, reasonOptions(), ErrReasonContent},
		{"both template and text", ReasonSend{OptionID: ReasonRequestVariants, TemplateID: "TEMPLATE_VARIANT", Text: "oi", Vars: []ReasonVarValue{{ID: "DAYS", Value: 2}}}, reasonOptions(), ErrReasonContent},
		{"unknown template", ReasonSend{OptionID: ReasonRequestVariants, TemplateID: "OTHER"}, reasonOptions(), ErrReasonContent},
		{"missing var", ReasonSend{OptionID: ReasonRequestVariants, TemplateID: "TEMPLATE_VARIANT"}, reasonOptions(), ErrReasonContent},
		{"extra var", ReasonSend{OptionID: ReasonRequestVariants, TemplateID: "TEMPLATE_VARIANT", Vars: []ReasonVarValue{{ID: "DAYS", Value: 2}, {ID: "X", Value: 1}}}, reasonOptions(), ErrReasonContent},
		{"charset", ReasonSend{OptionID: ReasonOther, Text: "aspas “curvas”"}, reasonOptions(), ErrTextCharset},
		{"over the option limit", ReasonSend{OptionID: ReasonOther, Text: strings.Repeat("a", 351)}, reasonOptions(), ErrTextTooLong},
		{"over a tighter option limit", ReasonSend{OptionID: ReasonOther, Text: strings.Repeat("a", 101)},
			[]ReasonOption{{ID: ReasonOther, Enabled: true, Actionable: true, CharLimit: 100, CapAvailable: 1}}, ErrTextTooLong},
	}
	for _, tc := range cases {
		err := ValidateReasonSend(tc.opts, tc.send)
		if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestTemplateTextsAreShownInTheAccountSiteOnly(t *testing.T) {
	options := []ReasonOption{
		{ID: ReasonDeliveryPromise, Description: "Promessa de entrega", Templates: []ReasonTemplate{
			{ID: "TEMPLATE___DELIVERY_PROMISE___1", TextsBySite: map[string]string{"MLA": "Hola", "MLB": "Olá,\nEntregaremos sua compra %s entre %d e %d h."}},
			{ID: "TEMPLATE___ONLY_MLA", TextsBySite: map[string]string{"MLA": "Hola"}},
			{ID: "TEMPLATE___NO_TEXT"},
		}},
		{ID: ReasonOther},
	}
	got := ForSite(options, "mlb")
	if len(got) != 2 || got[0].Description != "Promessa de entrega" {
		t.Fatalf("options = %+v", got)
	}
	cases := []struct {
		template int
		want     string
	}{
		{0, "Olá,\nEntregaremos sua compra %s entre %d e %d h."},
		{1, ""},
		{2, ""},
	}
	for _, tc := range cases {
		tpl := got[0].Templates[tc.template]
		if tpl.Text != tc.want || tpl.TextsBySite != nil {
			t.Errorf("template %s = %+v", tpl.ID, tpl)
		}
	}
	if options[0].Templates[0].Text != "" || options[0].Templates[0].TextsBySite == nil {
		t.Fatal("ForSite must not change its input")
	}
	if ForSite(options, "")[0].Templates[0].Text != "" {
		t.Fatal("an account without a site shows no template text")
	}
}
