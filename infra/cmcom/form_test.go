package cmcom

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/rcs"
)

func testSender() rcs.Sender {
	return rcs.Sender{
		BrandName:       "Padaria Sol",
		Description:     "Atualizações de pedidos da Padaria Sol",
		LogoURL:         "https://cdn.example.com/logo.png",
		BannerURL:       "https://cdn.example.com/banner.png",
		Color:           "#FF8800",
		UseCase:         rcs.UseCaseTransactional,
		Sector:          rcs.SectorRetail,
		ContactName:     "Maria Souza",
		ContactEmail:    "maria@padariasol.com.br",
		ContactPhone:    "+5511987654321",
		WebsiteURL:      "https://padariasol.com.br",
		PrivacyURL:      "https://padariasol.com.br/privacidade",
		TermsURL:        "https://padariasol.com.br/termos",
		CNPJ:            "12345678000195",
		BillingCategory: rcs.BillingNonConversational,
	}
}

func element(id, key string, required bool, selectable ...string) formElement {
	return formElement{ID: id, InputKey: key, IsRequired: required, InputProperties: formElementProperties{Selectable: selectable}}
}

func inputsByID(inputs []formInput) map[string]string {
	out := make(map[string]string, len(inputs))
	for _, in := range inputs {
		out[in.FormElementID] = in.InputValue
	}
	return out
}

func TestFormInputsMapsEveryBrandField(t *testing.T) {
	elements := []formElement{
		element("e1", "brandName", true),
		element("e2", "brandDescription", true),
		element("e3", "logoImage", true),
		element("e4", "heroImage", true),
		element("e5", "brandColor", true),
		element("e6", "websiteUrl", true),
		element("e7", "privacyPolicyUrl", true),
		element("e8", "termsOfServiceUrl", true),
		element("e9", "contactName", true),
		element("e10", "contactEmail", true),
		element("e11", "contactPhoneNumber", true),
		element("e12", "billingCategory", true, "CONVERSATIONAL", "NON_CONVERSATIONAL"),
		element("e13", "useCase", true, "OTP", "TRANSACTIONAL", "PROMOTIONAL", "MULTI_USE"),
		element("e14", "companyRegistrationNumber", true),
		element("e15", "countries", true, "NL", "BR", "DE"),
		element("e16", "industry", true, "Retail", "Finance"),
	}
	got, err := formInputs(elements, testSender())
	if err != nil {
		t.Fatalf("formInputs: %v", err)
	}
	want := map[string]string{
		"e1":  "Padaria Sol",
		"e2":  "Atualizações de pedidos da Padaria Sol",
		"e3":  "https://cdn.example.com/logo.png",
		"e4":  "https://cdn.example.com/banner.png",
		"e5":  "#FF8800",
		"e6":  "https://padariasol.com.br",
		"e7":  "https://padariasol.com.br/privacidade",
		"e8":  "https://padariasol.com.br/termos",
		"e9":  "Maria Souza",
		"e10": "maria@padariasol.com.br",
		"e11": "+5511987654321",
		"e12": "NON_CONVERSATIONAL",
		"e13": "TRANSACTIONAL",
		"e14": "12345678000195",
		"e15": "BR",
		"e16": "Retail",
	}
	if !reflect.DeepEqual(inputsByID(got), want) {
		t.Fatalf("got %v\nwant %v", inputsByID(got), want)
	}
	for i, in := range got {
		if in.FormElementID != elements[i].ID {
			t.Fatalf("inputs must follow the form order, got %v", got)
		}
	}
}

func TestFormInputsKeyMatching(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"rcs.profile.agent_name", "Padaria Sol"},
		{"DISPLAY_NAME", "Padaria Sol"},
		{"brand-colour", "#FF8800"},
		{"agentLogo", "https://cdn.example.com/logo.png"},
		{"bannerImage", "https://cdn.example.com/banner.png"},
		{"privacy", "https://padariasol.com.br/privacidade"},
		{"termsAndConditions", "https://padariasol.com.br/termos"},
		{"homepage", "https://padariasol.com.br"},
		{"brandEmail", "maria@padariasol.com.br"},
		{"brandPhone", "+5511987654321"},
		{"contactPerson", "Maria Souza"},
		{"cnpj", "12345678000195"},
		{"taxId", "12345678000195"},
		{"description", "Atualizações de pedidos da Padaria Sol"},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			got, err := formInputs([]formElement{element("x", tc.key, true)}, testSender())
			if err != nil {
				t.Fatalf("formInputs: %v", err)
			}
			if len(got) != 1 || got[0].InputValue != tc.want {
				t.Fatalf("got %v, want %q", got, tc.want)
			}
		})
	}
}

