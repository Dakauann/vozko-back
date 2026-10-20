package workflow_usecase

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	wf "vozko/domain/whatsappform"
	"vozko/domain/workflow"
	whatsappform_usecase "vozko/usecases/whatsappform"
	"vozko/usecases/workflow/node_executors"
)

const simFormMessageType = "form"

var _ node_executors.FormSender = (*simFormSender)(nil)

type simFormSender struct {
	channel *simChannel
}

func newSimFormSender(channel *simChannel) *simFormSender {
	return &simFormSender{channel: channel}
}

func (s *simFormSender) SendFromWorkflow(ctx context.Context, in whatsappform_usecase.WorkflowSend) (wf.Send, error) {
	if err := in.Copy.Validate(); err != nil {
		return wf.Send{}, err
	}
	id := s.channel.sent(simChannelSend{
		msgType: simFormMessageType, text: in.Copy.Body,
		extra: map[string]string{"formId": in.FormID, "header": in.Copy.Header, "footer": in.Copy.Footer, "cta": in.Copy.CTA},
	})
	send := wf.Send{ID: id, MessageID: id, FormID: in.FormID, WorkflowRunID: in.RunID, Status: wf.SendSent}
	var card conversation.Message
	if err := card.AttachMetadata(wf.MetadataKey, wf.NewMessageCard(wf.Form{ID: in.FormID}, send, in.Copy)); err != nil {
		return wf.Send{}, err
	}
	if err := s.channel.Record(ctx, conversation.MessageHistoryRecord{
		SentBy: conversation.SentBySystem(), EntryID: in.EntryID, EntryType: shared.EntryTypeWhatsApp, Channel: conversation.MessageChannelWhatsApp,
		MessageType: conversation.MessageTypeAIResponse, MessageID: id, To: simContactNumber, Text: in.Copy.Body, Metadata: card.Metadata,
	}); err != nil {
		return wf.Send{}, err
	}
	return send, nil
}

type simFormReply struct {
	Answers map[string]interface{} `json:"answers"`
	Outcome string                 `json:"outcome"`
}

func (r simFormReply) answered() bool {
	return r.Outcome != workflow.FormHandleNoReply
}

func (r simFormReply) runData() map[string]interface{} {
	data := map[string]interface{}{workflow.DataKeyFormResponseID: "sim-response-" + uuid.New().String()}
	for key, value := range r.Answers {
		if key = strings.TrimSpace(key); key != "" {
			data["form."+key] = value
		}
	}
	return data
}

func simTimeoutSeconds(node *workflow.Node) int {
	return int(node_executors.PromptTimeoutSeconds(node.Config))
}

type waitingFormPayload struct {
	NodeID         string `json:"nodeId"`
	FormID         string `json:"formId"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}
