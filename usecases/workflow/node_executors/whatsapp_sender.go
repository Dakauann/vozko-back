package node_executors

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"strings"
	"time"

	"vozko/domain/balance"
	"vozko/domain/conversation"
	lead_domain "vozko/domain/lead"
	media_domain "vozko/domain/media"
	"vozko/domain/messaging"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	template_domain "vozko/domain/whatsapp/template"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workflow"
)

type leadLookup interface {
	FindByID(workspaceID, id string) (*lead_domain.Lead, error)
}

type whatsappEntryLookup interface {
	FindByID(id string) (*wce.WhatsAppCampaignEntry, error)
	GetCampaignForEntry(entryID string) (*wce.EntryCampaignInfo, error)
}

type businessPhoneLookup interface {
	FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error)
}

type workspacePhoneAccessLookup interface {
	HasAccess(workspaceID, phoneID string) (bool, error)
}

type messageWindowLookup interface {
	IsWindowOpen(leadID, businessPhoneID string) (bool, error)
}

type templateLookup interface {
	FindByID(templateID string) (*template_domain.Template, error)
}

type mediaLookup interface {
	GetMediaByID(mediaID string) (*media_domain.Media, error)
}

// SenderDeps carries everything the workflow send nodes need.
//
// Most fields are WhatsApp's (client factory, business phones, templates, the
// lead window) because WhatsApp keeps a dedicated sender; Adapters serves every
// other channel through the shared registry. The struct is named for its role,
// sending, rather than for the channel that needs the most from it.
type SenderDeps struct {
	ClientFactory           conversation.WhatsAppClientFactory
	LeadRepo                leadLookup
	WhatsAppEntryRepo       whatsappEntryLookup
	BusinessPhoneRepo       businessPhoneLookup
	MessageWindowRepo       messageWindowLookup
	HistoryManager          conversation.MessageHistoryManager
	TemplateRepo            templateLookup
	MediaRepo               mediaLookup
	ConsumeWhatsappTemplate balance.ConsumeWhatsappTemplateUseCase
	WorkspacePhoneAccess    workspacePhoneAccessLookup

	BillingPub messaging.MessageQueuePub

	FileStorage media_domain.FileStorage

	ConversationMediaRepo conversation.ConversationMediaRepository

	// Adapters is the channel registry used for every non-WhatsApp channel.
	// Optional: without it, only WhatsApp can be sent to.
	Adapters conversation.AdapterRegistry
}

type whatsappSender struct {
	deps SenderDeps
}

type whatsappSendTarget struct {
	leadID                  string
	leadNumber              string
	campaignBusinessPhoneID string
	receivedBusinessPhoneID string
}

func newWhatsAppSender(deps SenderDeps) *whatsappSender {
	if deps.ClientFactory == nil || deps.LeadRepo == nil || deps.WhatsAppEntryRepo == nil {
		return nil
	}
	return &whatsappSender{deps: deps}
}

func NewWhatsAppSenderFromDeps(deps SenderDeps) *whatsappSender {
	return newWhatsAppSender(deps)
}

func (s *whatsappSender) SendText(ctx context.Context, run *workflow.WorkflowRun, text string, messageType conversation.MessageType) (*conversation.SendTextMessageOutput, string, error) {
	if s == nil {
		return nil, "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if run == nil || shared.EntryType(run.EntryType) != shared.EntryTypeWhatsApp {
		return nil, "", nil
	}
	target, err := s.resolveTarget(run.EntryID, run.WorkspaceID)
	if err != nil {
		return nil, "", err
	}

	selectedBusinessPhoneID := target.receivedBusinessPhoneID
	if selectedBusinessPhoneID == "" {
		selectedBusinessPhoneID = target.campaignBusinessPhoneID
	}
	if err := s.ensureWindowOpen(target.leadID, selectedBusinessPhoneID); err != nil {
		return nil, "", err
	}

	client, usedBusinessPhoneID, err := s.resolveWhatsAppClient(target.campaignBusinessPhoneID, target.receivedBusinessPhoneID)
	if err != nil {
		return nil, "", err
	}

	output, err := client.SendTextMessage(ctx, conversation.SendTextMessageInput{
		To:   target.leadNumber,
		Body: text,
	})
	if err != nil {
		return nil, usedBusinessPhoneID, err
	}

	if s.deps.HistoryManager != nil {
		from := s.resolveBusinessPhoneNumber(usedBusinessPhoneID)
		record := conversation.MessageHistoryRecord{
			EntryID:     run.EntryID,
			EntryType:   shared.EntryTypeWhatsApp,
			Channel:     conversation.MessageChannelWhatsApp,
			MessageType: messageType,
			MessageID:   strings.TrimSpace(output.MessageID),
			From:        from,
			To:          target.leadNumber,
			Text:        text,
			Timestamp:   time.Now().UTC(),
		}
		if err := s.deps.HistoryManager.Record(ctx, conversation.MessageDirectionOutbound, record); err != nil {
			return nil, usedBusinessPhoneID, err
		}
	}

	return output, usedBusinessPhoneID, nil
}

