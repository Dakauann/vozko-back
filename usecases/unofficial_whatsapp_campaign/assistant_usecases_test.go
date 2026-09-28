package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/media"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type instancesByID map[string]*uw.Instance

func (m instancesByID) Instance(_ context.Context, id string) (*uw.Instance, error) {
	if i, ok := m[id]; ok {
		return i, nil
	}
	return nil, uw.ErrInstanceNotFound
}

func department(id string) *string { return &id }

var assistantInstances = instancesByID{
	"i-ok":      {ID: "i-ok", WorkspaceID: "ws1", DepartmentID: department("d-sales"), Status: uw.StatusConnected},
	"i-banned":  {ID: "i-banned", WorkspaceID: "ws1", DepartmentID: department("d-sales"), Status: uw.StatusBanned},
	"i-support": {ID: "i-support", WorkspaceID: "ws1", DepartmentID: department("d-support"), Status: uw.StatusConnected},
	"i-foreign": {ID: "i-foreign", WorkspaceID: "ws2", Status: uw.StatusConnected},
}

var salesScope = uw.DepartmentScope{DepartmentIDs: []string{"d-sales"}, Restrict: true}

func TestUsableInstanceIsVisibleAndAbleToCampaign(t *testing.T) {
	uc := NewCampaignInstanceUseCase(assistantInstances)
	if i, err := uc.Usable(context.Background(), "ws1", salesScope, "i-ok"); err != nil || i.ID != "i-ok" {
		t.Fatalf("usable number refused: %v", err)
	}
	for _, id := range []string{"i-support", "i-foreign", "nope"} {
		if _, err := uc.Usable(context.Background(), "ws1", salesScope, id); !errors.Is(err, uw.ErrInstanceNotFound) {
			t.Fatalf("%s: %v", id, err)
		}
	}
	var unusable *uwc.InstanceUnusableError
	if _, err := uc.Usable(context.Background(), "ws1", salesScope, "i-banned"); !errors.As(err, &unusable) {
		t.Fatalf("banned number: %v", err)
	}
}

type dispatchRecorder struct {
	inputs []uwc.DispatchCampaignInput
	err    error
}

func (d *dispatchRecorder) Dispatch(_ context.Context, in uwc.DispatchCampaignInput) error {
	d.inputs = append(d.inputs, in)
	return d.err
}

func TestCampaignActionOnlyActsOnAnOwnedCampaign(t *testing.T) {
	dispatch := &dispatchRecorder{}
	uc := NewCampaignActionUseCase(NewCampaignAccessUseCase(accessCampaigns), dispatch)
	if _, err := uc.Act(context.Background(), "ws1", salesScope, "c-team", campaign.ActionStart); !errors.Is(err, uwc.ErrCampaignNotFound) {
		t.Fatalf("other department: %v", err)
	}
	if len(dispatch.inputs) != 0 {
		t.Fatalf("dispatched %+v", dispatch.inputs)
	}
	c, err := uc.Act(context.Background(), "ws1", salesScope, "c-mine", campaign.ActionStart)
	if err != nil || c.ID != "c-mine" || len(dispatch.inputs) != 1 || dispatch.inputs[0].Action != campaign.ActionStart {
		t.Fatalf("own campaign: %v %+v", err, dispatch.inputs)
	}
	dispatch.err = uw.ErrRestrictedByWA
	if _, err := uc.Act(context.Background(), "ws1", salesScope, "c-mine", campaign.ActionStart); !errors.Is(err, uw.ErrRestrictedByWA) {
		t.Fatalf("dispatch refusal was swallowed: %v", err)
	}
}

type sheetFiles map[string]string

func (f sheetFiles) Read(_ context.Context, workspaceID, mediaID string) (*media.Content, error) {
	body, ok := f[workspaceID+"|"+mediaID]
	if !ok {
		return nil, media.ErrMediaNotFound
	}
	return &media.Content{Name: "lista.csv", Data: []byte(body)}, nil
}

func textMessage(body string) uwc.MessageSpec {
	return uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{body}}
}

func TestImportPreviewUsesTheMessageVariablesAndTheTargetRule(t *testing.T) {
	uc := NewImportPreviewUseCase(sheetFiles{"ws1|m1": "numero;nome;var1\n+55 84 99440-9624;Maria;BF10\n123;Ana;BF10\n5584994409624;Maria;BF10\n"})
	p, err := uc.Preview(context.Background(), uwc.ImportRequest{WorkspaceID: "ws1", MediaID: "m1", Message: textMessage("Oi {{1}}!")})
	if err != nil {
		t.Fatal(err)
	}
	if p.Variables != 1 || p.ValidRows != 1 || p.Rows[0].Number != "5584994409624" ||
		p.IssueCounts[campaign.IssueInvalidNumber] != 1 || p.IssueCounts[campaign.IssueDuplicate] != 1 {
		t.Fatalf("preview = %+v", p)
	}
}

func TestImportPreviewRefusesABadMessageOrAForeignFile(t *testing.T) {
	uc := NewImportPreviewUseCase(sheetFiles{"ws1|m1": "numero\n5584994409624\n"})
	if _, err := uc.Preview(context.Background(), uwc.ImportRequest{WorkspaceID: "ws1", MediaID: "m1", Message: textMessage("")}); !errors.Is(err, uwc.ErrMessageBodyRequired) {
		t.Fatalf("empty message: %v", err)
	}
	if _, err := uc.Preview(context.Background(), uwc.ImportRequest{WorkspaceID: "ws2", MediaID: "m1", Message: textMessage("Oi")}); !errors.Is(err, media.ErrMediaNotFound) {
		t.Fatalf("foreign file: %v", err)
	}
}
