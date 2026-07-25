package whatsapp_repository

import (
	"strings"

	"gorm.io/gorm"

	"vozko/domain/shared"
	"vozko/domain/whatsapp/template"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewTemplateRepository(db *gorm.DB) template.Repository {
	return &repository{db: db}
}

func (r *repository) Create(t *template.Template) error {
	record := mapToSchema(t)
	if err := r.db.Create(&record).Error; err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			return template.ErrTemplateAlreadyExists
		}
		return err
	}
	t.ID = record.ID
	return nil
}

func (r *repository) Update(templateID string, t *template.Template) error {
	paramFormat := string(t.ParameterFormat)
	if paramFormat == "" {
		paramFormat = "positional"
	}
	update := map[string]interface{}{
		"waba_id":          t.WABAId,
		"name":             t.Name,
		"language":         t.Language,
		"category":         string(t.Category),
		"status":           string(t.Status),
		"parameter_format": paramFormat,
		"components":       mapComponentsToSchema(t.Components),
		"header_media_url": t.HeaderMediaURL,
		"header_media_id":  t.HeaderMediaID,
	}
	result := r.db.Model(&schema.WhatsAppTemplate{}).Where("id = ?", templateID).Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return template.ErrTemplateNotFound
	}
	return nil
}

func (r *repository) Delete(templateID string) error {
	result := r.db.Delete(&schema.WhatsAppTemplate{}, "id = ?", templateID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return template.ErrTemplateNotFound
	}
	return nil
}

func (r *repository) FindByID(templateID string) (*template.Template, error) {
	var record schema.WhatsAppTemplate
	if err := r.db.Where("id = ?", templateID).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, template.ErrTemplateNotFound
		}
		return nil, err
	}
	return mapToDomain(&record), nil
}

func (r *repository) FindByExternalID(externalID string) (*template.Template, error) {
	var record schema.WhatsAppTemplate
	if err := r.db.Where("external_id = ?", externalID).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, template.ErrTemplateNotFound
		}
		return nil, err
	}
	return mapToDomain(&record), nil
}

func (r *repository) FindByName(name, language string) (*template.Template, error) {
	var record schema.WhatsAppTemplate
	query := r.db.Where("name = ?", strings.ToLower(name))
	if language != "" {
		query = query.Where("language = ?", language)
	}
	if err := query.First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, template.ErrTemplateNotFound
		}
		return nil, err
	}
	return mapToDomain(&record), nil
}

func (r *repository) FindByExternalIDAndWABA(externalID string, wabaID string) (*template.Template, error) {
	var record schema.WhatsAppTemplate
	if err := r.db.Where("external_id = ? AND waba_id = ?", externalID, wabaID).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, template.ErrTemplateNotFound
		}
		return nil, err
	}
	return mapToDomain(&record), nil
}

func (r *repository) BatchFindByExternalIDs(externalIDs []string) ([]*template.Template, error) {
	if len(externalIDs) == 0 {
		return nil, nil
	}
	var records []schema.WhatsAppTemplate
	if err := r.db.Where("external_id IN ?", externalIDs).Find(&records).Error; err != nil {
		return nil, err
	}
	result := make([]*template.Template, 0, len(records))
	for i := range records {
		result = append(result, mapToDomain(&records[i]))
	}
	return result, nil
}

func (r *repository) FindByNameAndWABA(name, language, wabaID string) (*template.Template, error) {
	var record schema.WhatsAppTemplate
	query := r.db.Where("name = ? AND waba_id = ?", strings.ToLower(name), wabaID)
	if language != "" {
		query = query.Where("language = ?", language)
	}
	if err := query.First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, template.ErrTemplateNotFound
		}
		return nil, err
	}
	return mapToDomain(&record), nil
}

