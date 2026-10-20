package crmbulk

import (
	"net/http"
	"testing"

	crmbulk_usecase "vozko/usecases/crmbulk"
)

func TestBulkForwardsTheCloseOutcome(t *testing.T) {
	svc := &stubService{}
	rec := call(t, NewCRMBulkHandler(svc).Bulk, `{"action":"close","outcomeCode":"sold","targets":[{"entryId":"e1","entryType":"telegram"}]}`)
	if rec.Code != http.StatusOK || svc.got.Action != crmbulk_usecase.ActionClose || svc.got.OutcomeCode != "sold" {
		t.Fatalf("status %d, input %+v", rec.Code, svc.got)
	}
}

func TestBulkUnavailableActionIsAServiceError(t *testing.T) {
	svc := &stubService{err: crmbulk_usecase.ErrActionUnavailable}
	rec := call(t, NewCRMBulkHandler(svc).Bulk, `{"action":"close","targets":[{"entryId":"e1","entryType":"telegram"}]}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
}
