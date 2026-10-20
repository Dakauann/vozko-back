package conversation_usecase

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

var ErrFormFileIntegrity = errors.New("conversation: the form file does not match the hash WhatsApp sent")

type OfficialFormTarget struct {
	LeadID          string
	LeadNumber      string
	BusinessPhoneID string
	WorkspaceID     string
	WindowOpen      bool
}

func (s *MessageSenderService) OfficialTarget(entryID string) (OfficialFormTarget, error) {
	leadID, number, phoneID, workspaceID, err := whatsappEntryTarget(s.whatsappRepo, s.leadRepo, entryID)
	if err != nil {
		return OfficialFormTarget{}, err
	}
	return OfficialFormTarget{
		LeadID: leadID, LeadNumber: number, BusinessPhoneID: phoneID, WorkspaceID: workspaceID,
		WindowOpen: s.checkMessageWindow(leadID, phoneID) == nil,
	}, nil
}

type FormMessageRecord struct {
	EntryID   string
	To        string
	MessageID string
	Body      string
	SentBy    string
	Metadata  map[string]any
}

func (s *MessageSenderService) RecordFormMessage(in FormMessageRecord) (*conversation.Message, error) {
	now := time.Now().UTC()
	providerID := in.MessageID
	message := &conversation.Message{
		ID: uuid.NewString(), EntryID: in.EntryID, EntryType: shared.EntryTypeWhatsApp, Channel: conversation.MessageChannelWhatsApp,
		To: in.To, Text: in.Body, Read: true, ReadAt: &now, WhatsAppMessageID: &providerID,
		DeliveryStatus: conversation.DeliveryStatusSent, CreatedAt: now, UpdatedAt: now,
	}
	if in.SentBy != "" {
		author := in.SentBy
		message.SentBy, message.MessageType, message.From, message.ReadBy = conversation.SentByPerson(author), conversation.MessageTypeOperator, author, &author
	} else {
		message.SentBy, message.MessageType = conversation.SentBySystem(), conversation.MessageTypeAIResponse
	}
	keys := make([]string, 0, len(in.Metadata))
	for key := range in.Metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := message.AttachMetadata(key, in.Metadata[key]); err != nil {
			return nil, fmt.Errorf("form message metadata: %w", err)
		}
	}
	message.Normalize()
	if err := s.messageRepo.Create(message); err != nil {
		return nil, fmt.Errorf("failed to save form message: %w", err)
	}
	s.scheduleAnalysis(in.EntryID, string(shared.EntryTypeWhatsApp))
	return message, nil
}

type FormFile struct {
	BusinessPhoneID string
	EntryID         string
	EntryType       shared.EntryType
	MediaID         string
	Name            string
	MimeType        string
	SHA256          string
}

type FormFileStore struct {
	clients conversation.WhatsAppClientFactory
	store   conversation.MediaStore
	newID   func() string
}

func NewFormFileStore(clients conversation.WhatsAppClientFactory, store conversation.MediaStore, newID func() string) *FormFileStore {
	if newID == nil {
		newID = uuid.NewString
	}
	return &FormFileStore{clients: clients, store: store, newID: newID}
}

func hashMatches(data []byte, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	sum := sha256.Sum256(data)
	return strings.EqualFold(expected, hex.EncodeToString(sum[:])) || expected == base64.StdEncoding.EncodeToString(sum[:])
}

func (f *FormFileStore) Store(ctx context.Context, file FormFile) (string, error) {
	client, err := f.clients.ClientForPhone(file.BusinessPhoneID)
	if err != nil {
		return "", err
	}
	data, mimeType, err := client.DownloadMedia(ctx, file.MediaID)
	if err != nil {
		return "", fmt.Errorf("form file %s: %w", file.MediaID, err)
	}
	if !hashMatches(data, file.SHA256) {
		return "", ErrFormFileIntegrity
	}
	if strings.TrimSpace(file.SHA256) == "" {
		log.Printf("[whatsapp-forms] file %s arrived without a hash to check", file.MediaID)
	}
	if mimeType == "" {
		mimeType = file.MimeType
	}
	kind := conversation.MediaTypeDocument
	if strings.HasPrefix(mimeType, "image/") {
		kind = conversation.MediaTypeImage
	}
	id := f.newID()
	stored, err := f.store.Store(conversation.StoreMediaInput{
		ID: id, Key: fmt.Sprintf("conversations/%s/%s/%s%s", file.EntryType, file.EntryID, id, getExtensionFromMimeType(mimeType)),
		EntryID: file.EntryID, EntryType: file.EntryType, Type: kind, MimeType: mimeType, Data: data,
		OriginalFilename: file.Name, WhatsAppMediaID: file.MediaID,
	})
	if err != nil {
		return "", err
	}
	return stored.ID, nil
}