func (r *repository) List(input template.ListInput) (*shared.PaginatedResult[*template.Template], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	countQuery := r.db.Model(&schema.WhatsAppTemplate{})
	countQuery = applyFilters(countQuery, input)
	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, err
	}

	// Aggregate the business (WABA) name in the query layer: one LEFT JOIN on the
	// indexed whatsapp_business_accounts.meta_waba_id, so the client renders the name
	// inline and never refetches/joins WABAs itself.
	dataQuery := r.db.Model(&schema.WhatsAppTemplate{}).
		Select(templateTable + ".*, w.name AS joined_waba_name").
		Joins("LEFT JOIN whatsapp_business_accounts w ON w.meta_waba_id = " + templateTable + ".waba_id AND w.deleted_at IS NULL").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize)
	dataQuery = applyFilters(dataQuery, input)
	dataQuery = applySorts(dataQuery, input.Options.Sorts)

	var records []templateWithWABA
	if err := dataQuery.Find(&records).Error; err != nil {
		return nil, err
	}

	items := make([]*template.Template, 0, len(records))
	for i := range records {
		item := mapToDomain(&records[i].WhatsAppTemplate)
		if records[i].JoinedWABAName != "" {
			item.WABAName = records[i].JoinedWABAName
		}
		items = append(items, item)
	}

	return shared.NewPaginatedResult(items, pagination, total), nil
}

// templateWithWABA is the List scan target: the template row plus the business name
// joined from whatsapp_business_accounts in the same query.
type templateWithWABA struct {
	schema.WhatsAppTemplate
	JoinedWABAName string `gorm:"column:joined_waba_name"`
}

func (r *repository) UpdateStatus(templateID string, status template.TemplateStatus) error {
	result := r.db.Model(&schema.WhatsAppTemplate{}).
		Where("id = ?", templateID).
		Update("status", string(status))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return template.ErrTemplateNotFound
	}
	return nil
}

func (r *repository) UpdateHeaderMediaURL(templateID string, headerMediaURL *string) error {
	result := r.db.Model(&schema.WhatsAppTemplate{}).
		Where("id = ?", templateID).
		Update("header_media_url", headerMediaURL)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return template.ErrTemplateNotFound
	}
	return nil
}

func (r *repository) UpdateHeaderMedia(templateID string, headerMediaURL *string, headerMediaID *string) error {
	updates := map[string]interface{}{
		"header_media_url": headerMediaURL,
		"header_media_id":  headerMediaID,
	}
	result := r.db.Model(&schema.WhatsAppTemplate{}).
		Where("id = ?", templateID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return template.ErrTemplateNotFound
	}
	return nil
}

func (r *repository) SyncFromExternal(t *template.Template) error {
	var existing schema.WhatsAppTemplate
	err := r.db.Where("external_id = ?", t.ExternalID).First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		return r.Create(t)
	}
	if err != nil {
		return err
	}
	t.ID = existing.ID
	return r.Update(existing.ID, t)
}

// templateTable qualifies columns so filters/sorts stay unambiguous once List LEFT
// JOINs whatsapp_business_accounts (both tables carry id/created_at/updated_at).
const templateTable = "whatsapp_templates"

func applyFilters(query *gorm.DB, input template.ListInput) *gorm.DB {
	if len(input.TemplateIDs) > 0 {
		query = query.Where(templateTable+".id IN ?", input.TemplateIDs)
	}
	if input.WABAId != "" {
		query = query.Where(templateTable+".waba_id = ?", input.WABAId)
	}
	if input.Status != "" {
		query = query.Where(templateTable+".status = ?", string(input.Status))
	}
	if input.Category != "" {
		query = query.Where(templateTable+".category = ?", string(input.Category))
	}
	if input.Language != "" {
		query = query.Where(templateTable+".language = ?", input.Language)
	}
	if input.Search != "" {
		like := "%" + input.Search + "%"
		query = query.Where(templateTable+".name ILIKE ?", like)
	}
	return query
}

func applySorts(query *gorm.DB, sorts []shared.Sort) *gorm.DB {
	if len(sorts) == 0 {
		return query.Order(templateTable + ".created_at DESC")
	}
	for _, sort := range sorts {
		field := strings.ToLower(sort.Field)
		direction := "ASC"
		if strings.ToUpper(string(sort.Direction)) == "DESC" {
			direction = "DESC"
		}
		switch field {
		case "name", "status", "category", "language", "created_at", "updated_at":
			query = query.Order(templateTable + "." + field + " " + direction)
		}
	}
	return query
}

