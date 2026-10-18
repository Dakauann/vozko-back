package lead_usecase

import (
	"testing"

	"vozko/domain/lead"
	"vozko/domain/leadimport"
)

const consentFile = "telefone,nome,consentimento,finalidade\n" +
	"5511955554444,Lia,2026-09-01,novidades da escola\n"

func TestAnImportedConsentCitesItsJobLineAndWhoRanIt(t *testing.T) {
	f := newImportFixture(t, allImportPermissions())
	job := f.upload(t, consentFile)
	job = f.dryRun(t, job, suggested(job))
	job = f.start(t, job)

	if job.Status != leadimport.StatusDone || job.Result == nil || job.Result.Created != 1 {
		t.Fatalf("job = %+v result %+v", job, job.Result)
	}
	lia := f.base.byNumber("5511955554444")
	if lia == nil || lia.WhatsAppOptIn == nil || lia.WhatsAppOptIn.Source != lead.ConsentImport || lia.WhatsAppOptIn.Purpose != "novidades da escola" {
		t.Fatalf("lia = %+v", lia)
	}
	record := lia.ConsentRecord
	if record == nil || record.Evidence != (lead.ConsentEvidence{Type: lead.EvidenceImportLine, Ref: job.ID + ":2"}) ||
		record.Actor != (lead.ConsentActor{Type: lead.ConsentActorUser, ID: impUser}) {
		t.Fatalf("the imported grant must cite its job, line and importer: %+v", record)
	}
}
