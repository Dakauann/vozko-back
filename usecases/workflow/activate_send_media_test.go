package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/media"
	"vozko/domain/workflow"
)

type workspaceMediaStub struct {
	media.MediaRepository
	items map[string]*media.Media
}

func (s workspaceMediaStub) GetMediaForWorkspace(workspaceID, id string) (*media.Media, error) {
	if m, ok := s.items[id]; ok && m.WorkspaceID == workspaceID {
		return m, nil
	}
	return nil, media.ErrMediaNotFound
}

func TestASendMediaStepMustPointAtSendableMediaOfTheWorkspace(t *testing.T) {
	validator := &mediaValidator{workspaceID: "ws1", repo: workspaceMediaStub{items: map[string]*media.Media{
		"catalog": {ID: "catalog", WorkspaceID: "ws1", Type: media.MediaTypeProductImage, URL: "https://files/catalog.png"},
		"foreign": {ID: "foreign", WorkspaceID: "ws2", Type: media.MediaTypeProductImage, URL: "https://files/foreign.png"},
		"sheet":   {ID: "sheet", WorkspaceID: "ws1", Type: media.MediaTypeLeadImport, URL: "https://files/leads.csv"},
	}}}

	for mediaID, wantRefused := range map[string]bool{"catalog": false, "foreign": true, "sheet": true, "note-image-1": true} {
		node := &workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSendMedia, Config: map[string]interface{}{"media_id": mediaID}}
		err := validator.Validate(node)
		if got := errors.Is(err, workflow.ErrNodeInvalidMediaID); got != wantRefused {
			t.Errorf("media %q: err = %v, want refused = %v", mediaID, err, wantRefused)
		}
	}
}
