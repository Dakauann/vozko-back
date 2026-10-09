package leadaction_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/calls/calllist"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
	calllist_usecase "vozko/usecases/calls/calllist"
)

type fakeCallLists struct {
	prepared   []calllist.Draft
	created    []calllist.Draft
	selected   []int
	lists      map[string]*calllist.List
	prepareErr error
	createErr  error
}

func newFakeCallLists() *fakeCallLists {
	return &fakeCallLists{lists: map[string]*calllist.List{}}
}

var fakeListVerdict = calllist.ListVerdict{StatusMoves: []calllist.Status{}}

func (f *fakeCallLists) View(_ context.Context, _ Actor, id string) (calllist_usecase.ListView, error) {
	l, ok := f.lists[id]
	if !ok {
		return calllist_usecase.ListView{}, calllist.ErrListNotFound
	}
	return calllist_usecase.ListView{List: l, Verdict: fakeListVerdict}, nil
}

func (f *fakeCallLists) Prepare(_ context.Context, a Actor, d calllist.Draft) (calllist_usecase.Prepared, error) {
	d.WorkspaceID, d.CreatedBy = a.WorkspaceID, a.UserID
	f.prepared = append(f.prepared, d)
	if f.prepareErr != nil {
		return calllist_usecase.Prepared{}, f.prepareErr
	}
	return calllist_usecase.Prepared{}, nil
}

func (f *fakeCallLists) CreateFromSnapshot(_ context.Context, _ Actor, _ calllist_usecase.Prepared, selected int) (calllist_usecase.ListView, error) {
	if f.createErr != nil {
		return calllist_usecase.ListView{}, f.createErr
	}
	d := f.prepared[len(f.prepared)-1]
	f.created = append(f.created, d)
	f.selected = append(f.selected, selected)
	l := &calllist.List{ID: d.ID, WorkspaceID: d.WorkspaceID, Name: d.Name, Status: calllist.StatusBuilding, Selected: selected}
	f.lists[d.ID] = l
	return calllist_usecase.ListView{List: l, Verdict: fakeListVerdict}, nil
}

func callListRequest(key string, expected int) Request {
	return Request{
		Actor: Actor{WorkspaceID: workspaceID, UserID: manager}, Action: leadaction.ActionCallList, IdempotencyKey: key,
		Params: leadaction.Params{CallList: &leadaction.CallListParams{
			Name: "Retorno", AssigneeIDs: []string{"worker-1"}, PhoneSource: "contact", PhoneLabel: "landline",
		}},
		Selection: matchingAll(expected),
	}
}

func TestTheCallListActionFreezesTheSelectionUnderTheListIDAndReturnsTheList(t *testing.T) {
	h := newHarness(4)
	out, err := h.svc.Start(context.Background(), callListRequest("call-list-1", 4))
	if err != nil {
		t.Fatal(err)
	}
	if out.CallList == nil || out.CallList.Status != calllist.StatusBuilding || out.CallList.Selected != 4 {
		t.Fatalf("outcome = %+v", out)
	}
	if out.CallList.Verdict.StatusMoves == nil {
		t.Fatalf("the outcome lost the verdict of the call lists service: %+v", out.CallList.Verdict)
	}
	if len(h.callLists.created) != 1 {
		t.Fatalf("created %d lists", len(h.callLists.created))
	}
	d := h.callLists.created[0]
	if d.ID != out.CallList.ID || d.Name != "Retorno" ||
		d.Phone != (calllist.PhoneChoice{Source: calllist.PhoneContact, Label: lead.PhoneLandline}) || d.AssigneeIDs[0] != "worker-1" {
		t.Fatalf("draft = %+v", d)
	}
	if size, _ := h.selections.SnapshotSize(context.Background(), workspaceID, d.ID); size != 4 {
		t.Fatalf("the frozen set under the list id holds %d leads", size)
	}

	again, err := h.svc.Start(context.Background(), callListRequest("call-list-1", 4))
	if err != nil || again.CallList == nil || again.CallList.ID != out.CallList.ID || again.CallList.Verdict.StatusMoves == nil ||
		len(h.callLists.created) != 1 || h.selections.freezes != 1 {
		t.Fatalf("a retried request = %+v, %v, created %d, froze %d", again, err, len(h.callLists.created), h.selections.freezes)
	}
}