func mapToSchema(t *template.Template) *schema.WhatsAppTemplate {
	paramFormat := string(t.ParameterFormat)
	if paramFormat == "" {
		paramFormat = "positional"
	}
	return &schema.WhatsAppTemplate{
		ID:              t.ID,
		ExternalID:      t.ExternalID,
		WABAId:          t.WABAId,
		Name:            t.Name,
		Language:        t.Language,
		Category:        string(t.Category),
		Status:          string(t.Status),
		ParameterFormat: paramFormat,
		Components:      mapComponentsToSchema(t.Components),
		HeaderMediaURL:  t.HeaderMediaURL,
		HeaderMediaID:   t.HeaderMediaID,
	}
}

func mapComponentsToSchema(components []template.TemplateComponent) schema.WhatsAppTemplateComponents {
	result := make(schema.WhatsAppTemplateComponents, len(components))
	for i, c := range components {
		result[i] = schema.WhatsAppTemplateComponent{
			Type:       c.Type,
			Format:     c.Format,
			Text:       c.Text,
			Parameters: c.Parameters,
		}
		for _, b := range c.Buttons {
			result[i].Buttons = append(result[i].Buttons, schema.WhatsAppTemplateButton{
				Type:        b.Type,
				Text:        b.Text,
				URL:         b.URL,
				PhoneNumber: b.PhoneNumber,
				Example:     b.Example,
			})
		}
		if c.Example != nil {
			result[i].Example = &schema.WhatsAppTemplateExample{
				HeaderText:   c.Example.HeaderText,
				HeaderHandle: c.Example.HeaderHandle,
				BodyText:     c.Example.BodyText,
			}
			for _, np := range c.Example.BodyTextNamed {
				result[i].Example.BodyTextNamed = append(result[i].Example.BodyTextNamed, schema.WhatsAppTemplateNamedParam{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
			for _, np := range c.Example.HeaderTextNamed {
				result[i].Example.HeaderTextNamed = append(result[i].Example.HeaderTextNamed, schema.WhatsAppTemplateNamedParam{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
		}
	}
	return result
}

func mapToDomain(record *schema.WhatsAppTemplate) *template.Template {
	components := make([]template.TemplateComponent, len(record.Components))
	for i, c := range record.Components {
		components[i] = template.TemplateComponent{
			Type:       c.Type,
			Format:     c.Format,
			Text:       c.Text,
			Parameters: c.Parameters,
		}
		for _, b := range c.Buttons {
			components[i].Buttons = append(components[i].Buttons, template.TemplateButton{
				Type:        b.Type,
				Text:        b.Text,
				URL:         b.URL,
				PhoneNumber: b.PhoneNumber,
				Example:     b.Example,
			})
		}
		if c.Example != nil {
			components[i].Example = &template.TemplateExample{
				HeaderText:   c.Example.HeaderText,
				HeaderHandle: c.Example.HeaderHandle,
				BodyText:     c.Example.BodyText,
			}
			for _, np := range c.Example.BodyTextNamed {
				components[i].Example.BodyTextNamed = append(components[i].Example.BodyTextNamed, template.NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
			for _, np := range c.Example.HeaderTextNamed {
				components[i].Example.HeaderTextNamed = append(components[i].Example.HeaderTextNamed, template.NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
		}
	}
	return &template.Template{
		ID:              record.ID,
		ExternalID:      record.ExternalID,
		WABAId:          record.WABAId,
		Name:            record.Name,
		Language:        record.Language,
		Category:        template.TemplateCategory(record.Category),
		Status:          template.TemplateStatus(record.Status),
		ParameterFormat: template.ParameterFormat(record.ParameterFormat),
		Components:      components,
		HeaderMediaURL:  record.HeaderMediaURL,
		HeaderMediaID:   record.HeaderMediaID,
		CreatedAt:       record.CreatedAt,
		UpdatedAt:       record.UpdatedAt,
	}
}
