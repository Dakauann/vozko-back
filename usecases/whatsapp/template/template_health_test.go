package template_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/whatsapp/quality"
	domain "vozko/domain/whatsapp/template"
)

type grantedTemplates map[string]*domain.Template

func (g grantedTemplates) Get(workspaceID, templateID string) (*domain.Template, error) {
	tmpl, ok := g[workspaceID+"/"+templateID]
	if !ok {
		return nil, domain.ErrTemplateAccessDenied
	}
	return tmpl, nil
}

type qualityLog struct {
	latest map[quality.Signal]quality.Change
	recent []quality.Change
}

func (q qualityLog) LatestBySignal(quality.Subject, string) (map[quality.Signal]quality.Change, error) {
	return q.latest, nil
}

func (q qualityLog) Recent(_ quality.Subject, _ string, limit int) ([]quality.Change, error) {
	if len(q.recent) > limit {
		return q.recent[:limit], nil
	}
	return q.recent, nil
}

var healthAt = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func approved() *domain.Template {
	return &domain.Template{ID: "tpl", ExternalID: "x1", Status: domain.TemplateStatusApproved, Category: domain.TemplateCategoryMarketing}
}

func TestATemplateMetaHasNotRatedShowsPendingQuality(t *testing.T) {
	reader := NewTemplateHealthReader(grantedTemplates{"ws/tpl": approved()}, qualityLog{})

	health, err := reader.Read("ws", "tpl")

	if err != nil || health.Quality != quality.QualityUnknown || health.Flagged || len(health.Changes) != 0 {
		t.Fatalf("health = %+v, %v", health, err)
	}
}

func TestTheHealthCarriesQualityFlagAndRecentChanges(t *testing.T) {
	changes := []quality.Change{
		{Signal: quality.SignalStatus, Current: "FLAGGED", OccurredAt: healthAt.Add(time.Hour)},
		{Signal: quality.SignalQuality, Previous: "GREEN", Current: "YELLOW", OccurredAt: healthAt},
	}
	reader := NewTemplateHealthReader(grantedTemplates{"ws/tpl": approved()}, qualityLog{
		latest: map[quality.Signal]quality.Change{quality.SignalQuality: changes[1], quality.SignalStatus: changes[0]},
		recent: changes,
	})

	health, err := reader.Read("ws", "tpl")

	if err != nil || health.Quality != "YELLOW" || !health.QualityAt.Equal(healthAt) || !health.Flagged {
		t.Fatalf("health = %+v, %v", health, err)
	}
	if health.Status != domain.TemplateStatusApproved || health.Category != domain.TemplateCategoryMarketing || len(health.Changes) != 2 {
		t.Fatalf("health = %+v", health)
	}
}

func TestAWorkspaceWithoutAGrantSeesNoHealth(t *testing.T) {
	reader := NewTemplateHealthReader(grantedTemplates{"owner/tpl": approved()}, qualityLog{})

	if _, err := reader.Read("stranger", "tpl"); !errors.Is(err, domain.ErrTemplateAccessDenied) {
		t.Fatalf("err = %v, want access denied for a workspace without a grant", err)
	}
}
