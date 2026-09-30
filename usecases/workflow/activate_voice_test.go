package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/media"
	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

type inboundTrunksStub map[string]bool

func (s inboundTrunksStub) ReceivesCalls(workspaceID, trunkID string) (bool, error) {
	return s[workspaceID+"|"+trunkID], nil
}

type audioMediaStub struct {
	media.MediaRepository
	items map[string]*media.Media
}

func (s audioMediaStub) GetMediaByID(id string) (*media.Media, error) {
	if m, ok := s.items[id]; ok {
		return m, nil
	}
	return nil, errors.New("not found")
}

func voiceCatalog() []workflow.NodeDefinition {
	return append(workflow.BuiltinDefinitions(),
		node_executors.NewPlayAudioExecutor(nil).Definition(),
		node_executors.NewWaitDTMFExecutor().Definition(),
	)
}

func voiceWorkflow(id, trunkID, mediaID string) *workflow.Workflow {
	return &workflow.Workflow{
		ID:          id,
		WorkspaceID: "ws1",
		Name:        "URA",
		Status:      workflow.WorkflowStatusDraft,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "t", Type: workflow.NodeTypeTriggerCallReceived, Config: map[string]interface{}{"trunk_id": trunkID}},
				{ID: "menu", Type: workflow.NodeTypeActionPlayAudio, Config: map[string]interface{}{"media_id": mediaID}},
				{ID: "keys", Type: workflow.NodeTypeWaitDTMF, Config: map[string]interface{}{"keys": []interface{}{"1"}}},
				{ID: "end", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{
				{Source: "t", Target: "menu"},
				{Source: "menu", Target: "keys"},
				{Source: "keys", Target: "end", Label: "1"},
				{Source: "keys", Target: "menu", Label: "invalid"},
			},
		},
	}
}

func voiceActivation(repo *MockWorkflowRepository, trunks InboundTrunkLookup) *activateWorkflowUseCase {
	uc := NewActivateWorkflowUseCase(repo).(*activateWorkflowUseCase)
	uc.SetCatalogFn(voiceCatalog)
	uc.SetMediaRepo(audioMediaStub{items: map[string]*media.Media{
		"greeting": {ID: "greeting", WorkspaceID: "ws1", Type: media.MediaTypeAudio, URL: "https://files/greeting.mp3"},
		"picture":  {ID: "picture", WorkspaceID: "ws1", Type: media.MediaTypeProductImage, URL: "https://files/picture.png"},
	}})
	if trunks != nil {
		uc.SetInboundTrunks(trunks)
	}
	return uc
}

func TestAVoiceWorkflowActivatesOnATrunkOfItsWorkspace(t *testing.T) {
	repo := NewMockWorkflowRepository()
	_ = repo.Create(voiceWorkflow("wf-1", "trunk-1", "greeting"))
	uc := voiceActivation(repo, inboundTrunksStub{"ws1|trunk-1": true})

	got, err := uc.Execute("wf-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.Status != workflow.WorkflowStatusActive || got.Type != workflow.WorkflowTypeVoice {
		t.Fatalf("status=%s type=%s", got.Status, got.Type)
	}
}

func TestAVoiceWorkflowNeedsAUsableTrunk(t *testing.T) {
	cases := []struct {
		name   string
		trunk  string
		trunks InboundTrunkLookup
		want   error
	}{
		{"no trunk chosen", "", inboundTrunksStub{}, workflow.ErrNodeMissingRequiredField},
		{"trunk of another workspace or outbound only", "trunk-x", inboundTrunksStub{"ws2|trunk-x": true}, workflow.ErrVoiceTrunkInvalid},
		{"trunk lookup not wired", "trunk-1", nil, workflow.ErrVoiceTrunkInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := NewMockWorkflowRepository()
			_ = repo.Create(voiceWorkflow("wf-1", tc.trunk, "greeting"))
			if _, err := voiceActivation(repo, tc.trunks).Execute("wf-1"); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestOnlyOneActiveVoiceWorkflowAnswersATrunk(t *testing.T) {
	repo := NewMockWorkflowRepository()
	active := voiceWorkflow("wf-live", "trunk-1", "greeting")
	active.Status = workflow.WorkflowStatusActive
	active.Normalize()
	_ = repo.Create(active)
	_ = repo.Create(voiceWorkflow("wf-new", "trunk-1", "greeting"))
	uc := voiceActivation(repo, inboundTrunksStub{"ws1|trunk-1": true})

	if _, err := uc.Execute("wf-new"); !errors.Is(err, workflow.ErrVoiceTrunkTaken) {
		t.Fatalf("err = %v, want ErrVoiceTrunkTaken", err)
	}

	live, _ := repo.FindByID("wf-live")
	live.Status = workflow.WorkflowStatusPaused
	_ = repo.Update(live)
	if _, err := uc.Execute("wf-live"); err != nil {
		t.Fatalf("reactivating the only workflow on the trunk: %v", err)
	}
}

func TestPlayAudioMustPointAtAnAudioFileOfTheWorkspace(t *testing.T) {
	for mediaID, wantErr := range map[string]bool{"greeting": false, "picture": true, "missing": true} {
		repo := NewMockWorkflowRepository()
		_ = repo.Create(voiceWorkflow("wf-1", "trunk-1", mediaID))
		_, err := voiceActivation(repo, inboundTrunksStub{"ws1|trunk-1": true}).Execute("wf-1")
		if got := errors.Is(err, workflow.ErrNodeInvalidMediaID); got != wantErr {
			t.Errorf("media %q: err = %v, want refused = %v", mediaID, err, wantErr)
		}
	}
}
