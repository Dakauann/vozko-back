package emailinbox

import (
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	em "vozko/domain/emailinbox"
	"vozko/domain/shared"
)

func storeHTML(media conversation.MediaStore, conversationID, html string) string {
	html = strings.TrimSpace(html)
	if html == "" {
		return ""
	}
	if len(html) > em.MaxStoredHTMLBytes {
		log.Printf("%s the html of an email in %s is larger than %d bytes and is shown as text", logPrefix, conversationID, em.MaxStoredHTMLBytes)
		return ""
	}
	id := uuid.NewString()
	stored, err := media.Store(conversation.StoreMediaInput{
		ID:               id,
		Key:              fmt.Sprintf("conversations/%s/%s/%s.html", shared.EntryTypeEmail, conversationID, id),
		EntryID:          conversationID,
		EntryType:        shared.EntryTypeEmail,
		Type:             conversation.MediaTypeDocument,
		MimeType:         em.HTMLMimeType,
		Data:             []byte(html),
		OriginalFilename: "email.html",
		Purpose:          conversation.MediaPurposeEmailBody,
	})
	if err != nil {
		log.Printf("%s the html of an email in %s was not stored and is shown as text: %v", logPrefix, conversationID, err)
		return ""
	}
	return stored.ID
}
