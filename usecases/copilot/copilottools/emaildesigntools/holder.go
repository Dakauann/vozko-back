package emaildesigntools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"vozko/domain/copilot"
	"vozko/domain/emailcampaign"
	"vozko/domain/emaildesign"
	emailcampaign_usecase "vozko/usecases/emailcampaign"
)

type Campaigns interface {
	Get(ctx context.Context, a emailcampaign_usecase.Actor, campaignID string) (*emailcampaign.Campaign, error)
	ReviseContent(ctx context.Context, a emailcampaign_usecase.Actor, campaignID string, content emailcampaign.Content) (*emailcampaign.Campaign, error)
}

const (
	emailCampaignGone      = "a campanha aberta não foi encontrada neste workspace; peça ao usuário para reabrir a campanha"
	emailCampaignLocked    = "a campanha está %s e o conteúdo dela não muda mais; só um rascunho ou uma campanha pronta aceita edição"
	emailCampaignScheduled = "a campanha está agendada e o conteúdo dela não muda por aqui; para editar, o usuário desagenda a campanha, que volta a rascunho"
	emailCampaignMoved     = "as mudanças entraram na tela, mas a campanha mudou de status enquanto você editava e não guardou esta edição; avise o usuário e confira com email_read"
	emailCampaignNotKept   = "as mudanças entraram na tela, mas não foram guardadas na campanha agora; o rascunho se salva sozinho na tela do usuário. Confira com email_read antes de seguir"
	emailSubjectNoCampaign = "subject só existe no conteúdo de uma campanha; um design não tem assunto"
)

var statusWords = map[emailcampaign.Status]string{
	emailcampaign.StatusRunning:   "em envio",
	emailcampaign.StatusPaused:    "pausada",
	emailcampaign.StatusHeld:      "retida",
	emailcampaign.StatusCompleted: "concluída",
	emailcampaign.StatusStopped:   "interrompida",
}

type purpose int

const (
	forReading purpose = iota
	forEditing
)

type envelope struct {
	Subject   string `json:"subject"`
	Preheader string `json:"preheader"`
}

type envelopeEdit struct {
	Base envelope `json:"base"`
	Next envelope `json:"next"`
}

type holder interface {
	carriesEnvelope() bool
	facts() map[string]interface{}
	keep(ctx context.Context, cc copilot.Context, before, after liveDesign) (map[string]interface{}, error)
}

type designHolder struct{}

func (designHolder) carriesEnvelope() bool { return false }

func (designHolder) facts() map[string]interface{} { return nil }

func (designHolder) keep(context.Context, copilot.Context, liveDesign, liveDesign) (map[string]interface{}, error) {
	return nil, nil
}

type campaignHolder struct {
	campaigns Campaigns
	campaign  emailcampaign.Campaign
}

func (campaignHolder) carriesEnvelope() bool { return true }

func (h campaignHolder) facts() map[string]interface{} {
	return map[string]interface{}{"campaign_status": h.campaign.Status}
}

func (h campaignHolder) keep(ctx context.Context, cc copilot.Context, before, after liveDesign) (map[string]interface{}, error) {
	if after.same(before) {
		return h.facts(), nil
	}
	content := emailcampaign.Content{Subject: after.Envelope.Subject, Preheader: after.Envelope.Preheader, Document: after.Document}
	kept, err := h.campaigns.ReviseContent(ctx, actorOf(cc), h.campaign.ID, content)
	switch {
	case errors.Is(err, emailcampaign.ErrNotRevisable), errors.Is(err, emailcampaign.ErrNotEditable):
		return nil, errors.New(emailCampaignMoved)
	case err != nil || kept == nil:
		return nil, errors.New(emailCampaignNotKept)
	}
	facts := map[string]interface{}{"campaign_status": kept.Status}
	if h.campaign.Status == emailcampaign.StatusReady && kept.Status == emailcampaign.StatusDraft {
		facts["returned_to_draft"] = true
	}
	return facts, nil
}

func lockedMessage(status emailcampaign.Status) string {
	if status == emailcampaign.StatusScheduled {
		return emailCampaignScheduled
	}
	word := statusWords[status]
	if word == "" {
		word = string(status)
	}
	return fmt.Sprintf(emailCampaignLocked, word)
}

func (d Deps) openHolder(ctx context.Context, cc copilot.Context, use purpose) (holder, error) {
	if !cc.View.OnEmailDocument() {
		return nil, errors.New(emailNotHere)
	}
	if cc.Screen == nil {
		return nil, errors.New(emailNoEditor)
	}
	if cc.View.OnEmailCampaign() {
		return d.openCampaign(ctx, cc, use)
	}
	if d.Designs == nil {
		return nil, errors.New(emailDesignGone)
	}
	design, err := d.Designs.GetDesign(ctx, actorOf(cc), cc.View.ProjectID)
	if err != nil || design == nil {
		return nil, errors.New(emailDesignGone)
	}
	return designHolder{}, nil
}

func (d Deps) openCampaign(ctx context.Context, cc copilot.Context, use purpose) (holder, error) {
	if d.Campaigns == nil {
		return nil, errors.New(emailCampaignGone)
	}
	campaign, err := d.Campaigns.Get(ctx, actorOf(cc), cc.View.ProjectID)
	if err != nil || campaign == nil || campaign.WorkspaceID != cc.WorkspaceID {
		return nil, errors.New(emailCampaignGone)
	}
	if use == forEditing && !campaign.Status.ContentRevisable() {
		return nil, errors.New(lockedMessage(campaign.Status))
	}
	return campaignHolder{campaigns: d.Campaigns, campaign: *campaign}, nil
}

func (l liveDesign) content() emailcampaign.Content {
	if l.Envelope == nil {
		return emailcampaign.Content{Document: l.Document}
	}
	return emailcampaign.Content{Subject: l.Envelope.Subject, Preheader: l.Envelope.Preheader, Document: l.Document}
}

func (l liveDesign) shown() emaildesign.Document {
	return l.content().RenderDocument()
}

func (l liveDesign) gaps() []emaildesign.Gap {
	gaps := emaildesign.OpenGaps(l.shown())
	if l.Envelope != nil {
		gaps = append(gaps, emaildesign.SubjectGaps(l.Envelope.Subject)...)
	}
	return gaps
}

func (l liveDesign) review() emaildesign.DesignReview {
	return emaildesign.Review(l.shown(), l.content().Subject)
}

func (l liveDesign) same(other liveDesign) bool {
	left, errLeft := json.Marshal(l.content())
	right, errRight := json.Marshal(other.content())
	return errLeft == nil && errRight == nil && string(left) == string(right)
}
