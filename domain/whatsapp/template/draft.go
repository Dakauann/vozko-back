package template

import "strings"

func ValidateDraft(name string, category TemplateCategory, components []TemplateComponent) error {
	if err := ValidateName(strings.ToLower(strings.TrimSpace(name))); err != nil {
		return err
	}
	if !category.IsValid() {
		return ErrInvalidCategory
	}
	if err := ValidateComponents(components); err != nil {
		return err
	}
	return ValidateAuthenticationTemplate(category, components)
}
