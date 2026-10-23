package node_executors

import (
	"context"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type headerButtonsClient struct {
	conversation.WhatsAppClient
	sent []conversation.SendButtonMessageInput
}

func (c *headerButtonsClient) SendButtonMessage(_ context.Context, in conversation.SendButtonMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.sent = append(c.sent, in)
	return &conversation.SendTextMessageOutput{MessageID: "wamid.header"}, nil
}

func promptWithHeaderMedia(t *testing.T, resolver *conversation.MediaURLResolver, url string) (*workflow.NodeResult, error, *headerButtonsClient) {
	t.Helper()
	client := &headerButtonsClient{}
	sender := newMediaSender(client, &recordingHistory{}, nil)
	sender.deps.MediaURLs = resolver
	executor := &interactivePromptExecutor{sender: sender, channel: &channelSender{whatsapp: sender}}
	res, err := executor.Execute(customerCtx(workflow.NodeTypeActionSendInteractive, map[string]interface{}{
		"body":             "Qual plano?",
		"buttons":          `[{"Type":"reply","ID":"pro","Title":"Pro"}]`,
		"header_type":      string(conversation.HeaderTypeImage),
		"header_media_url": url,
	}, waMediaRun()))
	return res, err, client
}

func TestAPromptHeaderSendsOurURLOnlyWhenItIsSendableHere(t *testing.T) {
	run := waMediaRun()
	library := ourStorage + "ws-1/capa.png"
	note := ourStorage + "conv/nota.jpg"
	resolver := &conversation.MediaURLResolver{
		Storage: ourStorageHosts{},
		Library: urlLibrary{library: {ID: "lib-1", WorkspaceID: run.WorkspaceID, URL: library, Type: media.MediaTypeProductImage}},
		Uploads: urlUploads{note: {ID: "up-1", EntryID: run.EntryID, EntryType: shared.EntryTypeWhatsApp, Type: conversation.MediaTypeImage, URL: note, Purpose: conversation.MediaPurposeNote}},
	}

	for name, url := range map[string]string{"library item": library, "external file": "https://images.example.com/a.png"} {
		t.Run(name, func(t *testing.T) {
			res, err, client := promptWithHeaderMedia(t, resolver, url)
			if err != nil || res.Output["sent"] != true || len(client.sent) != 1 || client.sent[0].MediaLink != url {
				t.Fatalf("err %v output %+v sent %+v, want the header %s delivered", err, res, client.sent, url)
			}
		})
	}
	for name, url := range map[string]string{"note image": note, "file with no row": ourStorage + "x.png", "empty link": ""} {
		t.Run(name, func(t *testing.T) {
			_, err, client := promptWithHeaderMedia(t, resolver, url)
			if err == nil || len(client.sent) != 0 {
				t.Fatalf("err %v sent %+v, want the prompt refused", err, client.sent)
			}
		})
	}
}

func TestAPromptHeaderWithoutTheURLResolverSendsNothing(t *testing.T) {
	_, err, client := promptWithHeaderMedia(t, nil, "https://images.example.com/a.png")
	if err == nil || len(client.sent) != 0 {
		t.Fatalf("err %v sent %+v, want an unchecked header URL to send nothing", err, client.sent)
	}
}