func (s *whatsappSender) SendButtons(ctx context.Context, run *workflow.WorkflowRun, text string, buttons []conversation.InteractiveButton) (*conversation.SendTextMessageOutput, string, error) {
	if s == nil {
		return nil, "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if run == nil || shared.EntryType(run.EntryType) != shared.EntryTypeWhatsApp {
		return nil, "", nil
	}
	target, err := s.resolveTarget(run.EntryID, run.WorkspaceID)
	if err != nil {
		return nil, "", err
	}

	selectedBusinessPhoneID := target.receivedBusinessPhoneID
	if selectedBusinessPhoneID == "" {
		selectedBusinessPhoneID = target.campaignBusinessPhoneID
	}

	if err := s.ensureWindowOpen(target.leadID, selectedBusinessPhoneID); err != nil {
		return nil, "", err
	}

	client, usedBusinessPhoneID, err := s.resolveWhatsAppClient(target.campaignBusinessPhoneID, target.receivedBusinessPhoneID)
	if err != nil {
		return nil, usedBusinessPhoneID, err
	}

	res, err := client.SendButtonMessage(ctx, conversation.SendButtonMessageInput{
		To:         target.leadNumber,
		HeaderType: conversation.HeaderTypeText,
		HeaderText: text,
		Buttons:    buttons,
	})

	if err != nil {
		return nil, usedBusinessPhoneID, err
	}

	if s.deps.HistoryManager != nil {
		from := s.resolveBusinessPhoneNumber(usedBusinessPhoneID)
		record := conversation.MessageHistoryRecord{
			EntryID:     run.EntryID,
			EntryType:   shared.EntryTypeWhatsApp,
			Channel:     conversation.MessageChannelWhatsApp,
			MessageType: conversation.MessageTypeTemplate,
			MessageID:   strings.TrimSpace(res.MessageID),
			From:        from,
			To:          target.leadNumber,
			Text:        text,
			Timestamp:   time.Now().UTC(),
		}
		if err := s.deps.HistoryManager.Record(ctx, conversation.MessageDirectionOutbound, record); err != nil {
			return nil, usedBusinessPhoneID, err
		}
	}

	return res, usedBusinessPhoneID, nil
}

