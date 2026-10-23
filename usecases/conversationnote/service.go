package conversationnote_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/conversationnote"
	"vozko/domain/notification"
	"vozko/domain/shared"
	"vozko/domain/user"
	"vozko/domain/workspace"
)

const unknownAuthorName = "Alguém da equipe"

type Access interface {
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
	CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool
	IsWorkspaceMember(userID, workspaceID string) bool
}

type People interface {
	FindByIDs(ids []string) ([]*user.User, error)
}

type Announcer interface {
	AnnounceNote(note conversationnote.Authored)
	AnnounceNoteDeleted(note conversationnote.Note)
}

type EntryContacts interface {
	GetEntryInfo(entryID, entryType string) (conversation.EntryInfo, error)
}

type Media interface {
	ListByIDs(ids []string) ([]*conversation.ConversationMedia, error)
}

type Deps struct {
	Notes     conversationnote.Repository
	Access    Access
	People    People
	Announcer Announcer
	Notifier  notification.Publisher
	Contacts  EntryContacts
	Media     Media
	Uploader  conversation.UploadConversationMediaUseCase
	Now       func() time.Time
	NewID     func() string
}

type Service struct {
	deps Deps
}

var _ conversationnote.UseCases = (*Service)(nil)

func New(deps Deps) (*Service, error) {
	missing := []string{}
	for name, absent := range map[string]bool{
		"notes":          deps.Notes == nil,
		"access":         deps.Access == nil,
		"people":         deps.People == nil,
		"announcer":      deps.Announcer == nil,
		"notifier":       deps.Notifier == nil,
		"entry contacts": deps.Contacts == nil,
		"media":          deps.Media == nil,
		"media uploader": deps.Uploader == nil,
	} {
		if absent {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return nil, fmt.Errorf("conversation notes: missing %s", strings.Join(missing, ", "))
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	if deps.NewID == nil {
		deps.NewID = uuid.NewString
	}
	return &Service{deps: deps}, nil
}

func (s *Service) List(ctx context.Context, by shared.Person, q conversationnote.PageQuery) (conversationnote.Page, error) {
	if !s.canSee(by, q.WorkspaceID, q.Entry) {
		return conversationnote.Page{}, conversation.ErrUnauthorized
	}
	limit := conversationnote.PageLimit(q.Limit)
	q.Limit = limit + 1
	notes, err := s.deps.Notes.ListByEntry(ctx, q)
	if err != nil {
		return conversationnote.Page{}, err
	}
	hasMore := len(notes) > limit
	if hasMore {
		notes = notes[:limit]
	}
	media, err := s.mediaOf(notes)
	if err != nil {
		return conversationnote.Page{}, err
	}
	return conversationnote.Page{Notes: s.authored(notes, media), HasMore: hasMore}, nil
}

func (s *Service) Create(ctx context.Context, by shared.Person, d conversationnote.Draft) (conversationnote.Authored, error) {
	if !s.canSee(by, d.WorkspaceID, d.Entry) {
		return conversationnote.Authored{}, conversation.ErrUnauthorized
	}
	mentions, err := conversationnote.NormalizeMentions(d.Mentions)
	if err != nil {
		return conversationnote.Authored{}, err
	}
	imageIDs, err := conversationnote.NormalizeImageIDs(d.ImageIDs)
	if err != nil {
		return conversationnote.Authored{}, err
	}
	media, err := s.attachable(d.Entry, imageIDs)
	if err != nil {
		return conversationnote.Authored{}, err
	}
	d.AuthorID = by.UserID
	d.Mentions = s.workspaceMembers(d.WorkspaceID, mentions)
	d.ImageIDs = imageIDs
	note, err := conversationnote.New(d, s.deps.NewID(), s.deps.Now())
	if err != nil {
		return conversationnote.Authored{}, err
	}
	if err := s.deps.Notes.Create(ctx, note); err != nil {
		return conversationnote.Authored{}, err
	}
	written := s.authored([]conversationnote.Note{note}, media)[0]
	s.deps.Announcer.AnnounceNote(written)
	s.notifyMentioned(context.WithoutCancel(ctx), written)
	return written, nil
}

func (s *Service) AttachImage(_ context.Context, by shared.Person, in conversationnote.ImageUpload) (conversationnote.Image, error) {
	if !in.Entry.EntryType.SupportsConversationView() || !s.canSee(by, in.WorkspaceID, in.Entry) {
		return conversationnote.Image{}, conversation.ErrUnauthorized
	}
	sniffed, err := conversationnote.SniffImage(in.Data)
	if err != nil {
		return conversationnote.Image{}, err
	}
	stored, err := s.deps.Uploader.Execute(conversation.UploadMediaInput{
		EntryID:   strings.TrimSpace(in.Entry.EntryID),
		EntryType: string(in.Entry.EntryType),
		MediaType: conversation.MediaTypeImage,
		Filename:  sniffed.FileName(in.FileName),
		Data:      in.Data,
		MimeType:  sniffed.MIME,
		Purpose:   conversation.MediaPurposeNote,
	})
	if errors.Is(err, conversation.ErrMediaURLRequired) {
		return conversationnote.Image{}, conversationnote.ErrStorageMissing
	}
	if err != nil {
		return conversationnote.Image{}, err
	}
	if stored == nil {
		return conversationnote.Image{}, conversationnote.ErrStorageMissing
	}
	return conversationnote.ImageOf(stored), nil
}

func (s *Service) attachable(entry shared.EntryRef, ids []string) ([]*conversation.ConversationMedia, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	media, err := s.deps.Media.ListByIDs(ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*conversation.ConversationMedia, len(media))
	for _, m := range media {
		if m != nil {
			byID[m.ID] = m
		}
	}
	for _, id := range ids {
		if err := conversationnote.Attachable(byID[id], entry); err != nil {
			return nil, err
		}
	}
	return media, nil
}

func (s *Service) mediaOf(notes []conversationnote.Note) ([]*conversation.ConversationMedia, error) {
	var ids []string
	for _, note := range notes {
		ids = append(ids, note.ImageIDs...)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return s.deps.Media.ListByIDs(ids)
}

func (s *Service) Delete(ctx context.Context, by shared.Person, workspaceID, noteID string) error {
	note, err := s.deps.Notes.Find(ctx, workspaceID, strings.TrimSpace(noteID))
	if err != nil {
		return err
	}
	if !s.canSee(by, workspaceID, note.Entry()) {
		return conversationnote.ErrNotFound
	}
	if err := note.DeletableBy(by.UserID); err != nil {
		return err
	}
	if err := s.deps.Notes.MarkDeleted(ctx, workspaceID, note.ID, s.deps.Now()); err != nil {
		return err
	}
	s.deps.Announcer.AnnounceNoteDeleted(note)
	return nil
}

func (s *Service) canSee(by shared.Person, workspaceID string, entry shared.EntryRef) bool {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(entry.EntryID) == "" {
		return false
	}
	if !s.deps.Access.HasWorkspacePermission(by.UserID, workspaceID, string(workspace.ResourceConversations), string(workspace.ActionRead), by.SystemAdmin) {
		return false
	}
	return by.MayActOn(s.deps.Access, workspaceID, entry.EntryID, string(entry.EntryType))
}

func (s *Service) workspaceMembers(workspaceID string, ids []string) []string {
	members := make([]string, 0, len(ids))
	for _, id := range ids {
		if s.deps.Access.IsWorkspaceMember(id, workspaceID) {
			members = append(members, id)
		}
	}
	return members
}

func (s *Service) authored(notes []conversationnote.Note, media []*conversation.ConversationMedia) []conversationnote.Authored {
	names := s.namesOf(notes)
	out := make([]conversationnote.Authored, 0, len(notes))
	for _, note := range notes {
		name := names[note.AuthorID]
		if name == "" {
			name = unknownAuthorName
		}
		out = append(out, conversationnote.Authored{
			Note:       note,
			AuthorName: name,
			Images:     conversationnote.AttachedImages(note.ImageIDs, media, note.Entry()),
		})
	}
	return out
}

func (s *Service) namesOf(notes []conversationnote.Note) map[string]string {
	ids := make([]string, 0, len(notes))
	seen := make(map[string]bool, len(notes))
	for _, note := range notes {
		if !seen[note.AuthorID] {
			seen[note.AuthorID] = true
			ids = append(ids, note.AuthorID)
		}
	}
	names := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return names
	}
	people, err := s.deps.People.FindByIDs(ids)
	if err != nil {
		log.Printf("[conversation-notes] resolving author names failed: %v", err)
		return names
	}
	for _, person := range people {
		if person != nil {
			names[person.ID] = strings.TrimSpace(person.Username)
		}
	}
	return names
}

func (s *Service) notifyMentioned(ctx context.Context, note conversationnote.Authored) {
	var contact string
	for _, userID := range note.Mentions {
		if userID == note.AuthorID || !s.canSee(shared.Person{UserID: userID}, note.WorkspaceID, note.Entry()) {
			continue
		}
		if contact == "" {
			contact = s.contactLabel(note.Entry())
		}
		if _, err := s.deps.Notifier.Publish(ctx, mentionNotice(note, userID, contact)); err != nil {
			log.Printf("[conversation-notes] notifying %s of a mention in note %s failed: %v", userID, note.ID, err)
		}
	}
}

func (s *Service) contactLabel(entry shared.EntryRef) string {
	info, err := s.deps.Contacts.GetEntryInfo(entry.EntryID, string(entry.EntryType))
	if err != nil {
		return conversation.UnknownContactLabel
	}
	return info.ContactLabel()
}

func mentionNotice(note conversationnote.Authored, userID, contact string) notification.Draft {
	return notification.Draft{
		WorkspaceID: note.WorkspaceID,
		Category:    notification.CategoryConversations,
		Severity:    notification.SeverityInfo,
		Title:       notification.ClipTitle(note.AuthorName + " mencionou você na conversa com " + contact),
		Body:        note.AuthorName + " mencionou você em uma nota interna na conversa com " + contact + ". Abra a conversa para ler.",
		ActionURL:   conversation.DashboardLink(note.Entry()),
		ActionLabel: "Abrir conversa",
		Audience:    notification.Users(userID),
		DedupKey:    "conversation_note_mention:" + note.ID + ":" + userID,
		Source:      notification.SourceConversationNoteMention,
	}
}
