package oppboard_usecase

import (
	"errors"
	"testing"

	"vozko/domain/savedview"
)

func TestBoardAndListRefuseWithoutAnAuthorizer(t *testing.T) {
	searcher := &fakeSearcher{}
	svc := NewService(searcher, &fakeStages{}, nil, opportunityFields())

	if _, err := svc.GetBoard(BoardInput{WorkspaceID: "ws1", UserID: "u1", GroupBy: savedview.GroupByStage}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("board: want ErrUnauthorized, got %v", err)
	}
	if _, _, err := svc.GetList(ListInput{WorkspaceID: "ws1", UserID: "u1"}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("list: want ErrUnauthorized, got %v", err)
	}
	if len(searcher.searchInputs)+len(searcher.sumInputs) != 0 {
		t.Fatal("nothing may be searched without an authorizer")
	}
}