func (s *whatsappSender) SendButtonsWithInput(ctx context.Context, run *workflow.WorkflowRun, input conversation.SendButtonMessageInput) (*conversation.SendTextMessageOutput, string, error) {
	if s == nil {
		return nil, "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if run == nil || shared.EntryType(run.EntryType) != shared.EntryTypeWhatsApp {
		return nil, "", nil
	}
	target, err := s.resolveTarget(run.EntryID, run.WorkspaceID)
	if err != nil {
		return nil, "", err
	}

	selectedBusinessPhoneID := target.receivedBusinessPhoneID
	if selectedBusinessPhoneID == "" {
		selectedBusinessPhoneID = target.campaignBusinessPhoneID
	}

	if err := s.ensureWindowOpen(target.leadID, selectedBusinessPhoneID); err != nil {
		return nil, "", err
	}

	client, usedBusinessPhoneID, err := s.resolveWhatsAppClient(target.campaignBusinessPhoneID, target.receivedBusinessPhoneID)
	if err != nil {
		return nil, usedBusinessPhoneID, err
	}

	input.To = target.leadNumber
	res, err := client.SendButtonMessage(ctx, input)
	if err != nil {
		return nil, usedBusinessPhoneID, err
	}

	if s.deps.HistoryManager != nil {
		from := s.resolveBusinessPhoneNumber(usedBusinessPhoneID)
		record := conversation.MessageHistoryRecord{
			EntryID:     run.EntryID,
			EntryType:   shared.EntryTypeWhatsApp,
			Channel:     conversation.MessageChannelWhatsApp,
			MessageType: conversation.MessageTypeTemplate,
			MessageID:   strings.TrimSpace(res.MessageID),
			From:        from,
			To:          target.leadNumber,
			Text:        input.BodyText,
			Timestamp:   time.Now().UTC(),
		}
		if err := s.deps.HistoryManager.Record(ctx, conversation.MessageDirectionOutbound, record); err != nil {
			return nil, usedBusinessPhoneID, err
		}
	}

	return res, usedBusinessPhoneID, nil
}

func (s *whatsappSender) SendListWithInput(ctx context.Context, run *workflow.WorkflowRun, input conversation.SendListMessageInput) (*conversation.SendTextMessageOutput, string, error) {
	if s == nil {
		return nil, "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if run == nil || shared.EntryType(run.EntryType) != shared.EntryTypeWhatsApp {
		return nil, "", nil
	}
	target, err := s.resolveTarget(run.EntryID, run.WorkspaceID)
	if err != nil {
		return nil, "", err
	}

	selectedBusinessPhoneID := target.receivedBusinessPhoneID
	if selectedBusinessPhoneID == "" {
		selectedBusinessPhoneID = target.campaignBusinessPhoneID
	}

	if err := s.ensureWindowOpen(target.leadID, selectedBusinessPhoneID); err != nil {
		return nil, "", err
	}

	client, usedBusinessPhoneID, err := s.resolveWhatsAppClient(target.campaignBusinessPhoneID, target.receivedBusinessPhoneID)
	if err != nil {
		return nil, usedBusinessPhoneID, err
	}

	input.To = target.leadNumber
	res, err := client.SendListMessage(ctx, input)
	if err != nil {
		return nil, usedBusinessPhoneID, err
	}

	if s.deps.HistoryManager != nil {
		from := s.resolveBusinessPhoneNumber(usedBusinessPhoneID)
		record := conversation.MessageHistoryRecord{
			EntryID:     run.EntryID,
			EntryType:   shared.EntryTypeWhatsApp,
			Channel:     conversation.MessageChannelWhatsApp,
			MessageType: conversation.MessageTypeTemplate,
			MessageID:   strings.TrimSpace(res.MessageID),
			From:        from,
			To:          target.leadNumber,
			Text:        input.BodyText,
			Timestamp:   time.Now().UTC(),
		}
		if err := s.deps.HistoryManager.Record(ctx, conversation.MessageDirectionOutbound, record); err != nil {
			return nil, usedBusinessPhoneID, err
		}
	}

	return res, usedBusinessPhoneID, nil
}

