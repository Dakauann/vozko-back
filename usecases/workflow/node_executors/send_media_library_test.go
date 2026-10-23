package node_executors

import (
	"testing"

	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type workspaceLibrary struct {
	rows       map[string]*media.Media
	workspaces []string
}

func (l *workspaceLibrary) GetMediaForWorkspace(workspaceID, id string) (*media.Media, error) {
	l.workspaces = append(l.workspaces, workspaceID)
	row, ok := l.rows[id]
	if !ok || row.WorkspaceID != workspaceID {
		return nil, media.ErrMediaNotFound
	}
	return row, nil
}

func libraryOf(rows ...*media.Media) *workspaceLibrary {
	byID := make(map[string]*media.Media, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	return &workspaceLibrary{rows: byID}
}

func sendLibraryMedia(t *testing.T, library *workspaceLibrary, mediaID string) (*workflow.NodeResult, *fakeAdapter) {
	t.Helper()
	adapter := &fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}
	executor := &sendMediaExecutor{sender: &whatsappSender{deps: SenderDeps{MediaRepo: library}}, channel: instagramSender(adapter)}
	res, err := executor.Execute(customerCtx(workflow.NodeTypeActionSendMedia, map[string]interface{}{"media_id": mediaID}, igRun()))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	return res, adapter
}

func TestAWorkflowSendsLibraryMediaOfItsOwnWorkspace(t *testing.T) {
	library := libraryOf(&media.Media{ID: "lib-1", WorkspaceID: "ws-1", URL: "https://cdn/catalogo.png", Type: media.MediaTypeProductImage})

	res, adapter := sendLibraryMedia(t, library, "lib-1")

	if res.Output["sent"] != true || adapter.sentMedia.URL != "https://cdn/catalogo.png" {
		t.Fatalf("output %+v sent %+v, want the library file delivered", res.Output, adapter.sentMedia)
	}
	if len(library.workspaces) != 1 || library.workspaces[0] != "ws-1" {
		t.Fatalf("library read in %v, want the run's workspace", library.workspaces)
	}
}

func TestAWorkflowRefusesMediaItCannotSend(t *testing.T) {
	cases := map[string]struct {
		library *workspaceLibrary
		id      string
	}{
		"another workspace": {libraryOf(&media.Media{ID: "lib-1", WorkspaceID: "ws-2", URL: "https://cdn/a.png", Type: media.MediaTypeProductImage}), "lib-1"},
		"note image id":     {libraryOf(), "note-image-1"},
		"lead import sheet": {libraryOf(&media.Media{ID: "lib-1", WorkspaceID: "ws-1", URL: "https://cdn/l.csv", Type: media.MediaTypeLeadImport}), "lib-1"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			res, adapter := sendLibraryMedia(t, c.library, c.id)
			if res.Output["sent"] != false || adapter.sendCalls != 0 || adapter.sentMedia.URL != "" {
				t.Fatalf("output %+v sent %+v, want nothing delivered", res.Output, adapter.sentMedia)
			}
		})
	}
}

func TestAWorkflowWithoutALibrarySendsNothing(t *testing.T) {
	adapter := &fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}
	executor := &sendMediaExecutor{sender: &whatsappSender{}, channel: instagramSender(adapter)}

	res, err := executor.Execute(customerCtx(workflow.NodeTypeActionSendMedia, map[string]interface{}{"media_id": "lib-1"}, igRun()))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Output["sent"] != false || adapter.sentMedia.URL != "" {
		t.Fatalf("output %+v, want an unresolvable library to send nothing and say so", res.Output)
	}
}
