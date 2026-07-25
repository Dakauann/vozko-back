package template

import (
	"errors"
	"testing"
)

func TestBillingCategory_Utility(t *testing.T) {
	tmpl := &Template{Category: TemplateCategoryUtility}
	cat, err := tmpl.BillingCategory()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cat != "UTILITY" {
		t.Errorf("got %q, want UTILITY", cat)
	}
}

func TestBillingCategory_Marketing(t *testing.T) {
	tmpl := &Template{Category: TemplateCategoryMarketing}
	cat, err := tmpl.BillingCategory()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cat != "MARKETING" {
		t.Errorf("got %q, want MARKETING", cat)
	}
}

func TestBillingCategory_Authentication(t *testing.T) {
	tmpl := &Template{Category: TemplateCategoryAuthentication}
	cat, err := tmpl.BillingCategory()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cat != "AUTHENTICATION" {
		t.Errorf("got %q, want AUTHENTICATION", cat)
	}
}

func TestBillingCategory_NilTemplate(t *testing.T) {
	var tmpl *Template
	_, err := tmpl.BillingCategory()
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("got %v, want ErrTemplateNotFound", err)
	}
}

func TestBillingCategory_EmptyCategory(t *testing.T) {
	tmpl := &Template{Category: ""}
	_, err := tmpl.BillingCategory()
	if !errors.Is(err, ErrTemplateCategoryUnavailable) {
		t.Errorf("got %v, want ErrTemplateCategoryUnavailable", err)
	}
}

func TestBillingCategory_InvalidCategory(t *testing.T) {
	tmpl := &Template{Category: "PROMOTIONAL"}
	_, err := tmpl.BillingCategory()
	if !errors.Is(err, ErrTemplateCategoryUnavailable) {
		t.Errorf("got %v, want ErrTemplateCategoryUnavailable", err)
	}
}

func TestBillingCategory_CaseInsensitive_Lowercase(t *testing.T) {
	tmpl := &Template{Category: "utility"}
	cat, err := tmpl.BillingCategory()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cat != "UTILITY" {
		t.Errorf("got %q, want UTILITY", cat)
	}
}

func TestBillingCategory_CaseInsensitive_MixedCase(t *testing.T) {
	tmpl := &Template{Category: "Marketing"}
	cat, err := tmpl.BillingCategory()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cat != "MARKETING" {
		t.Errorf("got %q, want MARKETING", cat)
	}
}

func TestBillingCategory_WhitespaceHandling(t *testing.T) {
	tmpl := &Template{Category: "  AUTHENTICATION  "}
	cat, err := tmpl.BillingCategory()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cat != "AUTHENTICATION" {
		t.Errorf("got %q, want AUTHENTICATION", cat)
	}
}

func TestBillingCategory_WhitespaceOnly(t *testing.T) {
	tmpl := &Template{Category: "   "}
	_, err := tmpl.BillingCategory()
	if !errors.Is(err, ErrTemplateCategoryUnavailable) {
		t.Errorf("got %v, want ErrTemplateCategoryUnavailable", err)
	}
}