func (s *whatsappSender) SendMedia(ctx context.Context, run *workflow.WorkflowRun, mediaURL, caption string) (*conversation.SendTextMessageOutput, string, error) {
	if s == nil {
		return nil, "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if run == nil || shared.EntryType(run.EntryType) != shared.EntryTypeWhatsApp {
		return nil, "", nil
	}
	target, err := s.resolveTarget(run.EntryID, run.WorkspaceID)
	if err != nil {
		return nil, "", err
	}

	selectedBusinessPhoneID := target.receivedBusinessPhoneID
	if selectedBusinessPhoneID == "" {
		selectedBusinessPhoneID = target.campaignBusinessPhoneID
	}
	if err := s.ensureWindowOpen(target.leadID, selectedBusinessPhoneID); err != nil {
		return nil, "", err
	}

	client, usedBusinessPhoneID, err := s.resolveWhatsAppClient(target.campaignBusinessPhoneID, target.receivedBusinessPhoneID)
	if err != nil {
		return nil, usedBusinessPhoneID, err
	}

	mediaType := detectMediaType(mediaURL)
	var output *conversation.SendTextMessageOutput

	waMediaID, uploadErr := s.downloadAndUpload(ctx, client, mediaURL, mediaType)
	if uploadErr != nil {
		log.Printf("[workflow][whatsapp_sender] upload-first failed for %s, will fall back to link: %v", mediaURL, uploadErr)
	}

	switch mediaType {
	case "image":
		input := conversation.SendImageMessageInput{
			To:      target.leadNumber,
			Caption: caption,
		}
		if waMediaID != "" {
			input.ImageID = waMediaID
		} else {
			input.Link = mediaURL
		}
		output, err = client.SendImageMessage(ctx, input)
	case "video":
		input := conversation.SendVideoMessageInput{
			To:      target.leadNumber,
			Caption: caption,
		}
		if waMediaID != "" {
			input.VideoID = waMediaID
		} else {
			input.Link = mediaURL
		}
		output, err = client.SendVideoMessage(ctx, input)
	case "audio":
		output, err = client.SendAudioMessage(ctx, conversation.SendAudioMessageInput{
			To:       target.leadNumber,
			AudioURL: mediaURL,
		})
	default:
		filename := path.Base(mediaURL)
		if idx := strings.Index(filename, "?"); idx != -1 {
			filename = filename[:idx]
		}
		input := conversation.SendDocumentMessageInput{
			To:       target.leadNumber,
			Caption:  caption,
			Filename: filename,
		}
		if waMediaID != "" {
			input.DocumentID = waMediaID
		} else {
			input.Link = mediaURL
		}
		output, err = client.SendDocumentMessage(ctx, input)
	}

	if err != nil {
		return nil, usedBusinessPhoneID, err
	}

	if s.deps.HistoryManager != nil {
		from := s.resolveBusinessPhoneNumber(usedBusinessPhoneID)
		record := conversation.MessageHistoryRecord{
			EntryID:     run.EntryID,
			EntryType:   shared.EntryTypeWhatsApp,
			Channel:     conversation.MessageChannelWhatsApp,
			MessageType: conversation.MessageTypeMedia,
			MessageID:   strings.TrimSpace(output.MessageID),
			From:        from,
			To:          target.leadNumber,
			Text:        caption,
			Timestamp:   time.Now().UTC(),
		}
		if err := s.deps.HistoryManager.Record(ctx, conversation.MessageDirectionOutbound, record); err != nil {
			return nil, usedBusinessPhoneID, err
		}
	}

	return output, usedBusinessPhoneID, nil
}

