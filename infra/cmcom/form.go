package cmcom

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/rcs"
)

var ErrFormUnmapped = errors.New("cmcom: the provider form asks for fields the sender cannot fill")

type formElement struct {
	ID                  string                `json:"id"`
	InputKey            string                `json:"inputKey"`
	InputDescriptionKey string                `json:"inputDescriptionKey"`
	InputType           string                `json:"inputType"`
	IsRequired          bool                  `json:"isRequired"`
	IsAdminElement      bool                  `json:"isAdminElement"`
	InputProperties     formElementProperties `json:"inputProperties"`
}

type formElementProperties struct {
	Selectable []string `json:"selectable"`
}

type formInput struct {
	FormElementID string `json:"formElementId"`
	InputValue    string `json:"inputValue"`
}

type formField struct {
	aliases []string
	values  func(rcs.Sender) []string
}

func text(pick func(rcs.Sender) string) func(rcs.Sender) []string {
	return func(s rcs.Sender) []string { return []string{pick(s)} }
}

var formFields = []formField{
	{[]string{"email"}, text(func(s rcs.Sender) string { return s.ContactEmail })},
	{[]string{"phone", "telephone", "contactnumber", "mobilenumber"}, text(func(s rcs.Sender) string { return s.ContactPhone })},
	{[]string{"contactname", "contactperson", "fullname", "representative"}, text(func(s rcs.Sender) string { return s.ContactName })},
	{[]string{"privacy"}, text(func(s rcs.Sender) string { return s.PrivacyURL })},
	{[]string{"terms", "conditions"}, text(func(s rcs.Sender) string { return s.TermsURL })},
	{[]string{"banner", "hero", "coverimage", "headerimage"}, text(func(s rcs.Sender) string { return s.BannerURL })},
	{[]string{"logo", "icon", "avatar"}, text(func(s rcs.Sender) string { return s.LogoURL })},
	{[]string{"website", "webaddress", "homepage"}, text(func(s rcs.Sender) string { return s.WebsiteURL })},
	{[]string{"color", "colour"}, text(func(s rcs.Sender) string { return s.Color })},
	{[]string{"description"}, text(func(s rcs.Sender) string { return s.Description })},
	{[]string{"billing"}, billingValues},
	{[]string{"usecase", "purpose"}, useCaseValues},
	{[]string{"cnpj", "taxid", "taxnumber", "vatnumber", "companyregistration", "registrationnumber", "chamberofcommerce"}, text(func(s rcs.Sender) string { return s.CNPJ })},
	{[]string{"industry", "sector", "vertical"}, sectorValues},
	{[]string{"country", "countries"}, func(rcs.Sender) []string { return []string{"BR", "Brazil", "Brasil", "BRA"} }},
	{[]string{"brandname", "agentname", "profilename", "displayname", "sendername", "companyname", "businessname", "name"}, text(func(s rcs.Sender) string { return s.BrandName })},
}

func billingValues(s rcs.Sender) []string {
	switch s.BillingCategory {
	case rcs.BillingConversational:
		return []string{"CONVERSATIONAL"}
	case rcs.BillingNonConversational:
		return []string{"NON_CONVERSATIONAL"}
	}
	return nil
}

func useCaseValues(s rcs.Sender) []string {
	if !s.UseCase.Valid() {
		return nil
	}
	return []string{strings.ToUpper(string(s.UseCase))}
}

func sectorValues(s rcs.Sender) []string {
	if !s.Sector.Valid() {
		return nil
	}
	return []string{string(s.Sector)}
}

func normalizeKey(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(raw) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func fieldFor(inputKey string) (formField, bool) {
	key := normalizeKey(inputKey)
	if key == "" {
		return formField{}, false
	}
	for _, field := range formFields {
		for _, alias := range field.aliases {
			if strings.Contains(key, alias) {
				return field, true
			}
		}
	}
	return formField{}, false
}

func (f formField) valueFor(el formElement, s rcs.Sender) (string, bool) {
	candidates := f.values(s)
	if len(candidates) == 0 || strings.TrimSpace(candidates[0]) == "" {
		return "", false
	}
	if len(el.InputProperties.Selectable) == 0 {
		return strings.TrimSpace(candidates[0]), true
	}
	for _, option := range el.InputProperties.Selectable {
		for _, candidate := range candidates {
			if normalizeKey(option) != "" && normalizeKey(option) == normalizeKey(candidate) {
				return option, true
			}
		}
	}
	return "", false
}

func formInputs(elements []formElement, s rcs.Sender) ([]formInput, error) {
	var out []formInput
	var unmapped []string
	for _, el := range elements {
		if el.IsAdminElement {
			continue
		}
		name := strings.TrimSpace(el.InputKey)
		if name == "" {
			name = el.ID
		}
		value, ok := "", false
		if field, found := fieldFor(el.InputKey); found && strings.TrimSpace(el.ID) != "" {
			value, ok = field.valueFor(el, s)
		}
		if !ok {
			if el.IsRequired {
				unmapped = append(unmapped, name)
			}
			continue
		}
		out = append(out, formInput{FormElementID: el.ID, InputValue: value})
	}
	if len(unmapped) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrFormUnmapped, strings.Join(unmapped, ", "))
	}
	return out, nil
}

func requiredKeys(elements []formElement) []string {
	var out []string
	for _, el := range elements {
		if el.IsRequired && !el.IsAdminElement {
			out = append(out, el.InputKey)
		}
	}
	return out
}
