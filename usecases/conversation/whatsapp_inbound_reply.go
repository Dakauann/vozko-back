package conversation_usecase

import (
	"context"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/usecases/agentctx"
)

type whatsAppInboundTurn struct {
	agentCtx        *agentContext
	leadRecord      *lead.Lead
	message         *conversation.WhatsAppMessage
	entryID         string
	entryType       shared.EntryType
	conversationID  string
	businessNumber  string
	receivedPhoneID string
}

func withAgentTurn(ctx context.Context, agentCtx *agentContext) context.Context {
	if agentCtx == nil || agentCtx.agent == nil {
		return ctx
	}
	ctx = agentctx.WithAgent(ctx, agentCtx.agent)
	return agentctx.WithToolExecutionTracker(ctx, agentctx.NewToolExecutionTracker())
}

func replyPhoneID(agentCtx *agentContext, receivedPhoneID string) string {
	if agentCtx != nil && agentCtx.wcCampaign != nil && agentCtx.wcCampaign.BusinessPhoneID != "" {
		return agentCtx.wcCampaign.BusinessPhoneID
	}
	return receivedPhoneID
}

func (t whatsAppInboundTurn) context() WhatsAppContext {
	wctx := WhatsAppContext{
		UserPhoneNumber:     t.message.From,
		ConversationID:      t.conversationID,
		MessageReceivedTime: parseWhatsAppTimestamp(t.message.Timestamp),
	}
	if a := t.agentCtx; a != nil {
		if a.wcCampaign != nil {
			wctx.CampaignName = a.wcCampaign.Name
		}
		if a.agent != nil {
			wctx.AgentName = a.agent.Name
		}
		if a.wcLeadRecord != nil {
			wctx.UserName = a.wcLeadRecord.RealName()
		}
		if a.wcEntry != nil {
			wctx.Metadata = a.wcEntry.Metadata
		}
	}
	if t.leadRecord != nil {
		wctx.UserName = t.leadRecord.RealName()
	}
	return wctx
}

func (t whatsAppInboundTurn) reply(query, latest string) whatsAppAgentReply {
	return whatsAppAgentReply{
		agentCtx:          t.agentCtx,
		whatsappCtx:       t.context(),
		entryID:           t.entryID,
		entryType:         t.entryType,
		conversationID:    t.conversationID,
		recipient:         strings.TrimSpace(t.message.From),
		businessNumber:    t.businessNumber,
		outboundPhoneID:   replyPhoneID(t.agentCtx, t.receivedPhoneID),
		receivedPhoneID:   t.receivedPhoneID,
		incomingMessageID: strings.TrimSpace(t.message.ID),
		query:             query,
		latest:            latest,
	}
}
