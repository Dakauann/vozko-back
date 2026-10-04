package webchat

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/actor"
	"vozko/domain/channel"
	"vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/lead"
	"vozko/domain/shared"
	wcdomain "vozko/domain/webchat"
	"vozko/domain/workflow"
	conversation_usecase "vozko/usecases/conversation"
)

const (
	inboundKeyPrefix      = "webchat:inbound:"
	inboundDedupTTL       = 24 * time.Hour
	automationTimeout     = 3 * time.Minute
	defaultHistoryPage    = 50
	maxHistoryPage        = 100
	selectionKindWebchat  = "webchat_button"
	attachmentKeyTemplate = "conversations/webchat/"
)

func (s *VisitorService) SubmitIntake(ctx context.Context, session *Session, answers wcdomain.IntakeAnswers) (VisitorState, error) {
	w, v := session.Widget, session.Visitor
	now := s.Now()
	intake, err := w.ResolveIntake(answers, v.Verified(), now)
	if err != nil {
		return VisitorState{}, err
	}

	leadID := v.LeadID
	if intake.Phone != "" && leadID == nil {
		found, _, err := s.Leads.FindOrCreate(w.WorkspaceID, intake.Phone, lead.LeadUpdate{Name: intake.Name})
		if err != nil {
			return VisitorState{}, err
		}
		leadID = &found.ID
	}
	if err := s.Visitors.SaveIntake(ctx, v.ID, intake, leadID, now); err != nil {
		return VisitorState{}, err
	}

	updated, err := s.Visitors.FindByID(ctx, v.ID)
	if err != nil {
		return VisitorState{}, err
	}
	if conv, err := s.Conversations.FindByVisitor(ctx, w.ID, v.ID); err == nil {
		s.Operators.BroadcastEntryUpdate(conv.ID, string(shared.EntryTypeWebchat), nil)
	}
	return visitorState(w, updated), nil
}

type SendInput struct {
	ClientMessageID string
	Text            string
	SelectionID     string
	PageURL         string
	IP              string
}

func (s *VisitorService) Send(ctx context.Context, session *Session, in SendInput) (*wcdomain.VisitorMessage, error) {
	w, v := session.Widget, session.Visitor
	if err := s.allowMessage(session); err != nil {
		return nil, err
	}
	if v.MustCompleteIntake(w) {
		return nil, wcdomain.ErrIntakePending
	}
	providerID, err := wcdomain.InboundProviderID(v.ID, in.ClientMessageID)
	if err != nil {
		return nil, err
	}

	conv, err := s.conversationFor(ctx, session, in.PageURL)
	if err != nil {
		return nil, err
	}

	var selection *workflow.OptionSelection
	text := in.Text
	if in.SelectionID != "" {
		option, offered := conv.OfferedOption(in.SelectionID)
		if !offered {
			return nil, wcdomain.ErrSelectionNotOffered
		}
		selection = &workflow.OptionSelection{ID: option.ID, Title: option.Title, Kind: selectionKindWebchat}
		text = option.Title
	}
	if text, err = wcdomain.CleanVisitorText(text); err != nil {
		return nil, err
	}

	fresh, err := s.claimInbound(providerID)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	message := &wcdomain.VisitorMessage{ID: providerID, Author: wcdomain.AuthorVisitor, Text: text, CreatedAt: now}
	if !fresh {
		return message, nil
	}

	if err := s.Transcript.Record(ctx, conversation.MessageHistoryRecord{
		SentBy:            conversation.SentByContact(v.ID),
		EntryID:           conv.ID,
		EntryType:         shared.EntryTypeWebchat,
		Channel:           conversation.MessageChannelWebchat,
		MessageType:       conversation.MessageTypeUserMessage,
		ProviderMessageID: providerID,
		From:              v.ID,
		To:                w.ID,
		Text:              text,
		Timestamp:         now,
		SenderName:        v.DisplayName(),
	}); err != nil {
		s.releaseInbound(providerID)
		return nil, err
	}
	s.afterInbound(ctx, session, conv, message, text, selection)
	return message, nil
}

func (s *VisitorService) allowMessage(session *Session) error {
	if err := allow(s.Limits.MessagesPerVisitor, session.Visitor.ID); err != nil {
		return err
	}
	if err := allow(s.Limits.MessagesPerVisitorDay, session.Visitor.ID); err != nil {
		return err
	}
	return allow(s.Limits.MessagesPerIP, session.IPHash)
}

func (s *VisitorService) claimInbound(providerID string) (bool, error) {
	if s.OneShot == nil {
		return false, wcdomain.ErrRateLimited
	}
	fresh, err := s.OneShot.SetNX(inboundKeyPrefix+providerID, "1", inboundDedupTTL)
	if err != nil {
		return false, err
	}
	return fresh, nil
}