func TestFormInputsSelectables(t *testing.T) {
	conversational := testSender()
	conversational.BillingCategory = rcs.BillingConversational
	multiUse := testSender()
	multiUse.UseCase = rcs.UseCaseMultiUse
	cases := []struct {
		name   string
		sender rcs.Sender
		el     formElement
		want   string
	}{
		{"conversational", conversational, element("b", "billingCategory", true, "NON_CONVERSATIONAL", "CONVERSATIONAL"), "CONVERSATIONAL"},
		{"title case billing", testSender(), element("b", "billing", true, "Conversational", "Non-conversational"), "Non-conversational"},
		{"billing free text", testSender(), element("b", "billingCategory", true), "NON_CONVERSATIONAL"},
		{"multi use", multiUse, element("u", "useCase", true, "OTP", "Multi-use"), "Multi-use"},
		{"use case free text", multiUse, element("u", "useCase", true), "MULTI_USE"},
		{"country by name", testSender(), element("c", "country", true, "Netherlands", "Brazil"), "Brazil"},
		{"country free text", testSender(), element("c", "country", true), "BR"},
		{"free text with matching selectable", testSender(), element("n", "brandName", true, "padaria sol"), "padaria sol"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := formInputs([]formElement{tc.el}, tc.sender)
			if err != nil {
				t.Fatalf("formInputs: %v", err)
			}
			if len(got) != 1 || got[0].InputValue != tc.want {
				t.Fatalf("got %v, want %q", got, tc.want)
			}
		})
	}
}

func TestFormInputsFailsClosed(t *testing.T) {
	noTerms := testSender()
	noTerms.TermsURL = " "
	cases := []struct {
		name     string
		sender   rcs.Sender
		elements []formElement
		names    string
	}{
		{"unknown required element", testSender(), []formElement{element("e1", "brandName", true), element("e2", "googleVerificationToken", true)}, "googleVerificationToken"},
		{"required element without a value", noTerms, []formElement{element("e1", "termsUrl", true)}, "termsUrl"},
		{"selectable without our value", testSender(), []formElement{element("e1", "billingCategory", true, "UNKNOWN")}, "billingCategory"},
		{"country list without brazil", testSender(), []formElement{element("e1", "countries", true, "NL", "DE")}, "countries"},
		{"industry not offered", testSender(), []formElement{element("e1", "industry", true, "Gaming")}, "industry"},
		{"element without key", testSender(), []formElement{element("e1", " ", true)}, "e1"},
		{"element without id", testSender(), []formElement{element("", "brandName", true)}, "brandName"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := formInputs(tc.elements, tc.sender)
			if !errors.Is(err, ErrFormUnmapped) {
				t.Fatalf("formInputs = %v, %v; want ErrFormUnmapped", got, err)
			}
			if !strings.Contains(err.Error(), tc.names) {
				t.Fatalf("error %q must name %q", err, tc.names)
			}
		})
	}
}

func TestFormInputsSkipsWhatItMayLeaveOut(t *testing.T) {
	noBanner := testSender()
	noBanner.BannerURL = ""
	elements := []formElement{
		element("e1", "brandName", true),
		element("e2", "internalNotes", false),
		element("e3", "heroImage", false),
		{ID: "e4", InputKey: "accountManager", IsRequired: true, IsAdminElement: true},
		element("e5", "industry", false, "Gaming"),
	}
	got, err := formInputs(elements, noBanner)
	if err != nil {
		t.Fatalf("formInputs: %v", err)
	}
	if want := map[string]string{"e1": "Padaria Sol"}; !reflect.DeepEqual(inputsByID(got), want) {
		t.Fatalf("got %v, want %v", inputsByID(got), want)
	}
}
