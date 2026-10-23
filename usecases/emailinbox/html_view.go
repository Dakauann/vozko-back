package emailinbox

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/conversation"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/domain/media"
	"vozko/domain/shared"
)

var ErrHTMLViewIncomplete = errors.New("emailinbox: the email html view is missing a dependency")

type MessageReader interface {
	GetByID(id string) (*conversation.Message, error)
}

type OriginalRenderer interface {
	OriginalHTML(ctx context.Context, sent emailsending.Message, merge em.CampaignMerge) (string, error)
}

type HTMLViewDeps struct {
	Visibility conversation.Visibility
	Messages   MessageReader
	Media      conversation.EntryMediaReader
	Files      media.FileReader
	Sent       SentMessages
	Originals  OriginalRenderer
	Sanitizer  em.HTMLSanitizer
}

type HTMLViewer struct {
	d HTMLViewDeps
}

type HTMLRequest struct {
	Viewer      shared.Person
	WorkspaceID string
	EntryID     string
	MessageID   string
	ShowImages  bool
}

func NewHTMLViewer(d HTMLViewDeps) (*HTMLViewer, error) {
	if d.Visibility == nil || d.Messages == nil || d.Media == nil || d.Files == nil || d.Sent == nil || d.Originals == nil || d.Sanitizer == nil {
		return nil, ErrHTMLViewIncomplete
	}
	return &HTMLViewer{d: d}, nil
}

func (v *HTMLViewer) View(ctx context.Context, in HTMLRequest) (em.HTMLView, error) {
	if err := conversation.RequireVisible(v.d.Visibility, in.Viewer, in.WorkspaceID, in.EntryID, string(shared.EntryTypeEmail)); err != nil {
		return em.HTMLView{}, err
	}
	msg, err := v.d.Messages.GetByID(strings.TrimSpace(in.MessageID))
	if errors.Is(err, conversation.ErrMessageNotFound) || (err == nil && (msg == nil || msg.EntryID != in.EntryID || msg.EntryType != shared.EntryTypeEmail)) {
		return em.HTMLView{}, em.ErrNotAnEmail
	}
	if err != nil {
		return em.HTMLView{}, err
	}
	inbound := msg.ResolvedDirection() == conversation.MessageDirectionInbound
	view := em.HTMLView{Text: msg.Text, Inbound: inbound}
	raw, err := v.rawHTML(ctx, in.WorkspaceID, msg)
	if err != nil {
		log.Printf("%s the html of message %s is shown as text: %v", logPrefix, msg.ID, err)
		return view, nil
	}
	if raw == "" {
		return view, nil
	}
	clean, err := v.d.Sanitizer.Sanitize(raw, !inbound || in.ShowImages)
	if err != nil {
		log.Printf("%s the html of message %s could not be made safe and is shown as text: %v", logPrefix, msg.ID, err)
		return view, nil
	}
	view.HTML, view.HasHTML, view.ImagesBlocked = clean.HTML, true, clean.RemoteImagesBlocked
	return view, nil
}

func (v *HTMLViewer) rawHTML(ctx context.Context, workspaceID string, msg *conversation.Message) (string, error) {
	ref := em.HTMLRefOf(msg.Metadata)
	switch {
	case ref.Stored():
		return v.storedHTML(ctx, msg.EntryID, ref.MediaID)
	case ref.CampaignOriginal():
		sent, err := v.d.Sent.Get(ctx, ref.SentMessageID)
		if err != nil {
			return "", err
		}
		if sent.WorkspaceID != workspaceID || sent.RunID != ref.CampaignID {
			return "", fmt.Errorf("the sent message %s is not the campaign original of this workspace", ref.SentMessageID)
		}
		return v.d.Originals.OriginalHTML(ctx, *sent, *ref.Merge)
	}
	return "", nil
}

func (v *HTMLViewer) storedHTML(ctx context.Context, entryID, mediaID string) (string, error) {
	row, err := v.d.Media.GetInEntry(entryID, shared.EntryTypeEmail, mediaID)
	if err != nil {
		return "", err
	}
	if !row.BelongsTo(entryID, shared.EntryTypeEmail) || row.MimeType != em.HTMLMimeType {
		return "", fmt.Errorf("media %s is not the html of this email", mediaID)
	}
	key, ours := v.d.Files.KeyFromURL(row.URL)
	if !ours {
		return "", fmt.Errorf("media %s is not in our storage", mediaID)
	}
	data, _, err := v.d.Files.DownloadFile(ctx, key)
	if err != nil {
		return "", err
	}
	if len(data) > em.MaxStoredHTMLBytes {
		return "", em.ErrHTMLUnsafe
	}
	return string(data), nil
}