func (s *whatsappSender) SendTemplate(ctx context.Context, run *workflow.WorkflowRun, templateID, preferredBusinessPhoneID string, paramValues map[string]string, state *workflow.RunState) (*conversation.SendTextMessageOutput, string, error) {
	if s == nil {
		return nil, "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if run == nil {
		return nil, "", nil
	}
	if s.deps.TemplateRepo == nil {
		return nil, "", fmt.Errorf("workflow whatsapp sender: template repository not configured")
	}
	tmpl, err := s.deps.TemplateRepo.FindByID(templateID)
	if err != nil {
		return nil, "", fmt.Errorf("workflow whatsapp sender: failed to load template %s: %w", templateID, err)
	}
	if tmpl == nil {
		return nil, "", fmt.Errorf("workflow whatsapp sender: template not found: %s", templateID)
	}
	if !tmpl.IsReadyToSend() {
		return nil, "", fmt.Errorf("workflow whatsapp sender: template %s is not ready to send: %s", tmpl.Name, tmpl.GetUsabilityMessage())
	}

	target, err := s.resolveTargetForRun(run, state)
	if err != nil {
		return nil, "", err
	}

	client, usedBusinessPhoneID, err := s.resolveTemplateClient(run.WorkspaceID, strings.TrimSpace(preferredBusinessPhoneID), target, tmpl.WABAId)
	if err != nil {
		return nil, usedBusinessPhoneID, err
	}

	language := tmpl.Language
	if language == "" {
		language = "pt_BR"
	}

	apiInput := conversation.SendTemplateMessageInput{
		To:           target.leadNumber,
		TemplateName: tmpl.Name,
		Language:     language,
	}

	bodyParamNames, headerParamNames := tmpl.GetBodyAndHeaderParameterNames()
	isNamed := tmpl.IsNamedParameterFormat()
	apiInput.IsNamedParameterFormat = isNamed
	apiInput.ParameterNames = bodyParamNames

	if isNamed {

		params := make([]string, len(bodyParamNames))
		for i, name := range bodyParamNames {
			val := paramValues[name]
			val = workflow.Interpolate(val, state, nil)
			params[i] = val
		}
		apiInput.Parameters = params
	} else {

		params := make([]string, len(bodyParamNames))
		for i, name := range bodyParamNames {
			val := paramValues[name]
			val = workflow.Interpolate(val, state, nil)
			params[i] = val
		}
		apiInput.Parameters = params
	}

	if len(headerParamNames) > 0 {
		headerParams := make([]string, len(headerParamNames))
		for i, name := range headerParamNames {
			val := paramValues["header_"+name]
			val = workflow.Interpolate(val, state, nil)
			headerParams[i] = val
		}
		apiInput.HeaderTextParams = headerParams
	}

	if tmpl.HasMediaHeader() {
		headerMediaID := tmpl.GetHeaderMediaID()
		if headerMediaID != "" {
			apiInput.HeaderType = strings.ToLower(tmpl.GetHeaderFormat())
			apiInput.HeaderMediaID = headerMediaID
		}
	}

	log.Printf("[workflow][whatsapp_sender] sending template %s (lang=%s, named=%v, bodyParams=%d, headerParams=%d) to %s",
		tmpl.Name, language, isNamed, len(apiInput.Parameters), len(apiInput.HeaderTextParams), target.leadNumber)

	templateCategory, err := tmpl.BillingCategory()
	if err != nil {
		return nil, usedBusinessPhoneID, fmt.Errorf("workflow whatsapp sender: template billing category unavailable: %w", err)
	}
	billingRefID := fmt.Sprintf("wf:%s:%s", run.ID, run.CurrentNodeID)
	// Fail CLOSED on a missing billing dependency. This used to be
	// `if s.deps.ConsumeWhatsappTemplate != nil`, which meant a mis-wired
	// container silently sent paid templates for free — the failure mode nobody
	// notices until the invoice arrives.
	if s.deps.ConsumeWhatsappTemplate == nil {
		return nil, usedBusinessPhoneID, fmt.Errorf("workflow whatsapp sender: billing dependency not configured, refusing to send a paid template")
	}
	if _, consumeErr := s.deps.ConsumeWhatsappTemplate.Execute(run.WorkspaceID, billingRefID, templateCategory); consumeErr != nil {
		return nil, usedBusinessPhoneID, fmt.Errorf("insufficient balance to send WhatsApp template: %w", consumeErr)
	}

	output, err := client.SendTemplateMessage(ctx, apiInput)
	if err != nil && errors.Is(err, conversation.ErrSendOutcomeUnknown) {
		// Accepted by Meta, unreadable to us. Never refund, never resend.
		log.Printf("[workflow][whatsapp_sender] send outcome unknown for run=%s (Meta accepted), keeping the charge: %v", run.ID, err)
		err = nil
	}
	if err != nil {

		if refundErr := s.deps.ConsumeWhatsappTemplate.Refund(run.WorkspaceID, billingRefID, templateCategory); refundErr != nil {
			log.Printf("[workflow][whatsapp_sender] WARNING: failed to refund balance for template %s run=%s: %v", tmpl.Name, run.ID, refundErr)
		}
		return nil, usedBusinessPhoneID, err
	}

	if s.deps.HistoryManager != nil {
		from := s.resolveBusinessPhoneNumber(usedBusinessPhoneID)
		record := conversation.MessageHistoryRecord{
			EntryID:     run.EntryID,
			EntryType:   shared.EntryType(run.EntryType),
			Channel:     conversation.MessageChannelWhatsApp,
			MessageType: conversation.MessageTypeTemplate,
			MessageID:   strings.TrimSpace(output.MessageID),
			From:        from,
			To:          target.leadNumber,
			Text:        fmt.Sprintf("[template:%s]", tmpl.Name),
			Timestamp:   time.Now().UTC(),
		}
		if err := s.deps.HistoryManager.Record(ctx, conversation.MessageDirectionOutbound, record); err != nil {
			return nil, usedBusinessPhoneID, err
		}
	}

	return output, usedBusinessPhoneID, nil
}

