package teamchat_repository

import (
	"time"

	"github.com/lib/pq"

	"vozko/domain/teamchat"
	"vozko/infra/database/schema"
)

func threadRowOf(t teamchat.Thread) schema.TeamChatThread {
	return schema.TeamChatThread{
		ID:            t.ID,
		WorkspaceID:   t.WorkspaceID,
		Kind:          string(t.Kind),
		Title:         t.Title,
		DirectKey:     t.DirectKey,
		CreatedBy:     t.CreatedBy,
		LastSeq:       t.LastSeq,
		LastMessageAt: t.LastMessageAt.UTC(),
		CreatedAt:     t.CreatedAt.UTC(),
	}
}

func threadOf(row schema.TeamChatThread) teamchat.Thread {
	return teamchat.Thread{
		ID:            row.ID,
		WorkspaceID:   row.WorkspaceID,
		Kind:          teamchat.Kind(row.Kind),
		Title:         row.Title,
		DirectKey:     row.DirectKey,
		CreatedBy:     row.CreatedBy,
		LastSeq:       row.LastSeq,
		LastMessageAt: row.LastMessageAt.UTC(),
		CreatedAt:     row.CreatedAt.UTC(),
	}
}

func memberRowOf(m teamchat.Member) schema.TeamChatMember {
	return schema.TeamChatMember{
		ThreadID:    m.ThreadID,
		UserID:      m.UserID,
		WorkspaceID: m.WorkspaceID,
		Role:        string(m.Role),
		LastReadSeq: m.LastReadSeq,
		Muted:       m.Muted,
		JoinedAt:    m.JoinedAt.UTC(),
		LeftAt:      utcPointer(m.LeftAt),
	}
}

func memberOf(row schema.TeamChatMember) teamchat.Member {
	return teamchat.Member{
		ThreadID:    row.ThreadID,
		WorkspaceID: row.WorkspaceID,
		UserID:      row.UserID,
		Role:        teamchat.Role(row.Role),
		LastReadSeq: row.LastReadSeq,
		Muted:       row.Muted,
		JoinedAt:    row.JoinedAt.UTC(),
		LeftAt:      utcPointer(row.LeftAt),
	}
}

func messageRowOf(m teamchat.Message) schema.TeamChatMessage {
	return schema.TeamChatMessage{
		ID:          m.ID,
		ThreadID:    m.ThreadID,
		WorkspaceID: m.WorkspaceID,
		Seq:         m.Seq,
		AuthorID:    m.AuthorID,
		Kind:        string(m.Kind),
		Body:        m.Body,
		Event:       string(m.Event),
		SubjectIDs:  stringArray(m.SubjectIDs),
		ReplyToID:   optionalID(m.ReplyToID),
		Mentions:    stringArray(m.Mentions),
		ClientID:    m.ClientID,
		CreatedAt:   m.CreatedAt.UTC(),
		EditedAt:    utcPointer(m.EditedAt),
		DeletedAt:   utcPointer(m.DeletedAt),
	}
}

func messageOf(row schema.TeamChatMessage) teamchat.Message {
	return teamchat.Message{
		ID:          row.ID,
		ThreadID:    row.ThreadID,
		WorkspaceID: row.WorkspaceID,
		Seq:         row.Seq,
		AuthorID:    row.AuthorID,
		Kind:        teamchat.MessageKind(row.Kind),
		Body:        row.Body,
		Event:       teamchat.Event(row.Event),
		SubjectIDs:  idList(row.SubjectIDs),
		ReplyToID:   valueOf(row.ReplyToID),
		Mentions:    idList(row.Mentions),
		ClientID:    row.ClientID,
		CreatedAt:   row.CreatedAt.UTC(),
		EditedAt:    utcPointer(row.EditedAt),
		DeletedAt:   utcPointer(row.DeletedAt),
	}
}

func scheduledRowOf(s teamchat.Scheduled) schema.TeamChatScheduledMessage {
	return schema.TeamChatScheduledMessage{
		ID:            s.ID,
		WorkspaceID:   s.WorkspaceID,
		ThreadID:      s.ThreadID,
		AuthorID:      s.AuthorID,
		Body:          s.Body,
		Mentions:      stringArray(s.Mentions),
		ReplyToID:     optionalID(s.ReplyToID),
		SendAt:        s.SendAt.UTC(),
		Status:        string(s.Status),
		FailureReason: string(s.FailureReason),
		SentMessageID: optionalID(s.SentMessageID),
		CreatedAt:     s.CreatedAt.UTC(),
		UpdatedAt:     s.UpdatedAt.UTC(),
	}
}

func scheduledOf(row schema.TeamChatScheduledMessage) teamchat.Scheduled {
	return teamchat.Scheduled{
		ID:            row.ID,
		WorkspaceID:   row.WorkspaceID,
		ThreadID:      row.ThreadID,
		AuthorID:      row.AuthorID,
		Body:          row.Body,
		Mentions:      idList(row.Mentions),
		ReplyToID:     valueOf(row.ReplyToID),
		SendAt:        row.SendAt.UTC(),
		Status:        teamchat.ScheduledStatus(row.Status),
		FailureReason: teamchat.FailureReason(row.FailureReason),
		SentMessageID: valueOf(row.SentMessageID),
		CreatedAt:     row.CreatedAt.UTC(),
		UpdatedAt:     row.UpdatedAt.UTC(),
	}
}

func scheduledList(rows []schema.TeamChatScheduledMessage) []teamchat.Scheduled {
	out := make([]teamchat.Scheduled, 0, len(rows))
	for _, row := range rows {
		out = append(out, scheduledOf(row))
	}
	return out
}

func stringArray(values []string) pq.StringArray {
	if values == nil {
		return pq.StringArray{}
	}
	return pq.StringArray(values)
}

func idList(values pq.StringArray) []string {
	if values == nil {
		return []string{}
	}
	return []string(values)
}

func optionalID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

func valueOf(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

func utcPointer(at *time.Time) *time.Time {
	if at == nil {
		return nil
	}
	utc := at.UTC()
	return &utc
}