func (s *VisitorService) releaseInbound(providerID string) {
	if err := s.OneShot.Del(inboundKeyPrefix + providerID); err != nil {
		log.Printf("[webchat] release inbound claim %s: %v", providerID, err)
	}
}

func (s *VisitorService) conversationFor(ctx context.Context, session *Session, pageURL string) (*wcdomain.Conversation, error) {
	if len(pageURL) > wcdomain.MaxPageURLBytes {
		pageURL = ""
	}
	return s.Conversations.FindOrCreate(ctx, wcdomain.FindOrCreateConversationInput{
		WorkspaceID: session.Widget.WorkspaceID,
		WidgetID:    session.Widget.ID,
		VisitorID:   session.Visitor.ID,
		PageURL:     pageURL,
	})
}

func (s *VisitorService) afterInbound(
	ctx context.Context,
	session *Session,
	conv *wcdomain.Conversation,
	message *wcdomain.VisitorMessage,
	text string,
	selection *workflow.OptionSelection,
) {
	w, v := session.Widget, session.Visitor
	if len(conv.PendingOptions) > 0 {
		if err := s.Conversations.SetPendingOptions(ctx, conv.ID, nil); err != nil {
			log.Printf("[webchat] clear offered options conversation=%s: %v", conv.ID, err)
		}
	}
	if err := s.Conversations.RecordInbound(ctx, conv.ID, message.CreatedAt); err != nil {
		log.Printf("[webchat] record inbound conversation=%s: %v", conv.ID, err)
	}
	s.Assignments.EnsureAssignment(conv.ID, string(shared.EntryTypeWebchat), w.ID)
	s.publish(ctx, wcdomain.VisitorEvent{VisitorID: v.ID, Kind: wcdomain.EventMessage, Message: message})

	input := conversation_usecase.InboundAutomationInput{
		WorkspaceID:          w.WorkspaceID,
		EntryID:              conv.ID,
		EntryType:            shared.EntryTypeWebchat,
		ContactRef:           v.ID,
		LeadID:               v.LeadID,
		Text:                 text,
		Selection:            selection,
		ConversationOverride: conv.AutomationEnabled,
		Config:               w.Automation(),
	}
	detached := context.WithoutCancel(ctx)
	s.Async(func() {
		runCtx, cancel := context.WithTimeout(detached, automationTimeout)
		defer cancel()
		s.Automation.Dispatch(runCtx, input)
	})
}

func (s *VisitorService) publish(ctx context.Context, event wcdomain.VisitorEvent) {
	if err := s.Events.Publish(ctx, event); err != nil {
		log.Printf("[webchat] publish %s to visitor=%s: %v", event.Kind, event.VisitorID, err)
	}
}

type HistoryInput struct {
	Before *time.Time
	After  *time.Time
	Limit  int
}

type HistoryView struct {
	Messages []wcdomain.VisitorMessage
	Options  []wcdomain.Option
	Human    bool
}

func (s *VisitorService) History(ctx context.Context, session *Session, in HistoryInput) (*HistoryView, error) {
	conv, err := s.Conversations.FindByVisitor(ctx, session.Widget.ID, session.Visitor.ID)
	if err != nil {
		if errors.Is(err, wcdomain.ErrConversationNotFound) {
			return &HistoryView{Messages: []wcdomain.VisitorMessage{}}, nil
		}
		return nil, err
	}
	limit := in.Limit
	if limit <= 0 || limit > maxHistoryPage {
		limit = defaultHistoryPage
	}
	rows, err := s.Messages.ListByEntryPaginated(conversation.ListMessagesInput{
		EntryID:   conv.ID,
		EntryType: shared.EntryTypeWebchat,
		Limit:     limit,
		Before:    in.Before,
		After:     in.After,
	})
	if err != nil {
		return nil, err
	}
	s.Presenter.PresentMessages(conv.ID, shared.EntryTypeWebchat, rows)

	out := make([]wcdomain.VisitorMessage, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		if projected, ok := wcdomain.ProjectMessage(rows[i]); ok {
			out = append(out, projected)
		}
	}
	return &HistoryView{
		Messages: out,
		Options:  conv.PendingOptions,
		Human:    conv.AutomationEnabled != nil && !*conv.AutomationEnabled,
	}, nil
}

type UploadInput struct {
	ClientMessageID string
	FileName        string
	Data            []byte
	PageURL         string
}