func (s *whatsappSender) resolveTargetForRun(run *workflow.WorkflowRun, state *workflow.RunState) (*whatsappSendTarget, error) {
	if run == nil {
		return nil, fmt.Errorf("workflow whatsapp sender: run is required")
	}
	if shared.EntryType(run.EntryType) == shared.EntryTypeWhatsApp {
		return s.resolveTarget(run.EntryID, run.WorkspaceID)
	}

	leadID := ""
	leadNumber := ""
	if state != nil {
		leadID = strings.TrimSpace(state.GetString("lead_id"))
		leadNumber = strings.TrimSpace(state.GetString("phone_number"))
	}

	if leadNumber == "" && leadID != "" && s.deps.LeadRepo != nil {
		leadRecord, err := s.deps.LeadRepo.FindByID(run.WorkspaceID, leadID)
		if err != nil {
			return nil, fmt.Errorf("workflow whatsapp sender: failed to load lead %s: %w", leadID, err)
		}
		if leadRecord != nil {
			leadNumber = strings.TrimSpace(leadRecord.Number)
		}
	}

	if leadNumber == "" {
		return nil, fmt.Errorf("workflow whatsapp sender: no recipient phone available for entry type %s", run.EntryType)
	}

	return &whatsappSendTarget{
		leadID:                  leadID,
		leadNumber:              leadNumber,
		campaignBusinessPhoneID: "",
		receivedBusinessPhoneID: "",
	}, nil
}

func (s *whatsappSender) resolveTemplateClient(workspaceID, preferredBusinessPhoneID string, target *whatsappSendTarget, templateWABAID string) (conversation.WhatsAppClient, string, error) {
	if preferredBusinessPhoneID != "" {
		if err := s.ensureWorkspaceOwnsPhone(workspaceID, preferredBusinessPhoneID); err != nil {
			return nil, preferredBusinessPhoneID, err
		}
		client, err := s.resolveTemplateClientForPhone(preferredBusinessPhoneID, templateWABAID)
		if err != nil {
			return nil, preferredBusinessPhoneID, err
		}
		return client, preferredBusinessPhoneID, nil
	}

	candidates := []string{target.receivedBusinessPhoneID, target.campaignBusinessPhoneID}
	seen := make(map[string]struct{}, len(candidates))
	var lastErr error
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}

		client, err := s.resolveTemplateClientForPhone(candidate, templateWABAID)
		if err == nil {
			return client, candidate, nil
		}
		lastErr = err
		log.Printf("[workflow][whatsapp_sender] template phone candidate %s rejected: %v", candidate, err)
	}

	if lastErr != nil {
		return nil, "", lastErr
	}

	return nil, "", fmt.Errorf("workflow whatsapp sender: no business phone available for template %s", templateWABAID)
}

func (s *whatsappSender) resolveTemplateClientForPhone(businessPhoneID, templateWABAID string) (conversation.WhatsAppClient, error) {
	businessPhoneID = strings.TrimSpace(businessPhoneID)
	if businessPhoneID == "" {
		return nil, fmt.Errorf("workflow whatsapp sender: business phone is required")
	}
	if templateWABAID != "" {
		phoneWABAID, err := s.deps.ClientFactory.WABAIdForPhone(businessPhoneID)
		if err != nil {
			return nil, fmt.Errorf("workflow whatsapp sender: failed to resolve WABA for phone %s: %w", businessPhoneID, err)
		}
		if phoneWABAID != "" && phoneWABAID != templateWABAID {
			return nil, fmt.Errorf("workflow whatsapp sender: phone %s belongs to WABA %s, but template belongs to WABA %s", businessPhoneID, phoneWABAID, templateWABAID)
		}
	}

	client, err := s.deps.ClientFactory.ClientForPhone(businessPhoneID)
	if err != nil {
		return nil, fmt.Errorf("workflow whatsapp sender: failed to create client for phone %s: %w", businessPhoneID, err)
	}
	return client, nil
}

