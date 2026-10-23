package node_executors

import (
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

const ourStorage = "https://cdn.vozko.test/"

type ourStorageHosts struct{}

func (ourStorageHosts) OwnsURL(rawURL string) bool { return strings.HasPrefix(rawURL, ourStorage) }

type urlLibrary map[string]*media.Media

func (l urlLibrary) GetMediaForWorkspaceByURL(workspaceID, url string) (*media.Media, error) {
	row, ok := l[url]
	if !ok || row.WorkspaceID != workspaceID {
		return nil, media.ErrMediaNotFound
	}
	return row, nil
}

type urlUploads map[string]*conversation.ConversationMedia

func (u urlUploads) GetInEntryByURL(entryID string, entryType shared.EntryType, url string) (*conversation.ConversationMedia, error) {
	row, ok := u[url]
	if !ok || row.EntryID != entryID || row.EntryType != entryType {
		return nil, conversation.ErrMediaNotFound
	}
	return row, nil
}

func externalOnlyURLs() *conversation.MediaURLResolver {
	return &conversation.MediaURLResolver{Storage: ourStorageHosts{}, Library: urlLibrary{}, Uploads: urlUploads{}}
}

func sendMediaByURL(t *testing.T, resolver *conversation.MediaURLResolver, url string) (*workflow.NodeResult, *fakeAdapter) {
	t.Helper()
	adapter := &fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}
	executor := &sendMediaExecutor{sender: &whatsappSender{deps: SenderDeps{MediaURLs: resolver}}, channel: instagramSender(adapter)}
	res, err := executor.Execute(customerCtx(workflow.NodeTypeActionSendMedia, map[string]interface{}{"media_url": url}, igRun()))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	return res, adapter
}

func TestAWorkflowSendsOurURLOnlyWhenItIsSendableHere(t *testing.T) {
	library := ourStorage + "ws-1/catalogo.png"
	upload := ourStorage + "conv-1/foto.jpg"
	note := ourStorage + "conv-1/nota.jpg"
	foreign := ourStorage + "ws-2/segredo.png"
	resolver := &conversation.MediaURLResolver{
		Storage: ourStorageHosts{},
		Library: urlLibrary{
			library: {ID: "lib-1", WorkspaceID: "ws-1", URL: library, Type: media.MediaTypeProductImage},
			foreign: {ID: "lib-2", WorkspaceID: "ws-2", URL: foreign, Type: media.MediaTypeProductImage},
		},
		Uploads: urlUploads{
			upload: {ID: "up-1", EntryID: "conv-1", EntryType: shared.EntryTypeInstagram, Type: conversation.MediaTypeImage, URL: upload, Purpose: conversation.MediaPurposeMessage},
			note:   {ID: "up-2", EntryID: "conv-1", EntryType: shared.EntryTypeInstagram, Type: conversation.MediaTypeImage, URL: note, Purpose: conversation.MediaPurposeNote},
		},
	}

	for name, url := range map[string]string{"library item": library, "upload of this conversation": upload, "external file": "https://images.example.com/a.png"} {
		t.Run(name, func(t *testing.T) {
			res, adapter := sendMediaByURL(t, resolver, url)
			if res.Output["sent"] != true || adapter.sentMedia.URL != url {
				t.Fatalf("output %+v sent %+v, want %s delivered", res.Output, adapter.sentMedia, url)
			}
		})
	}
	for name, url := range map[string]string{"note image": note, "another workspace's file": foreign, "file with no row": ourStorage + "x.png"} {
		t.Run(name, func(t *testing.T) {
			res, adapter := sendMediaByURL(t, resolver, url)
			if res.Output["sent"] != false || adapter.sendCalls != 0 {
				t.Fatalf("output %+v calls %d, want nothing delivered", res.Output, adapter.sendCalls)
			}
		})
	}
}

func TestAWorkflowWithoutTheURLResolverSendsNoURL(t *testing.T) {
	res, adapter := sendMediaByURL(t, nil, "https://images.example.com/a.png")
	if res.Output["sent"] != false || adapter.sendCalls != 0 {
		t.Fatalf("output %+v, want an unchecked URL to send nothing", res.Output)
	}
}
