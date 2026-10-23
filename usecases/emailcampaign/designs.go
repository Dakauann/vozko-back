package emailcampaign_usecase

import (
	"context"

	"vozko/domain/emailcampaign"
	"vozko/domain/emaildesign"
)

func (s *Service) CreateDesign(ctx context.Context, a Actor, in emailcampaign.DesignInput) (*emailcampaign.Design, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	d := &emailcampaign.Design{WorkspaceID: a.WorkspaceID, CreatedBy: a.UserID}
	d.Apply(in)
	if err := s.deps.Designs.Create(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Service) GetDesign(ctx context.Context, a Actor, designID string) (*emailcampaign.Design, error) {
	return s.deps.Designs.Get(ctx, a.WorkspaceID, designID)
}

func (s *Service) UpdateDesign(ctx context.Context, a Actor, designID string, in emailcampaign.DesignInput) (*emailcampaign.Design, error) {
	d, err := s.deps.Designs.Get(ctx, a.WorkspaceID, designID)
	if err != nil {
		return nil, err
	}
	if err := in.Validate(); err != nil {
		return nil, err
	}
	d.Apply(in)
	if err := s.deps.Designs.Update(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Service) DeleteDesign(ctx context.Context, a Actor, designID string) error {
	return s.deps.Designs.Delete(ctx, a.WorkspaceID, designID)
}

func (s *Service) ListDesigns(ctx context.Context, a Actor, kind emailcampaign.DesignKind, offset, limit int) ([]emailcampaign.Design, int64, error) {
	if kind != "" && !kind.Valid() {
		return nil, 0, emailcampaign.ErrDesignKindInvalid
	}
	return s.deps.Designs.List(ctx, a.WorkspaceID, kind, offset, limit)
}

func (s *Service) Starters() []emaildesign.Starter {
	return emaildesign.StarterDocuments()
}

type PreviewInput struct {
	Document emaildesign.Document
	Subject  string
	Sample   *emaildesign.Personalization
	Annotate bool
	Draft    bool
}

type Preview struct {
	Subject string             `json:"subject"`
	HTML    string             `json:"html"`
	Text    string             `json:"text"`
	Hints   []emaildesign.Hint `json:"hints"`
}

func SamplePersonalization() emaildesign.Personalization {
	return emaildesign.Personalization{FirstName: "Maria", Name: "Maria Oliveira", Email: "maria@exemplo.com.br", Company: "Exemplo"}
}

func (s *Service) Render(in PreviewInput) (Preview, error) {
	sample := SamplePersonalization()
	if in.Sample != nil {
		sample = *in.Sample
	}
	out := Preview{Hints: emaildesign.Lint(in.Document, in.Subject)}
	if out.Hints == nil {
		out.Hints = []emaildesign.Hint{}
	}
	if in.Subject != "" {
		line, err := emaildesign.RenderSubject(in.Subject, sample)
		if err != nil {
			return Preview{}, err
		}
		out.Subject = line
	}
	rendered, err := emaildesign.Render(in.Document, emaildesign.RenderInput{
		Personalization:  sample,
		Subject:          in.Subject,
		UnsubscribeURL:   s.previewURL,
		UnsubscribeLabel: unsubscribeLabel,
		Annotate:         in.Annotate,
		Draft:            in.Draft,
	})
	if err != nil {
		return Preview{}, err
	}
	out.HTML, out.Text = rendered.HTML, rendered.Text
	return out, nil
}