func (s *VisitorService) Upload(ctx context.Context, session *Session, in UploadInput) (*wcdomain.VisitorMessage, error) {
	w, v := session.Widget, session.Visitor
	if !w.AllowAttachments || s.Media == nil {
		return nil, wcdomain.ErrAttachmentsDisabled
	}
	if len(in.Data) == 0 {
		return nil, wcdomain.ErrAttachmentTypeRefused
	}
	if len(in.Data) > wcdomain.MaxAttachmentBytes {
		return nil, wcdomain.ErrAttachmentTooLarge
	}
	if err := allow(s.Limits.UploadsPerVisitor, v.ID); err != nil {
		return nil, err
	}
	if err := s.allowMessage(session); err != nil {
		return nil, err
	}
	if v.MustCompleteIntake(w) {
		return nil, wcdomain.ErrIntakePending
	}
	mime := strings.SplitN(http.DetectContentType(in.Data), ";", 2)[0]
	kind, accepted := wcdomain.AttachmentMIMETypes[mime]
	if !accepted {
		return nil, wcdomain.ErrAttachmentTypeRefused
	}
	providerID, err := wcdomain.InboundProviderID(v.ID, in.ClientMessageID)
	if err != nil {
		return nil, err
	}
	conv, err := s.conversationFor(ctx, session, in.PageURL)
	if err != nil {
		return nil, err
	}
	fresh, err := s.claimInbound(providerID)
	if err != nil {
		return nil, err
	}
	if !fresh {
		return &wcdomain.VisitorMessage{ID: providerID, Author: wcdomain.AuthorVisitor, CreatedAt: s.Now()}, nil
	}

	mediaType := conversation.MediaTypeDocument
	if kind == channel.MediaImage {
		mediaType = conversation.MediaTypeImage
	}
	stored, err := s.Media.Store(conversation.StoreMediaInput{
		ID:               uuid.NewString(),
		Key:              attachmentKeyTemplate + conv.ID + "/" + uuid.NewString() + extensionFor(mime),
		EntryID:          conv.ID,
		EntryType:        shared.EntryTypeWebchat,
		Type:             mediaType,
		MimeType:         mime,
		Data:             in.Data,
		OriginalFilename: safeFileName(in.FileName, mime),
	})
	if err != nil {
		return nil, err
	}

	now := s.Now()
	if err := s.Transcript.Record(ctx, conversation.MessageHistoryRecord{
		SentBy:            conversation.SentByContact(v.ID),
		EntryID:           conv.ID,
		EntryType:         shared.EntryTypeWebchat,
		Channel:           conversation.MessageChannelWebchat,
		MessageType:       conversation.MessageTypeMedia,
		ProviderMessageID: providerID,
		From:              v.ID,
		To:                w.ID,
		Timestamp:         now,
		MediaID:           stored.ID,
		MediaType:         mediaType,
		MediaURL:          stored.URL,
		SenderName:        v.DisplayName(),
	}); err != nil {
		s.releaseInbound(providerID)
		return nil, err
	}
	message := &wcdomain.VisitorMessage{
		ID:        providerID,
		Author:    wcdomain.AuthorVisitor,
		Media:     &wcdomain.VisitorMedia{Kind: string(mediaType), URL: stored.URL, MimeType: mime, Filename: stored.OriginalFilename},
		CreatedAt: now,
	}
	s.afterInbound(ctx, session, conv, message, "", nil)
	return message, nil
}

func extensionFor(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "application/pdf":
		return ".pdf"
	}
	return ""
}

func safeFileName(name, mime string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.', r == ' ':
			b.WriteRune(r)
		}
		if b.Len() >= 120 {
			break
		}
	}
	cleaned := strings.Trim(strings.TrimSpace(b.String()), ".")
	if cleaned == "" {
		return "arquivo" + extensionFor(mime)
	}
	return cleaned
}

func (s *VisitorService) RequestHuman(ctx context.Context, session *Session) error {
	w, v := session.Widget, session.Visitor
	if !w.AllowHumanRequest {
		return wcdomain.ErrHumanRequestDisabled
	}
	if err := allow(s.Limits.HandOffsPerVisitor, v.ID); err != nil {
		return err
	}
	conv, err := s.Conversations.FindByVisitor(ctx, w.ID, v.ID)
	if err != nil {
		return err
	}
	departmentID := ""
	if w.DepartmentID != nil {
		departmentID = *w.DepartmentID
	}
	if _, err := s.Assignments.HandOffToRoulette(ia.RouletteHandOff{
		WorkspaceID:  w.WorkspaceID,
		EntryID:      conv.ID,
		EntryType:    string(shared.EntryTypeWebchat),
		DepartmentID: departmentID,
		ByActorID:    actor.SystemID,
	}); err != nil {
		return err
	}
	s.publish(ctx, wcdomain.VisitorEvent{VisitorID: v.ID, Kind: wcdomain.EventStatus, State: wcdomain.StateHuman})
	return nil
}

func (s *VisitorService) Typing(ctx context.Context, session *Session, on bool) error {
	if err := allow(s.Limits.TypingPerVisitor, session.Visitor.ID); err != nil {
		return err
	}
	conv, err := s.Conversations.FindByVisitor(ctx, session.Widget.ID, session.Visitor.ID)
	if err != nil {
		if errors.Is(err, wcdomain.ErrConversationNotFound) {
			return nil
		}
		return err
	}
	s.Operators.BroadcastTyping(conv.ID, string(shared.EntryTypeWebchat), session.Visitor.ID, on)
	return nil
}
