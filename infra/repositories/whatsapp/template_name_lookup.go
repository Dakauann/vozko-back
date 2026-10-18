package whatsapp_repository

import (
	"strings"

	"gorm.io/gorm"

	"vozko/domain/whatsapp/template"
	"vozko/infra/database/schema"
)

const grantedTemplatesByNameSQL = "SELECT t.* FROM whatsapp_templates t" +
	" JOIN workspace_template_access grants ON grants.template_id = t.id" +
	" WHERE grants.workspace_id = ? AND lower(t.name) = ? AND t.language = ? AND t.deleted_at IS NULL" +
	" ORDER BY t.created_at, t.id"

type grantedTemplateNames struct {
	db *gorm.DB
}

func NewGrantedTemplateNames(db *gorm.DB) template.GrantedTemplateNames {
	return &grantedTemplateNames{db: db}
}

func (r *grantedTemplateNames) FindGrantedByName(workspaceID, name, language string) ([]*template.Template, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, template.ErrTemplateAccessDenied
	}
	name, language = template.LookupKey(name, language)
	var records []schema.WhatsAppTemplate
	if err := r.db.Raw(grantedTemplatesByNameSQL, workspaceID, name, language).Scan(&records).Error; err != nil {
		return nil, err
	}
	found := make([]*template.Template, 0, len(records))
	for i := range records {
		found = append(found, mapToDomain(&records[i]))
	}
	return found, nil
}