const maxMediaDownloadBytes = 25 * 1024 * 1024

func (s *whatsappSender) downloadAndUpload(ctx context.Context, client conversation.WhatsAppClient, mediaURL, mediaType string) (string, error) {
	if mediaType == "audio" {
		return "", fmt.Errorf("audio upload-first not supported")
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return "", fmt.Errorf("invalid media url: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMediaDownloadBytes+1))
	if err != nil {
		return "", fmt.Errorf("read failed: %w", err)
	}
	if len(data) > maxMediaDownloadBytes {
		return "", fmt.Errorf("media exceeds %d bytes", maxMediaDownloadBytes)
	}

	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		switch mediaType {
		case "image":
			mimeType = "image/jpeg"
		case "video":
			mimeType = "video/mp4"
		default:
			mimeType = "application/pdf"
		}
	}

	fileName := path.Base(strings.SplitN(mediaURL, "?", 2)[0])
	if fileName == "" || fileName == "." || fileName == "/" {
		fileName = "media"
	}

	waMediaID, err := client.UploadMedia(ctx, data, fileName, mimeType)
	if err != nil {
		return "", fmt.Errorf("whatsapp upload failed: %w", err)
	}

	log.Printf("[workflow][whatsapp_sender] uploaded media to WhatsApp: waMediaID=%s type=%s size=%d", waMediaID, mediaType, len(data))
	return waMediaID, nil
}

func detectMediaType(mediaURL string) string {
	ext := strings.ToLower(path.Ext(strings.SplitN(mediaURL, "?", 2)[0]))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		return "image"
	case ".mp4", ".avi", ".mov", ".mkv", ".webm", ".3gp":
		return "video"
	case ".mp3", ".ogg", ".opus", ".wav", ".aac", ".m4a", ".amr":
		return "audio"
	default:
		return "document"
	}
}

func (s *whatsappSender) resolveTarget(entryID, fallbackWorkspaceID string) (*whatsappSendTarget, error) {
	entry, err := s.deps.WhatsAppEntryRepo.FindByID(entryID)
	if err != nil {
		return nil, fmt.Errorf("workflow whatsapp sender: failed to load entry %s: %w", entryID, err)
	}
	if entry == nil {
		return nil, fmt.Errorf("workflow whatsapp sender: entry not found: %s", entryID)
	}

	var campaignBusinessPhoneID, workspaceID string
	if info, err := s.deps.WhatsAppEntryRepo.GetCampaignForEntry(entryID); err == nil && info != nil {
		campaignBusinessPhoneID = strings.TrimSpace(info.BusinessPhoneID)
		workspaceID = strings.TrimSpace(info.WorkspaceID)
	} else if err != nil {
		log.Printf("[workflow][whatsapp_sender] failed to load campaign info for entry %s: %v", entryID, err)
	}

	if workspaceID == "" {
		workspaceID = strings.TrimSpace(fallbackWorkspaceID)
	}
	if workspaceID == "" {
		return nil, fmt.Errorf("workflow whatsapp sender: workspace not resolved for entry %s", entryID)
	}

	leadRecord, err := s.deps.LeadRepo.FindByID(workspaceID, entry.LeadID)
	if err != nil {
		return nil, fmt.Errorf("workflow whatsapp sender: failed to load lead %s: %w", entry.LeadID, err)
	}
	if leadRecord == nil {
		return nil, fmt.Errorf("workflow whatsapp sender: lead not found: %s", entry.LeadID)
	}

	return &whatsappSendTarget{
		leadID:                  entry.LeadID,
		leadNumber:              strings.TrimSpace(leadRecord.Number),
		campaignBusinessPhoneID: campaignBusinessPhoneID,
		receivedBusinessPhoneID: strings.TrimSpace(entry.ReceivedBusinessPhoneID),
	}, nil
}

