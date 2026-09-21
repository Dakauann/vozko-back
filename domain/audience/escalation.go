package audience

import (
	"fmt"
	"strings"
	"time"
)

const MaxEscalationNote = 500

type Escalation struct {
	WorkspaceID string
	Source      Source
	CommentID   string

	AuthorHandle     string
	AuthorExternalID string

	ContainerID string
	Permalink   string

	Excerpt    string
	Stance     Stance
	Severity   int
	Measured   bool
	OccurredAt time.Time

	Note string
}

func NewEscalation(c *Analysis, permalink, note string) Escalation {
	note = strings.TrimSpace(note)
	if len(note) > MaxEscalationNote {
		note = strings.TrimSpace(note[:MaxEscalationNote])
	}
	return Escalation{
		WorkspaceID:      strings.TrimSpace(c.WorkspaceID),
		Source:           c.Source,
		CommentID:        c.ID,
		AuthorHandle:     strings.TrimSpace(c.AuthorHandle),
		AuthorExternalID: strings.TrimSpace(c.AuthorExternalID),
		ContainerID:      c.ContainerID,
		Permalink:        strings.TrimSpace(permalink),
		Excerpt:          strings.TrimSpace(c.Excerpt),
		Stance:           c.Stance,
		Severity:         c.Severity,
		Measured:         c.Status == StatusAnalyzed,
		OccurredAt:       c.OccurredAt,
		Note:             note,
	}
}

func (e Escalation) Validate() error {
	if e.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidFilter)
	}
	if e.Excerpt == "" {
		return fmt.Errorf("%w: there is nothing to forward", ErrInvalidFilter)
	}
	if e.Author() == "" {
		return fmt.Errorf("%w: the comment has no identifiable author", ErrInvalidFilter)
	}
	return nil
}

func (e Escalation) Author() string {
	if e.AuthorHandle != "" {
		return "@" + e.AuthorHandle
	}
	return e.AuthorExternalID
}

func (e Escalation) Where() string {
	if e.Permalink != "" {
		return e.Permalink
	}
	return e.ContainerID
}

func (e Escalation) Message() string {
	var b strings.Builder
	b.WriteString("Comentário sinalizado")
	if e.Source != "" {
		b.WriteString(" no ")
		b.WriteString(sourceLabel(e.Source))
	}
	b.WriteString("\n\n")

	b.WriteString("Autor: ")
	b.WriteString(e.Author())
	b.WriteString("\n")

	if e.Measured {
		b.WriteString("Gravidade: ")
		b.WriteString(fmt.Sprintf("%d", e.Severity))
		if e.Stance != "" {
			b.WriteString(" (")
			b.WriteString(stanceLabel(e.Stance))
			b.WriteString(")")
		}
		b.WriteString("\n")
	}

	if !e.OccurredAt.IsZero() {
		b.WriteString("Quando: ")
		b.WriteString(e.OccurredAt.UTC().Format("02/01/2006 15:04"))
		b.WriteString(" UTC\n")
	}

	b.WriteString("Post: ")
	b.WriteString(e.Where())
	b.WriteString("\n\nComentário:\n\"")
	b.WriteString(e.Excerpt)
	b.WriteString("\"")

	if e.Note != "" {
		b.WriteString("\n\n")
		b.WriteString(e.Note)
	}
	return b.String()
}

func sourceLabel(s Source) string {
	if s == SourceInstagram {
		return "Instagram"
	}
	return string(s)
}

func stanceLabel(s Stance) string {
	switch s {
	case StanceSupporter:
		return "simpatizante"
	case StanceNeutral:
		return "neutro"
	case StanceCritic:
		return "crítico"
	case StanceHostile:
		return "hostil"
	}
	return string(s)
}
