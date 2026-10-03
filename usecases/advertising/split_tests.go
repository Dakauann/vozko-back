package advertising

import (
	"context"
	"strconv"

	ads "vozko/domain/advertising"
)

type testGateway interface {
	CreateSplitTest(ctx context.Context, token, metaAccountID string, test ads.SplitTest) (string, error)
	ListSplitTests(ctx context.Context, token, metaAccountID string) ([]ads.SplitTest, error)
}

type SplitTestUseCase struct {
	access  accountAccess
	gateway testGateway
	objects ads.ObjectRepository
}

func NewSplitTestUseCase(sync *SyncUseCase, gateway testGateway) *SplitTestUseCase {
	return &SplitTestUseCase{access: sync.access, gateway: gateway, objects: sync.objects}
}

var testLevels = map[ads.TestLevel]ads.Level{ads.TestCampaigns: ads.LevelCampaign, ads.TestAdSets: ads.LevelAdSet}

func (uc *SplitTestUseCase) List(ctx context.Context, workspaceID, accountID string) ([]ads.SplitTest, error) {
	account, token, err := uc.access.open(ctx, workspaceID, accountID, ads.UseRead)
	if err != nil {
		return nil, err
	}
	tests, err := uc.gateway.ListSplitTests(ctx, token, account.MetaAccountID)
	if err != nil {
		return nil, uc.access.failed(ctx, account, err)
	}
	for i := range tests {
		tests[i].AdAccountID = account.ID
	}
	return tests, nil
}

func (uc *SplitTestUseCase) prepare(ctx context.Context, workspaceID string, test *ads.SplitTest) (*ads.AdAccount, string, error) {
	test.Normalize()
	for i := range test.Cells {
		if err := uc.cellObjects(ctx, workspaceID, test, i); err != nil {
			return nil, "", err
		}
	}
	if err := test.Validate(uc.access.now()); err != nil {
		return nil, "", err
	}
	return uc.access.open(ctx, workspaceID, test.AdAccountID, ads.UseWrite)
}

func (uc *SplitTestUseCase) cellObjects(ctx context.Context, workspaceID string, test *ads.SplitTest, i int) error {
	cell := &test.Cells[i]
	for _, id := range cell.ObjectIDs {
		o, err := uc.objects.Find(ctx, workspaceID, id)
		if err != nil || o.AdAccountID != test.AdAccountID || o.Level != testLevels[test.Level] || o.Locked() {
			return ads.FieldError("cells["+strconv.Itoa(i)+"].objectIds", "not_available")
		}
		if cell.Name == "" {
			cell.Name = o.Name
		}
	}
	return nil
}

func (uc *SplitTestUseCase) Check(ctx context.Context, workspaceID string, test ads.SplitTest) (ads.SplitTest, error) {
	if _, _, err := uc.prepare(ctx, workspaceID, &test); err != nil {
		return ads.SplitTest{}, err
	}
	return test, nil
}

func (uc *SplitTestUseCase) Create(ctx context.Context, workspaceID string, test ads.SplitTest) (string, error) {
	account, token, err := uc.prepare(ctx, workspaceID, &test)
	if err != nil {
		return "", err
	}
	id, err := uc.gateway.CreateSplitTest(ctx, token, account.MetaAccountID, test)
	if err != nil {
		return "", uc.access.failed(ctx, account, err)
	}
	return id, nil
}
