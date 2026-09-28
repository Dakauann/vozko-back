package conversation

import (
	"fmt"
	"strings"
)

type DroppedOption struct {
	ID     string
	Reason string
}

func FitOptions(options []InteractiveOption, maxOptions, maxPayloadBytes int) ([]InteractiveOption, []DroppedOption) {
	kept := make([]InteractiveOption, 0, min(len(options), maxOptions))
	var dropped []DroppedOption
	for _, opt := range options {
		id := strings.TrimSpace(opt.ID)
		title := strings.TrimSpace(opt.Title)
		if title == "" {
			title = id
		}
		switch {
		case id == "":
			dropped = append(dropped, DroppedOption{ID: opt.Title, Reason: "no id to send back when chosen"})
		case maxPayloadBytes > 0 && len(id) > maxPayloadBytes:
			dropped = append(dropped, DroppedOption{ID: id, Reason: fmt.Sprintf(
				"id is %d bytes, over the %d-byte limit", len(id), maxPayloadBytes)})
		case len(kept) >= maxOptions:
			dropped = append(dropped, DroppedOption{ID: id, Reason: fmt.Sprintf("beyond the %d-option limit", maxOptions)})
		default:
			kept = append(kept, InteractiveOption{ID: id, Title: title})
		}
	}
	return kept, dropped
}

func (r SendInteractiveRequest) ComposedBody() string {
	parts := make([]string, 0, 3)
	for _, part := range []string{r.Header, r.Body, r.Footer} {
		if p := strings.TrimSpace(part); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "\n\n")
}