func TestTheCallListActionNeedsTheManageCapabilityAndCallListParams(t *testing.T) {
	h := newHarness(4)
	req := callListRequest("call-list-2", 4)
	req.Actor.UserID = seller
	if _, err := h.svc.Start(context.Background(), req); !errors.Is(err, leadaction.ErrForbidden) {
		t.Fatalf("a seller = %v", err)
	}
	req = callListRequest("call-list-3", 4)
	req.Params.CallList = nil
	if _, err := h.svc.Start(context.Background(), req); !errors.Is(err, leadaction.ErrCallListRequired) {
		t.Fatalf("no call list params = %v", err)
	}
	if h.selections.freezes != 0 || len(h.callLists.created) != 0 {
		t.Fatal("a refused call list froze or created something")
	}
}

func TestACallListRefusedByItsRulesIsRefusedBeforeFreezing(t *testing.T) {
	h := newHarness(4)
	h.callLists.prepareErr = calllist.ErrAssigneesRequired
	if _, err := h.svc.Start(context.Background(), callListRequest("call-list-4", 4)); !errors.Is(err, calllist.ErrAssigneesRequired) {
		t.Fatalf("Start = %v", err)
	}
	if h.selections.freezes != 0 {
		t.Fatal("the selection was frozen for a list its rules refuse")
	}
}

func TestACallListOverTheItemCapIsRefusedBeforeFreezing(t *testing.T) {
	h := newHarness(3)
	h.selections.matched = calllist.MaxItems + 1
	if _, err := h.svc.Start(context.Background(), callListRequest("call-list-5", calllist.MaxItems+1)); !errors.Is(err, calllist.ErrSelectionTooLarge) {
		t.Fatalf("Start = %v", err)
	}
	if h.selections.freezes != 0 {
		t.Fatal("an oversized call list froze its selection")
	}
}

func TestAFailedCallListCreationDropsItsFrozenSet(t *testing.T) {
	h := newHarness(4)
	h.callLists.createErr = errors.New("boom")
	if _, err := h.svc.Start(context.Background(), callListRequest("call-list-6", 4)); err == nil {
		t.Fatal("a failed creation was reported as created")
	}
	for id, ids := range h.selections.snapshots {
		if len(ids) > 0 {
			t.Fatalf("snapshot %s kept %d leads", id, len(ids))
		}
	}
}

func TestTheCallListPreviewSurfacesTheListRulesBeforeAnythingIsFrozen(t *testing.T) {
	h := newHarness(4)
	h.callLists.prepareErr = calllist.ErrAssigneeCannotWork
	if _, err := h.svc.Preview(context.Background(), callListRequest("", 4)); !errors.Is(err, calllist.ErrAssigneeCannotWork) {
		t.Fatalf("Preview = %v", err)
	}
	h.callLists.prepareErr = nil
	p, err := h.svc.Preview(context.Background(), callListRequest("", 4))
	if err != nil || p.Result.Selected != 4 || h.selections.freezes != 0 {
		t.Fatalf("preview = %+v, %v, froze %d", p, err, h.selections.freezes)
	}
}

func TestTheCallListActionPreparesTheListOnce(t *testing.T) {
	h := newHarness(4)
	if _, err := h.svc.Start(context.Background(), callListRequest("call-list-7", 4)); err != nil {
		t.Fatal(err)
	}
	if len(h.callLists.prepared) != 1 {
		t.Fatalf("prepared %d times, want once per request", len(h.callLists.prepared))
	}
}

func TestAReusedIdempotencyKeyWithAnotherCallListIsRefused(t *testing.T) {
	h := newHarness(4)
	if _, err := h.svc.Start(context.Background(), callListRequest("call-list-8", 4)); err != nil {
		t.Fatal(err)
	}
	other := callListRequest("call-list-8", 4)
	other.Params.CallList.Name = "Outra lista"
	if _, err := h.svc.Start(context.Background(), other); !errors.Is(err, leadaction.ErrIdempotencyKeyReused) {
		t.Fatalf("Start with another list under the same key = %v, want ErrIdempotencyKeyReused", err)
	}
	if len(h.callLists.created) != 1 || h.selections.freezes != 1 {
		t.Fatalf("created %d, froze %d, want the second request refused before anything", len(h.callLists.created), h.selections.freezes)
	}
}