func (s *whatsappSender) resolveWhatsAppClient(campaignBusinessPhoneID, receivedBusinessPhoneID string) (conversation.WhatsAppClient, string, error) {
	if receivedBusinessPhoneID != "" {
		client, err := s.deps.ClientFactory.ClientForPhone(receivedBusinessPhoneID)
		if err == nil {
			log.Printf("[workflow][whatsapp_sender] resolved client for received phone: %s", receivedBusinessPhoneID)
			return client, receivedBusinessPhoneID, nil
		}
		log.Printf("[workflow][whatsapp_sender] failed to create client for received phone %s: %v", receivedBusinessPhoneID, err)
	}

	if campaignBusinessPhoneID != "" {
		client, err := s.deps.ClientFactory.ClientForPhone(campaignBusinessPhoneID)
		if err == nil {
			log.Printf("[workflow][whatsapp_sender] resolved client for campaign phone: %s", campaignBusinessPhoneID)
			return client, campaignBusinessPhoneID, nil
		}
		log.Printf("[workflow][whatsapp_sender] failed to create client for campaign phone %s: %v", campaignBusinessPhoneID, err)
	}

	return nil, "", fmt.Errorf("cannot resolve WhatsApp client: no valid business phone (campaign=%q, received=%q)", campaignBusinessPhoneID, receivedBusinessPhoneID)
}

func (s *whatsappSender) ensureWindowOpen(leadID, businessPhoneID string) error {
	if s.deps.MessageWindowRepo == nil || leadID == "" || businessPhoneID == "" {
		return nil
	}
	isOpen, err := s.deps.MessageWindowRepo.IsWindowOpen(leadID, businessPhoneID)
	if err != nil {
		return conversation.ErrWindowClosed
	}
	if !isOpen {
		return conversation.ErrWindowClosed
	}
	return nil
}

func (s *whatsappSender) resolveBusinessPhoneNumber(businessPhoneID string) string {
	if businessPhoneID == "" || s.deps.BusinessPhoneRepo == nil {
		return businessPhoneID
	}
	phone, err := s.deps.BusinessPhoneRepo.FindByID(businessPhoneID)
	if err != nil || phone == nil {
		return businessPhoneID
	}
	if display := strings.TrimSpace(phone.DisplayPhoneNumber); display != "" {
		return display
	}
	return businessPhoneID
}

func (s *whatsappSender) ensureWorkspaceOwnsPhone(workspaceID, businessPhoneID string) error {
	if s.deps.BusinessPhoneRepo == nil || workspaceID == "" || businessPhoneID == "" {
		return nil
	}
	phone, err := s.deps.BusinessPhoneRepo.FindByID(businessPhoneID)
	if err != nil {
		return fmt.Errorf("workflow whatsapp sender: failed to verify workspace ownership for phone %s: %w", businessPhoneID, err)
	}
	if phone == nil || !phone.BelongsToWorkspace(workspaceID) {
		return fmt.Errorf("workflow whatsapp sender: phone %s is not available in workspace %s", businessPhoneID, workspaceID)
	}
	return nil
}

func (s *whatsappSender) BuildMessagingToolConfig(run *workflow.WorkflowRun) map[string]interface{} {
	if s == nil || run == nil {
		return nil
	}
	target, err := s.resolveTarget(run.EntryID, run.WorkspaceID)
	if err != nil {
		log.Printf("[workflow][whatsapp_sender] failed to resolve target for tool config: %v", err)
		return nil
	}

	businessPhoneID := target.receivedBusinessPhoneID
	if businessPhoneID == "" {
		businessPhoneID = target.campaignBusinessPhoneID
	}
	if businessPhoneID == "" {
		log.Printf("[workflow][whatsapp_sender] no business phone available for tool config (entry=%s)", run.EntryID)
		return nil
	}

	return map[string]interface{}{
		"__recipient_phone":   target.leadNumber,
		"__business_phone_id": businessPhoneID,
		"__workspace_id":      run.WorkspaceID,
		"__entry_id":          run.EntryID,
		"__entry_type":        run.EntryType,
	}
}
