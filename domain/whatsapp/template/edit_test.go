package template

import (
	"errors"
	"testing"
	"time"
)

var editNow = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func editable(status TemplateStatus, category TemplateCategory) *Template {
	return &Template{ID: "tpl-1", ExternalID: "x1", WABAId: "waba-1", Name: "promo", Language: "pt_BR", Status: status, Category: category}
}

func editBody(text string) []TemplateComponent {
	return []TemplateComponent{{Type: "BODY", Text: text}}
}

func TestOnlyApprovedRejectedOrPausedTemplatesCanBeEdited(t *testing.T) {
	cases := map[TemplateStatus]error{
		TemplateStatusApproved: nil,
		TemplateStatusRejected: nil,
		TemplateStatusPaused:   nil,
		TemplateStatusPending:  ErrTemplateNotEditable,
		TemplateStatusDisabled: ErrTemplateNotEditable,
	}
	for status, want := range cases {
		err := CheckEdit(editable(status, TemplateCategoryMarketing), Edit{Components: editBody("Oi")}, nil, editNow)
		if !errors.Is(err, want) && !(want == nil && err == nil) {
			t.Errorf("%s: got %v, want %v", status, err, want)
		}
	}
}

func TestAnApprovedTemplateKeepsItsCategory(t *testing.T) {
	edit := Edit{Category: TemplateCategoryUtility, Components: editBody("Oi")}
	if err := CheckEdit(editable(TemplateStatusApproved, TemplateCategoryMarketing), edit, nil, editNow); !errors.Is(err, ErrEditCategoryLocked) {
		t.Fatalf("got %v", err)
	}
	if err := CheckEdit(editable(TemplateStatusRejected, TemplateCategoryMarketing), edit, nil, editNow); err != nil {
		t.Fatalf("a rejected template may change category: %v", err)
	}
	same := Edit{Category: TemplateCategoryMarketing, Components: editBody("Oi")}
	if err := CheckEdit(editable(TemplateStatusApproved, TemplateCategoryMarketing), same, nil, editNow); err != nil {
		t.Fatalf("restating the category is not a change: %v", err)
	}
}

func TestAuthenticationTemplatesAreNotEditedHere(t *testing.T) {
	err := CheckEdit(editable(TemplateStatusApproved, TemplateCategoryAuthentication), Edit{Components: editBody("Oi")}, nil, editNow)
	if !errors.Is(err, ErrEditUnsupported) {
		t.Fatalf("got %v", err)
	}
}

func TestAnApprovedTemplateIsEditedOncePerDay(t *testing.T) {
	previous := []time.Time{editNow.Add(-23 * time.Hour)}
	err := CheckEdit(editable(TemplateStatusApproved, TemplateCategoryMarketing), Edit{Components: editBody("Oi")}, previous, editNow)
	var wait *EditWindowError
	if !errors.As(err, &wait) || !errors.Is(err, ErrEditWindow) || !wait.NextAt.Equal(editNow.Add(time.Hour)) {
		t.Fatalf("got %v", err)
	}
	if err := CheckEdit(editable(TemplateStatusApproved, TemplateCategoryMarketing), Edit{Components: editBody("Oi")}, []time.Time{editNow.Add(-25 * time.Hour)}, editNow); err != nil {
		t.Fatalf("a day later: %v", err)
	}
}

func TestAnApprovedTemplateIsEditedTenTimesPerThirtyDays(t *testing.T) {
	var previous []time.Time
	for day := 2; day <= 11; day++ {
		previous = append(previous, editNow.Add(-time.Duration(day)*24*time.Hour))
	}
	err := CheckEdit(editable(TemplateStatusApproved, TemplateCategoryMarketing), Edit{Components: editBody("Oi")}, previous, editNow)
	var wait *EditWindowError
	if !errors.As(err, &wait) || !wait.NextAt.Equal(editNow.Add(-11*24*time.Hour).Add(30*24*time.Hour)) {
		t.Fatalf("got %v", err)
	}
}

func TestRejectedAndPausedTemplatesHaveNoEditLimit(t *testing.T) {
	previous := []time.Time{editNow.Add(-time.Minute), editNow.Add(-2 * time.Minute)}
	for _, status := range []TemplateStatus{TemplateStatusRejected, TemplateStatusPaused} {
		if err := CheckEdit(editable(status, TemplateCategoryMarketing), Edit{Components: editBody("Oi")}, previous, editNow); err != nil {
			t.Errorf("%s: %v", status, err)
		}
	}
}

func TestTheEditedComponentsAreValidatedLikeANewTemplate(t *testing.T) {
	err := CheckEdit(editable(TemplateStatusApproved, TemplateCategoryMarketing), Edit{Components: []TemplateComponent{{Type: "FOOTER", Text: "Sem corpo"}}}, nil, editNow)
	if err == nil {
		t.Fatal("a template without a body must be refused")
	}
}

func TestAMediaHeaderIsValidatedBeforeItsHandleExists(t *testing.T) {
	components := []TemplateComponent{{Type: "HEADER", Format: "IMAGE"}, {Type: "BODY", Text: "Oi"}}
	if err := CheckEdit(editable(TemplateStatusApproved, TemplateCategoryMarketing), Edit{Components: components}, nil, editNow); err != nil {
		t.Fatalf("the handle is uploaded after the check: %v", err)
	}
}

func TestEditErrorsAreCoded(t *testing.T) {
	cases := map[error]string{
		ErrTemplateNotEditable: "template_not_editable",
		ErrEditCategoryLocked:  "template_edit_category_locked",
		ErrEditUnsupported:     "template_edit_unsupported",
		ErrEditWindow:          "template_edit_window",
		ErrTemplateInUse:       "template_in_use",
		ErrEditNotOwner:        "template_edit_not_owner",
	}
	for err, code := range cases {
		if got := ErrorCode(err); got != code {
			t.Errorf("%v: got %s, want %s", err, got, code)
		}
	}
}
