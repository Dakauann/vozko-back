package whatsapp_repository

import (
	"encoding/json"
	"strings"
	"time"

	"vozko/domain/cache"
	"vozko/domain/shared"
	"vozko/domain/whatsapp/template"
)

const templateCacheTTL = 60 * time.Second

type CachedTemplateRepository struct {
	inner  template.Repository
	shared cache.SharedState
	ttl    time.Duration
}

func NewCachedTemplateRepository(inner template.Repository, shared cache.SharedState) template.Repository {
	return &CachedTemplateRepository{inner: inner, shared: shared, ttl: templateCacheTTL}
}

func templateCacheKey(templateID string) string {
	return "wa:template:" + templateID
}

func (r *CachedTemplateRepository) invalidate(templateID string) {
	if r.shared == nil || templateID == "" {
		return
	}
	_ = r.shared.Del(templateCacheKey(templateID))
}

func (r *CachedTemplateRepository) FindByID(templateID string) (*template.Template, error) {
	id := strings.TrimSpace(templateID)
	if id == "" {
		return r.inner.FindByID(templateID)
	}
	if r.shared != nil {
		if raw, err := r.shared.GetString(templateCacheKey(id)); err == nil && raw != "" {
			var t template.Template
			if json.Unmarshal([]byte(raw), &t) == nil && t.ID != "" {
				return &t, nil
			}
		}
	}
	t, err := r.inner.FindByID(id)
	if err != nil || t == nil {
		return t, err
	}
	if r.shared != nil {
		if data, mErr := json.Marshal(t); mErr == nil {
			_ = r.shared.SetString(templateCacheKey(id), string(data), r.ttl)
		}
	}
	return t, nil
}

func (r *CachedTemplateRepository) Create(t *template.Template) error {
	err := r.inner.Create(t)
	if err == nil && t != nil {
		r.invalidate(t.ID)
	}
	return err
}

func (r *CachedTemplateRepository) Update(templateID string, t *template.Template) error {
	err := r.inner.Update(templateID, t)
	if err == nil {
		r.invalidate(templateID)
	}
	return err
}

func (r *CachedTemplateRepository) Delete(templateID string) error {
	err := r.inner.Delete(templateID)
	if err == nil {
		r.invalidate(templateID)
	}
	return err
}

func (r *CachedTemplateRepository) UpdateStatus(templateID string, status template.TemplateStatus) error {
	err := r.inner.UpdateStatus(templateID, status)
	if err == nil {
		r.invalidate(templateID)
	}
	return err
}

func (r *CachedTemplateRepository) UpdateHeaderMediaURL(templateID string, headerMediaURL *string) error {
	err := r.inner.UpdateHeaderMediaURL(templateID, headerMediaURL)
	if err == nil {
		r.invalidate(templateID)
	}
	return err
}

func (r *CachedTemplateRepository) UpdateHeaderMedia(templateID string, headerMediaURL *string, headerMediaID *string) error {
	err := r.inner.UpdateHeaderMedia(templateID, headerMediaURL, headerMediaID)
	if err == nil {
		r.invalidate(templateID)
	}
	return err
}

func (r *CachedTemplateRepository) SyncFromExternal(t *template.Template) error {
	err := r.inner.SyncFromExternal(t)
	if err == nil && t != nil {
		r.invalidate(t.ID)
	}
	return err
}

func (r *CachedTemplateRepository) FindByExternalID(externalID string) (*template.Template, error) {
	return r.inner.FindByExternalID(externalID)
}

func (r *CachedTemplateRepository) FindByExternalIDAndWABA(externalID string, wabaID string) (*template.Template, error) {
	return r.inner.FindByExternalIDAndWABA(externalID, wabaID)
}

func (r *CachedTemplateRepository) BatchFindByExternalIDs(externalIDs []string) ([]*template.Template, error) {
	return r.inner.BatchFindByExternalIDs(externalIDs)
}

func (r *CachedTemplateRepository) FindByName(name, language string) (*template.Template, error) {
	return r.inner.FindByName(name, language)
}

func (r *CachedTemplateRepository) FindByNameAndWABA(name, language, wabaID string) (*template.Template, error) {
	return r.inner.FindByNameAndWABA(name, language, wabaID)
}

func (r *CachedTemplateRepository) List(input template.ListInput) (*shared.PaginatedResult[*template.Template], error) {
	return r.inner.List(input)
}
