package lead_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/leadimport"
	"vozko/domain/workspace"
)

func withoutConsent() fakePermissions {
	perms := allImportPermissions()
	delete(perms, "leads:consent")
	return perms
}

func consentColumns() leadimport.Settings {
	s := leadimport.Settings{Policy: "fill_empty"}
	for i, field := range []string{leadimport.FieldNumber, leadimport.FieldName, leadimport.FieldConsentDate, leadimport.FieldConsentPurpose} {
		s.Columns = append(s.Columns, leadimport.Column{Index: i, Field: field})
	}
	return s
}

func assertConsentGrantRefused(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, workspace.ErrConsentGrantForbidden) || !errors.Is(err, leadimport.ErrForbidden) {
		t.Fatalf("err = %v, want the consent grant refusal", err)
	}
}

func TestADryRunMappingTheConsentColumnNeedsTheConsentPermission(t *testing.T) {
	f := newImportFixture(t, withoutConsent())
	job := f.upload(t, consentFile)

	_, err := f.imp.DryRun(context.Background(), importer(), job.ID, consentColumns())
	assertConsentGrantRefused(t, err)
	if f.store.job(job.ID).Status != leadimport.StatusUploaded || len(f.runs.runs) != 0 {
		t.Fatal("a refused dry run changed the import")
	}
}

func TestStartingAnImportThatGrantsConsentChecksThePermissionAgainBeforeAnyRow(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	job := f.upload(t, consentFile)
	job = f.dryRun(t, job, consentColumns())
	delete(f.perms, "leads:consent")

	_, err := f.imp.Start(context.Background(), importer(), job.ID, StartChoices{})
	assertConsentGrantRefused(t, err)
	if got := f.store.job(job.ID); got.Status != leadimport.StatusAnalyzed || f.base.writes != 0 {
		t.Fatalf("job = %+v, writes = %d", got, f.base.writes)
	}
}

func TestTheWorkerStopsAnImportThatGrantsConsentWhenThePermissionIsGone(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	job := f.upload(t, consentFile)
	job = f.dryRun(t, job, consentColumns())
	if _, err := f.imp.Start(context.Background(), importer(), job.ID, StartChoices{}); err != nil {
		t.Fatal(err)
	}
	delete(f.perms, "leads:consent")
	f.runs.drain()

	got := f.store.job(job.ID)
	if got.Status != leadimport.StatusFailed || got.FailureCode != leadimport.FailureForbidden || f.base.writes != 0 {
		t.Fatalf("job = %+v, writes = %d", got, f.base.writes)
	}
}

func TestAnImportWithoutTheConsentColumnNeedsNoConsentPermission(t *testing.T) {
	f := newImportFixture(t, withoutConsent())
	job := f.upload(t, consentFile)
	s := leadimport.Settings{Policy: "fill_empty", Columns: []leadimport.Column{{Index: 0, Field: leadimport.FieldNumber}, {Index: 1, Field: leadimport.FieldName}}}
	job = f.dryRun(t, job, s)
	job = f.start(t, job)
	if job.Status != leadimport.StatusDone || job.Result == nil || job.Result.Created != 1 {
		t.Fatalf("job = %+v", job)
	}
}

func TestTheCatalogMarksTheConsentColumnsByTheConsentPermission(t *testing.T) {
	for _, tc := range []struct {
		name    string
		perms   fakePermissions
		allowed bool
	}{
		{"with the permission", allImportPermissions(), true},
		{"without the permission", withoutConsent(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog, err := newImportFixture(t, tc.perms).imp.Catalog(context.Background(), importer())
			if err != nil {
				t.Fatal(err)
			}
			seen := 0
			for _, f := range catalog.Fields {
				if f.Group != leadimport.GroupConsent {
					continue
				}
				seen++
				if f.Allowed != tc.allowed || f.Requires != workspace.ActionConsent {
					t.Fatalf("%s: allowed %v requires %q", f.Key, f.Allowed, f.Requires)
				}
			}
			if seen != 2 {
				t.Fatalf("%d consent fields in the catalog", seen)
			}
		})
	}
}
