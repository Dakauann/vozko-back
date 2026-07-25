package shortlink

import (
	"testing"
)

func TestCreateRequestValidate(t *testing.T) {
	valid := "2026-12-31T23:59:59Z"
	invalid := "not-a-date"
	badMax := int64(0)

	if errs := (createShortLinkRequest{TargetURL: "https://x.com", RedirectType: "302", ExpiresAt: &valid}).Validate(); errs != nil {
		t.Fatalf("valid create should pass: %v", errs)
	}
	if errs := (createShortLinkRequest{TargetURL: "  "}).Validate(); errs["targetUrl"] == "" {
		t.Fatal("empty target should fail")
	}
	if errs := (createShortLinkRequest{TargetURL: "https://x.com", RedirectType: "307"}).Validate(); errs["redirectType"] == "" {
		t.Fatal("bad redirect should fail")
	}
	if errs := (createShortLinkRequest{TargetURL: "https://x.com", ExpiresAt: &invalid}).Validate(); errs["expiresAt"] == "" {
		t.Fatal("bad expiresAt should fail")
	}
	if errs := (createShortLinkRequest{TargetURL: "https://x.com", MaxClicks: &badMax}).Validate(); errs["maxClicks"] == "" {
		t.Fatal("bad maxClicks should fail")
	}
}

func TestCreateRequestToInput(t *testing.T) {
	dep := "  dep-1  "
	exp := "2026-12-31T23:59:59Z"
	max := int64(100)
	req := createShortLinkRequest{
		TargetURL:    "  https://x.com  ",
		CustomAlias:  "  promo  ",
		Title:        "  Title  ",
		RedirectType: "301",
		Password:     "secret",
		DepartmentID: &dep,
		ExpiresAt:    &exp,
		MaxClicks:    &max,
	}
	in := req.toInput("ws-1", "user-1")
	if in.TargetURL != "https://x.com" || in.CustomAlias != "promo" || in.Title != "Title" {
		t.Fatalf("trims failed: %+v", in)
	}
	if in.DepartmentID == nil || *in.DepartmentID != "dep-1" {
		t.Fatalf("department = %v", in.DepartmentID)
	}
	if in.ExpiresAt == nil || in.MaxClicks == nil {
		t.Fatal("expiry/maxclicks not parsed")
	}
}

func TestUpdateRequestValidate(t *testing.T) {
	bad := "bogus"
	badExp := "nope"
	badMax := int64(-1)

	if errs := (updateShortLinkRequest{}).Validate(); errs != nil {
		t.Fatalf("empty update should pass: %v", errs)
	}
	if errs := (updateShortLinkRequest{RedirectType: &bad}).Validate(); errs["redirectType"] == "" {
		t.Fatal("bad redirect")
	}
	if errs := (updateShortLinkRequest{Status: &bad}).Validate(); errs["status"] == "" {
		t.Fatal("bad status")
	}
	if errs := (updateShortLinkRequest{ExpiresAt: &badExp}).Validate(); errs["expiresAt"] == "" {
		t.Fatal("bad expiresAt")
	}
	if errs := (updateShortLinkRequest{MaxClicks: &badMax}).Validate(); errs["maxClicks"] == "" {
		t.Fatal("bad maxClicks")
	}
}

func TestUpdateRequestToInput(t *testing.T) {
	url := "  https://new.com  "
	exp := "2026-12-31T23:59:59Z"
	req := updateShortLinkRequest{TargetURL: &url, ExpiresAt: &exp, ClearPassword: true}
	in := req.toInput()
	if in.TargetURL == nil || *in.TargetURL != "https://new.com" {
		t.Fatalf("target = %v", in.TargetURL)
	}
	if in.ExpiresAt == nil || !in.ClearPassword {
		t.Fatal("expiry/clear not mapped")
	}
}

func TestHelpers(t *testing.T) {
	valid := "2026-12-31T23:59:59Z"
	empty := "  "
	if !validExpiresAt(nil) || !validExpiresAt(&empty) || !validExpiresAt(&valid) {
		t.Fatal("valid expires cases")
	}
	bad := "nope"
	if validExpiresAt(&bad) {
		t.Fatal("bad expires should be invalid")
	}

	if parseExpiresAt(nil) != nil || parseExpiresAt(&empty) != nil || parseExpiresAt(&bad) != nil {
		t.Fatal("parse nil cases")
	}
	if parseExpiresAt(&valid) == nil {
		t.Fatal("valid parse")
	}

	if trimmedPtr(nil) != nil {
		t.Fatal("trimmedPtr nil")
	}
	spaced := "  x  "
	if got := trimmedPtr(&spaced); got == nil || *got != "x" {
		t.Fatalf("trimmedPtr = %v", got)
	}

	if normalizeDepartmentPtr(nil) != nil || normalizeDepartmentPtr(&empty) != nil {
		t.Fatal("normalizeDepartmentPtr nil cases")
	}
	dep := "  dep  "
	if got := normalizeDepartmentPtr(&dep); got == nil || *got != "dep" {
		t.Fatalf("normalizeDepartmentPtr = %v", got)
	}
}
