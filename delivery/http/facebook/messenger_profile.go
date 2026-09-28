package facebook

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	fbdomain "vozko/domain/facebook"
	"vozko/infra/http/middleware"
)

type ProfileText struct {
	Locale string `json:"locale"`
	Text   string `json:"text"`
}

type ProfilePayload struct {
	Payload string `json:"payload"`
}

type ProfileQuestion struct {
	Question string `json:"question"`
	Payload  string `json:"payload"`
}

type ProfileIceBreakers struct {
	Locale string            `json:"locale"`
	Items  []ProfileQuestion `json:"items"`
}

type ProfileMenuItem struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Payload string `json:"payload,omitempty"`
	URL     string `json:"url,omitempty"`
}

type ProfileMenu struct {
	Locale                string            `json:"locale"`
	ComposerInputDisabled bool              `json:"composerInputDisabled"`
	Items                 []ProfileMenuItem `json:"items"`
}

type ProfileBody struct {
	Greeting       []ProfileText        `json:"greeting"`
	GetStarted     *ProfilePayload      `json:"getStarted"`
	IceBreakers    []ProfileIceBreakers `json:"iceBreakers"`
	PersistentMenu []ProfileMenu        `json:"persistentMenu"`
}

type StoryResponse struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	MediaType   string     `json:"mediaType"`
	CreatedTime *time.Time `json:"createdTime,omitempty"`
	URL         string     `json:"url,omitempty"`
}

func (b ProfileBody) toDomain() fbdomain.MessengerProfile {
	out := fbdomain.MessengerProfile{}
	for _, g := range b.Greeting {
		out.Greeting = append(out.Greeting, fbdomain.LocalizedText{Locale: g.Locale, Text: g.Text})
	}
	if b.GetStarted != nil {
		out.GetStarted = &fbdomain.GetStarted{Payload: b.GetStarted.Payload}
	}
	for _, set := range b.IceBreakers {
		items := make([]fbdomain.IceBreaker, 0, len(set.Items))
		for _, q := range set.Items {
			items = append(items, fbdomain.IceBreaker{Question: q.Question, Payload: q.Payload})
		}
		out.IceBreakers = append(out.IceBreakers, fbdomain.IceBreakerSet{Locale: set.Locale, Items: items})
	}
	for _, m := range b.PersistentMenu {
		items := make([]fbdomain.MenuItem, 0, len(m.Items))
		for _, item := range m.Items {
			items = append(items, fbdomain.MenuItem{Type: fbdomain.MenuItemType(item.Type), Title: item.Title, Payload: item.Payload, URL: item.URL})
		}
		out.PersistentMenu = append(out.PersistentMenu, fbdomain.PersistentMenu{Locale: m.Locale, ComposerInputDisabled: m.ComposerInputDisabled, Items: items})
	}
	return out
}

func presentProfile(p fbdomain.MessengerProfile) ProfileBody {
	out := ProfileBody{
		Greeting: []ProfileText{}, IceBreakers: []ProfileIceBreakers{}, PersistentMenu: []ProfileMenu{},
	}
	for _, g := range p.Greeting {
		out.Greeting = append(out.Greeting, ProfileText{Locale: g.Locale, Text: g.Text})
	}
	if p.GetStarted != nil {
		out.GetStarted = &ProfilePayload{Payload: p.GetStarted.Payload}
	}
	for _, set := range p.IceBreakers {
		items := make([]ProfileQuestion, 0, len(set.Items))
		for _, q := range set.Items {
			items = append(items, ProfileQuestion{Question: q.Question, Payload: q.Payload})
		}
		out.IceBreakers = append(out.IceBreakers, ProfileIceBreakers{Locale: set.Locale, Items: items})
	}
	for _, m := range p.PersistentMenu {
		items := make([]ProfileMenuItem, 0, len(m.Items))
		for _, item := range m.Items {
			items = append(items, ProfileMenuItem{Type: string(item.Type), Title: item.Title, Payload: item.Payload, URL: item.URL})
		}
		out.PersistentMenu = append(out.PersistentMenu, ProfileMenu{Locale: m.Locale, ComposerInputDisabled: m.ComposerInputDisabled, Items: items})
	}
	return out
}

func (h *Handler) GetMessengerProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := h.profile.Get(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err, "Failed to read the Messenger profile")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentProfile(*profile))
}

func (h *Handler) UpdateMessengerProfile(w http.ResponseWriter, r *http.Request) {
	var body ProfileBody
	if !decodeJSON(w, r, &body) {
		return
	}
	profile := body.toDomain()
	if err := h.profile.Update(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"], profile); err != nil {
		writeDomainError(w, err, "Failed to update the Messenger profile")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentProfile(profile))
}

func (h *Handler) ListStories(w http.ResponseWriter, r *http.Request) {
	stories, err := h.posts.Stories(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err, "Failed to list stories")
		return
	}
	items := make([]StoryResponse, 0, len(stories))
	for _, s := range stories {
		items = append(items, StoryResponse{ID: s.PostID, Status: s.Status, MediaType: s.MediaType, CreatedTime: s.CreatedTime, URL: s.URL})
	}
	response.WriteSuccess(w, http.StatusOK, map[string]any{"items": items})
}
