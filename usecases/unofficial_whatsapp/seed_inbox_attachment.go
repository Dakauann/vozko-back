package unofficial_whatsapp

import (
	"log"
	"mime"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

// The file a seeded opening carries.
//
// The bytes are already in object storage — an administrator uploaded them to
// the workspace's media library before the import ran — so nothing here
// uploads, downloads or re-encodes anything. All this does is translate one
// library asset into the per-conversation media row the CRM renders an
// attachment from, which is the same shape the inbound webhook writes.

// seedAsset is a workspace library object, resolved once per batch.
//
// Flattened out of media.Media on purpose: the library's own type carries a
// workspace and a preview URL that mean nothing to a conversation row, and the
// three fields a conversation actually needs are decided once here rather than
// re-derived per target.
type seedAsset struct {
	url       string
	mimeType  string
	fileName  string
	mediaType conversation.MediaType
}

// resolveAsset turns the script's attachment into the object behind it.
//
// Every refusal DEGRADES: the conversations still open and the opening still
// lands, as the text it always was. That is the same bargain the inbound
// webhook's storeAttachment makes, and for the same reason — a thread with no
// picture is a great deal better than an import that silently seeded nothing.
// Each refusal is logged, because a batch of two hundred openings quietly
// losing their file is not something to discover from a screenshot.
func (uc *SeedInboxUseCase) resolveAsset(in uw.SeedRequest) *seedAsset {
	if in.Script == nil || in.Script.Attachment == nil {
		return nil
	}
	attachment := in.Script.Attachment

	if uc.assets == nil || uc.attachments == nil {
		log.Printf("[unofficial-whatsapp] seed attachment %s skipped: media stores are not wired on this deployment",
			attachment.MediaID)
		return nil
	}

	asset, err := uc.assets.GetMediaByID(attachment.MediaID)
	if err != nil || asset == nil || strings.TrimSpace(asset.URL) == "" {
		log.Printf("[unofficial-whatsapp] seed attachment %s could not be resolved, seeding the opening plain: %v",
			attachment.MediaID, err)
		return nil
	}

	// The ownership check, and this is the only layer that can make it: the
	// batch arrives on a queue, which is not a request and carries no session
	// to check an id against. Refusing here is what stops a workspace seeding
	// another workspace's file into its own inbox.
	if asset.WorkspaceID != in.WorkspaceID {
		log.Printf("[unofficial-whatsapp] seed attachment %s belongs to another workspace, seeding the opening plain",
			attachment.MediaID)
		return nil
	}

	url := strings.TrimSpace(asset.URL)
	return &seedAsset{
		url: url,
		// Derived from the stored object's own extension, which is how the
		// storage layer decides the Content-Type it serves the file under. One
		// derivation, two places that must agree.
		mimeType:  mime.TypeByExtension(path.Ext(url)),
		fileName:  assetFileName(asset.Description, url),
		mediaType: conversationMediaType(attachment.Kind),
	}
}

// attach gives the opening message its file, minting this conversation's own
// media row.
//
// MessageTypeOperator is KEPT rather than swapped for MessageTypeMedia, and
// that is the whole judgement in this function. Inbound-ness is decided from
// the message TYPE in SQL — InboundMessageTypeStrings drives the unread count,
// CountInboundByEntry and the AI history — and MessageTypeMedia is in that set.
// Typing our own opening as media would file the company's first message under
// what the customer said. The renderer disagrees with none of this: the chat
// draws an attachment from media_id and media_type and never looks at the
// message type, so an operator message with a file on it renders exactly as a
// media bubble with the caption underneath.
//
// A row that cannot be written costs the file, never the message: the opening
// is appended by the caller either way.
func (uc *SeedInboxUseCase) attach(message *conversation.Message, asset *seedAsset, now time.Time) {
	if message == nil || asset == nil || uc.attachments == nil {
		return
	}

	row := &conversation.ConversationMedia{
		// Minted HERE, as every other writer of this table does. The repository
		// maps onto a separate schema struct, so a row created without an id
		// leaves this object's ID empty and the message below would link to "".
		ID:               uuid.NewString(),
		EntryID:          message.EntryID,
		EntryType:        shared.EntryTypeUnofficialWhatsApp,
		Type:             asset.mediaType,
		MimeType:         asset.mimeType,
		URL:              asset.url,
		OriginalFilename: asset.fileName,
		CreatedAt:        now.UTC(),
	}
	row.Normalize()
	if err := row.Validate(); err != nil {
		log.Printf("[unofficial-whatsapp] seed attachment for conversation %s is unusable: %v", message.EntryID, err)
		return
	}
	if err := uc.attachments.Create(row); err != nil {
		log.Printf("[unofficial-whatsapp] could not attach the seeded file to conversation %s: %v",
			message.EntryID, err)
		return
	}

	message.MediaID = &row.ID
	message.MediaType = asset.mediaType
}

// assetFileName is what a document bubble shows under the icon.
//
// The library's description first, because that is what the administrator typed
// when they uploaded the file; the stored object's basename otherwise, which is
// a uuid and ugly but still a name. Never empty: a document bubble with no
// label is a grey rectangle.
func assetFileName(description, url string) string {
	if name := strings.TrimSpace(description); name != "" {
		return name
	}
	return path.Base(url)
}
