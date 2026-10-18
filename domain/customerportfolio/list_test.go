package customerportfolio

import (
	"errors"
	"strings"
	"testing"
)

func TestListQueryValidation(t *testing.T) {
	if err := (ListQuery{Page: -1}).Validate(); !errors.Is(err, ErrListQueryInvalid) {
		t.Fatalf("negative page = %v", err)
	}
	if err := (ListQuery{Search: strings.Repeat("a", MaxSearchLength+1)}).Validate(); !errors.Is(err, ErrListQueryInvalid) {
		t.Fatalf("long search = %v", err)
	}
	if err := (ListQuery{HolderID: "u1", Search: "ana", Page: 1, PageSize: 20}).Validate(); err != nil {
		t.Fatalf("valid query = %v", err)
	}
}
