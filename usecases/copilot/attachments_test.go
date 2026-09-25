package copilot_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/copilot"
	"vozko/domain/media"
	"vozko/usecases/agentloop"
)

type workspaceFiles map[string]*media.Media

func (w workspaceFiles) GetMedia(workspaceID, mediaID string) (*media.Media, error) {
	m, ok := w[mediaID]
	if !ok || m.WorkspaceID != workspaceID {
		return nil, media.ErrMediaNotFound
	}
	return m, nil
}

var chatFiles = workspaceFiles{
	"m1": {ID: "m1", WorkspaceID: "ws1", URL: "https://files.test/ws1/clientes.csv", Type: media.MediaTypeDocument},
	"m9": {ID: "m9", WorkspaceID: "ws2", URL: "https://files.test/ws2/outros.csv", Type: media.MediaTypeDocument},
}

func attachmentService(prov *scriptAI, th *fakeThreads, ms *fakeMessages) *Service {
	return NewService(agentloop.Engine{AI: prov}, NewRegistry(), &fakeAccess{}, openFunds{}, th, ms, chatFiles, nil, func() string { return "a" })
}

func TestStreamGivesTheModelTheAttachedFiles(t *testing.T) {
	th, ms := &fakeThreads{thread: testThread()}, &fakeMessages{}
	prov := &scriptAI{turns: [][]ai.ToolCall{{}}, texts: []string{"ok"}}
	cc := ownerCtx
	cc.WorkspaceID = "ws1"
	if err := attachmentService(prov, th, ms).Stream(context.Background(), th.thread, copilot.UserMessage{Content: "importe", AttachmentIDs: []string{"m1"}}, cc, func(string, interface{}) {}); err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, m := range prov.inputs[0].Messages {
		if m.Role == ai.RoleUser && strings.Contains(m.Content, "clientes.csv (media_id: m1") {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("the model never saw the attachment: %+v", prov.inputs[0].Messages)
	}
	var stored []copilot.Attachment
	if ms.created[0].Content != "importe" || json.Unmarshal(ms.created[0].Attachments, &stored) != nil || stored[0].MediaID != "m1" {
		t.Fatalf("stored %+v", ms.created[0])
	}
}

func TestStreamRefusesAFileOfAnotherWorkspace(t *testing.T) {
	th, ms := &fakeThreads{thread: testThread()}, &fakeMessages{}
	cc := ownerCtx
	cc.WorkspaceID = "ws1"
	err := attachmentService(&scriptAI{}, th, ms).Stream(context.Background(), th.thread, copilot.UserMessage{Content: "x", AttachmentIDs: []string{"m9"}}, cc, func(string, interface{}) {})
	if !errors.Is(err, copilot.ErrAttachmentNotFound) || len(ms.created) != 0 {
		t.Fatalf("err %v created %d", err, len(ms.created))
	}
}

func TestStreamCapsAttachments(t *testing.T) {
	th, ms := &fakeThreads{thread: testThread()}, &fakeMessages{}
	ids := make([]string, copilot.MaxAttachments+1)
	for i := range ids {
		ids[i] = "m1"
	}
	cc := ownerCtx
	cc.WorkspaceID = "ws1"
	if err := attachmentService(&scriptAI{}, th, ms).Stream(context.Background(), th.thread, copilot.UserMessage{AttachmentIDs: ids}, cc, func(string, interface{}) {}); !errors.Is(err, copilot.ErrTooManyAttachments) {
		t.Fatalf("err = %v", err)
	}
}
