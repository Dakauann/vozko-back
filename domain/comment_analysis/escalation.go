package comment_analysis

import (
	"fmt"
	"strings"
	"time"
)

// Forwarding a comment to someone who needs to see it (§3).
//
// "alguém comentou mal, encaminhar a mensagem e o @ de quem comentou."
//
// The formatting lives here, in the domain, for the reason every other derived
// value does: a message assembled at three call sites is three messages, and
// the one a customer forwards to their own boss must not depend on which button
// they pressed. The value object holds only what we ALREADY store; it fetches
// nothing and decides nothing about who receives it.

// MaxEscalationNote bounds the operator's own words. The note is the only part
// of the message a person types, so it is the only part that can be used to
// make the message say something we did not write; bounded and placed last, it
// cannot push our own text out of view.
const MaxEscalationNote = 500

// Escalation is one comment, formatted for a human somewhere else.
type Escalation struct {
	WorkspaceID string
	Source      Source
	CommentID   string

	AuthorHandle     string
	AuthorExternalID string

	ContainerID string
	Permalink   string

	Excerpt  string
	Stance   Stance
	Severity int
	// Measured is false for a comment that was never classified. It can still
	// be forwarded (often that is exactly why), but the message must not print
	// an unmeasured severity as if it were a zero score.
	Measured    bool
	CommentedAt time.Time

	Note string
}

// NewEscalation builds the value object from a stored row plus the two things
// that are not on it: the post's public link, and the operator's note.
func NewEscalation(c *CommentAnalysis, permalink, note string) Escalation {
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
		CommentedAt:      c.CommentedAt,
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

// Author is the handle when we have one, and the channel's id when we do not.
// A recipient who cannot tell WHO they are being warned about has been sent
// nothing useful.
func (e Escalation) Author() string {
	if e.AuthorHandle != "" {
		return "@" + e.AuthorHandle
	}
	return e.AuthorExternalID
}

// Where names the post: its public link when we have one, its channel id
// otherwise, so the line is never empty.
func (e Escalation) Where() string {
	if e.Permalink != "" {
		return e.Permalink
	}
	return e.ContainerID
}

// Message is the text the recipient reads.
//
// The forwarded comment is quoted rather than run into our own sentences, so a
// comment that itself looks like an instruction reads as somebody's words and
// not as ours.
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

	if !e.CommentedAt.IsZero() {
		b.WriteString("Quando: ")
		b.WriteString(e.CommentedAt.UTC().Format("02/01/2006 15:04"))
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
