package stage_repository

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/pipeline"
	"vozko/domain/shared"
	"vozko/domain/stage"
	"vozko/infra/database/schema"
)

const defaultConversationPipelineName = "Atendimento"

var workspaceEntryIDSubqueries = map[shared.EntryType]string{
	shared.EntryTypeWhatsApp: `SELECT wce.id FROM whatsapp_campaign_entries wce
		JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id
		WHERE wc.workspace_id = ? AND wce.deleted_at IS NULL`,
	shared.EntryTypeInstagram: `SELECT igc.id::text FROM instagram_conversations igc
		WHERE igc.workspace_id = ?::uuid AND igc.deleted_at IS NULL`,
	shared.EntryTypeTelegram: `SELECT tgc.id::text FROM telegram_conversations tgc
		WHERE tgc.workspace_id = ?::uuid AND tgc.deleted_at IS NULL`,
	shared.EntryTypeUnofficialWhatsApp: `SELECT uwc.id::text FROM unofficial_whatsapp_conversations uwc
		WHERE uwc.workspace_id = ?::uuid AND uwc.deleted_at IS NULL`,
	shared.EntryTypeSupport: `SELECT se.id::text FROM support_entries se
		JOIN support_inboxes si ON si.id = se.inbox_id
		WHERE si.workspace_id = ?::uuid AND se.deleted_at IS NULL`,
}

type repository struct {
	db                *gorm.DB
	pipelineResolvers map[shared.EntryType]stage.ContainerPipelineResolver
}

func NewRepository(db *gorm.DB) stage.Repository {
	return &repository{
		db:                db,
		pipelineResolvers: map[shared.EntryType]stage.ContainerPipelineResolver{},
	}
}

type PipelineRegistrar interface {
	SetContainerPipelineResolver(shared.EntryType, stage.ContainerPipelineResolver)
}

var _ PipelineRegistrar = (*repository)(nil)

func (r *repository) SetContainerPipelineResolver(
	entryType shared.EntryType,
	resolver stage.ContainerPipelineResolver,
) {
	if r == nil || resolver == nil || entryType == "" {
		return
	}
	if r.pipelineResolvers == nil {
		r.pipelineResolvers = map[shared.EntryType]stage.ContainerPipelineResolver{}
	}
	r.pipelineResolvers[entryType] = resolver
}

func (r *repository) ensureDefaultConversationPipeline(workspaceID string) (string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return "", nil
	}

	var pipe schema.Pipeline
	err := r.db.Where("workspace_id = ? AND object_type = ? AND is_default = ?",
		workspaceID, string(pipeline.ObjectConversation), true).
		Order("created_at ASC").First(&pipe).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		pipe = schema.Pipeline{
			ID:          uuid.New().String(),
			WorkspaceID: workspaceID,
			Name:        defaultConversationPipelineName,
			ObjectType:  string(pipeline.ObjectConversation),
			IsDefault:   true,
			Position:    0,
		}
		if err := r.db.Create(&pipe).Error; err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}

	var count int64
	if err := r.db.Model(&schema.Stage{}).
		Where("workspace_id = ? AND pipeline_id = ?", workspaceID, pipe.ID).
		Count(&count).Error; err != nil {
		return "", err
	}
	if count == 0 {
		rows := make([]schema.Stage, 0, len(stage.DefaultStages))
		for _, ds := range stage.DefaultStages {
			rows = append(rows, schema.Stage{
				ID:          uuid.New().String(),
				WorkspaceID: workspaceID,
				PipelineID:  pipe.ID,
				Name:        ds.Name,
				Description: ds.Description,
				Color:       ds.Color,
				IsDefault:   true,
				IsInitial:   ds.Key == stage.DefaultStageRecebido,
				DefaultKey:  ds.Key,
				Position:    ds.Position,
			})
		}
		if len(rows) > 0 {
			if err := r.db.Create(&rows).Error; err != nil {
				return "", err
			}
		}
	}
	return pipe.ID, nil
}

const defaultOpportunityPipelineName = "Vendas"

var defaultOpportunityStages = []struct {
	Name        string
	Description string
	Color       string
	Position    int
	IsInitial   bool
	IsWon       bool
	IsLost      bool
}{
	{Name: "novo lead", Description: "Oportunidade recém-criada, ainda não qualificada.", Color: "#3b82f6", Position: 1, IsInitial: true},
	{Name: "qualificação", Description: "Avaliando fit, orçamento e decisor.", Color: "#f59e0b", Position: 2},
	{Name: "proposta", Description: "Proposta enviada ao cliente.", Color: "#8b5cf6", Position: 3},
	{Name: "negociação", Description: "Ajustes finais de preço e condições.", Color: "#06b6d4", Position: 4},
	{Name: "ganho", Description: "Negócio fechado com sucesso.", Color: "#10b981", Position: 5, IsWon: true},
	{Name: "perdido", Description: "Oportunidade perdida.", Color: "#ef4444", Position: 6, IsLost: true},
}

func (r *repository) EnsureDefaultOpportunityPipeline(workspaceID string) (string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return "", nil
	}

	var pipe schema.Pipeline
	err := r.db.Where("workspace_id = ? AND object_type = ? AND is_default = ?",
		workspaceID, string(pipeline.ObjectOpportunity), true).
		Order("created_at ASC").First(&pipe).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		pipe = schema.Pipeline{
			ID:          uuid.New().String(),
			WorkspaceID: workspaceID,
			Name:        defaultOpportunityPipelineName,
			ObjectType:  string(pipeline.ObjectOpportunity),
			IsDefault:   true,
			Position:    0,
		}
		if err := r.db.Create(&pipe).Error; err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}

	var count int64
	if err := r.db.Model(&schema.Stage{}).
		Where("workspace_id = ? AND pipeline_id = ?", workspaceID, pipe.ID).
		Count(&count).Error; err != nil {
		return "", err
	}
	if count == 0 {
		rows := make([]schema.Stage, 0, len(defaultOpportunityStages))
		for _, ds := range defaultOpportunityStages {
			rows = append(rows, schema.Stage{
				ID:          uuid.New().String(),
				WorkspaceID: workspaceID,
				PipelineID:  pipe.ID,
				Name:        ds.Name,
				Description: ds.Description,
				Color:       ds.Color,
				IsDefault:   true,
				IsInitial:   ds.IsInitial,
				IsWon:       ds.IsWon,
				IsLost:      ds.IsLost,
				Position:    ds.Position,
			})
		}
		if len(rows) > 0 {
			if err := r.db.Create(&rows).Error; err != nil {
				return "", err
			}
		}
	}
	return pipe.ID, nil
}

func (r *repository) ListByPipeline(workspaceID, pipelineID string) ([]*stage.Stage, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	pipelineID = strings.TrimSpace(pipelineID)
	if workspaceID == "" || pipelineID == "" {
		return []*stage.Stage{}, nil
	}
	var dbStages []schema.Stage
	if err := r.db.Where("workspace_id = ? AND pipeline_id = ?", workspaceID, pipelineID).
		Order("position ASC, created_at ASC").Find(&dbStages).Error; err != nil {
		return nil, err
	}
	result := make([]*stage.Stage, len(dbStages))
	for i := range dbStages {
		result[i] = mapTagToDomain(&dbStages[i])
	}
	return result, nil
}

func (r *repository) Create(t *stage.Stage) error {
	if strings.TrimSpace(t.PipelineID) == "" && strings.TrimSpace(t.CampaignID) == "" {
		pid, err := r.ensureDefaultConversationPipeline(t.WorkspaceID)
		if err != nil {
			return err
		}
		t.PipelineID = pid
	}

	dbStage := schema.Stage{
		ID:           t.ID,
		WorkspaceID:  t.WorkspaceID,
		CampaignID:   t.CampaignID,
		CampaignType: t.CampaignType,
		PipelineID:   t.PipelineID,
		Name:         t.Name,
		Description:  t.Description,
		Color:        t.Color,
		IsDefault:    t.IsDefault,
		IsInitial:    t.IsInitial,
		IsWon:        t.IsWon,
		IsLost:       t.IsLost,
		Probability:  t.Probability,
		RotDays:      t.RotDays,
		DefaultKey:   t.DefaultKey,
		Position:     t.Position,
	}
	if err := r.db.Create(&dbStage).Error; err != nil {
		return err
	}
	t.CreatedAt = dbStage.CreatedAt
	t.UpdatedAt = dbStage.UpdatedAt
	return nil
}

func (r *repository) Update(t *stage.Stage) error {
	update := map[string]interface{}{
		"name":        t.Name,
		"description": t.Description,
		"color":       t.Color,
		"is_default":  t.IsDefault,
		"is_initial":  t.IsInitial,
		"is_won":      t.IsWon,
		"is_lost":     t.IsLost,
		"probability": t.Probability,
		"rot_days":    t.RotDays,
		"pipeline_id": nullableUUID(t.PipelineID),
		"default_key": t.DefaultKey,
		"position":    t.Position,
	}
	return r.db.Model(&schema.Stage{}).Where("id = ?", t.ID).Updates(update).Error
}

func (r *repository) Delete(id string) error {
	if err := r.db.Where("stage_id = ?", id).Delete(&schema.EntryStage{}).Error; err != nil {
		return err
	}
	return r.db.Delete(&schema.Stage{}, "id = ?", id).Error
}

func (r *repository) FindByID(id string) (*stage.Stage, error) {
	var dbStage schema.Stage
	if err := r.db.Where("id = ?", id).First(&dbStage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, stage.ErrTagNotFound
		}
		return nil, err
	}
	return mapTagToDomain(&dbStage), nil
}

func (r *repository) FindIDsByName(workspaceID, name string) ([]string, error) {
	var ids []string
	if err := r.db.Model(&schema.Stage{}).
		Where("workspace_id = ? AND LOWER(name) = LOWER(?)", workspaceID, strings.TrimSpace(name)).
		Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *repository) ListByWorkspace(workspaceID string) ([]*stage.Stage, error) {
	var dbStages []schema.Stage
	if err := r.db.Where("workspace_id = ?", workspaceID).Order("position ASC, created_at ASC").Find(&dbStages).Error; err != nil {
		return nil, err
	}
	result := make([]*stage.Stage, len(dbStages))
	for i := range dbStages {
		result[i] = mapTagToDomain(&dbStages[i])
	}
	return result, nil
}

func (r *repository) ListDistinctByWorkspace(workspaceID string) ([]*stage.Stage, error) {
	var dbStages []schema.Stage
	if err := r.db.Raw(`SELECT DISTINCT ON (LOWER(name)) id, workspace_id, campaign_id, campaign_type,
		name, description, color, is_default, is_initial, default_key, position, created_at, updated_at
		FROM stages WHERE workspace_id = ? AND deleted_at IS NULL
		ORDER BY LOWER(name), position ASC, created_at ASC`, workspaceID).Scan(&dbStages).Error; err != nil {
		return nil, err
	}
	result := make([]*stage.Stage, len(dbStages))
	for i := range dbStages {
		result[i] = mapTagToDomain(&dbStages[i])
	}
	return result, nil
}

func (r *repository) resolveConversationPipeline(workspaceID, campaignID, campaignType string) (string, error) {
	if strings.TrimSpace(campaignID) != "" {
		if pid := r.campaignPipelineID(campaignID, campaignType); pid != "" {
			return pid, nil
		}
	}
	return r.ensureDefaultConversationPipeline(workspaceID)
}

func (r *repository) campaignPipelineID(campaignID, campaignType string) string {
	resolver, found := r.pipelineResolvers[shared.EntryType(campaignType)]
	if !found || resolver == nil {
		return ""
	}

	pipelineID, err := resolver.PipelineIDForContainer(context.Background(), campaignID)
	if err != nil {
		log.Printf("[stage] pipeline lookup failed for %s container %s: %v", campaignType, campaignID, err)
		return ""
	}
	return strings.TrimSpace(pipelineID)
}

func (r *repository) CreateConversationPipeline(workspaceID, name, stageGroupID string) (string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return "", fmt.Errorf("CreateConversationPipeline: workspace id required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Funil"
	}
	var maxPos int
	r.db.Model(&schema.Pipeline{}).
		Where("workspace_id = ? AND object_type = ?", workspaceID, string(pipeline.ObjectConversation)).
		Select("COALESCE(MAX(position),0)").Scan(&maxPos)
	pipe := schema.Pipeline{
		ID:           uuid.New().String(),
		WorkspaceID:  workspaceID,
		Name:         name,
		ObjectType:   string(pipeline.ObjectConversation),
		StageGroupID: strings.TrimSpace(stageGroupID),
		IsDefault:    false,
		Position:     maxPos + 1,
	}
	if err := r.db.Create(&pipe).Error; err != nil {
		return "", err
	}
	return pipe.ID, nil
}

func (r *repository) FindConversationPipelineByGroup(workspaceID, stageGroupID string) (string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	stageGroupID = strings.TrimSpace(stageGroupID)
	if workspaceID == "" || stageGroupID == "" {
		return "", nil
	}
	var pipe schema.Pipeline
	err := r.db.Select("id").
		Where("workspace_id = ? AND object_type = ? AND stage_group_id = ?",
			workspaceID, string(pipeline.ObjectConversation), stageGroupID).
		Order("created_at ASC").First(&pipe).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return pipe.ID, nil
}

func (r *repository) SetCampaignPipeline(campaignID, campaignType, pipelineID string) error {
	_ = campaignType
	if strings.TrimSpace(campaignID) == "" || strings.TrimSpace(pipelineID) == "" {
		return nil
	}
	return r.db.Table("whatsapp_campaigns").Where("id = ?", campaignID).Update("pipeline_id", pipelineID).Error
}

func (r *repository) ListByCampaign(workspaceID, campaignID, campaignType string) ([]*stage.Stage, error) {
	pipelineID, err := r.resolveConversationPipeline(workspaceID, campaignID, campaignType)
	if err != nil {
		return nil, err
	}
	if pipelineID == "" {
		return []*stage.Stage{}, nil
	}
	var dbStages []schema.Stage
	if err := r.db.Where("workspace_id = ? AND pipeline_id = ?", workspaceID, pipelineID).
		Order("position ASC, created_at ASC").Find(&dbStages).Error; err != nil {
		return nil, err
	}
	result := make([]*stage.Stage, len(dbStages))
	for i := range dbStages {
		result[i] = mapTagToDomain(&dbStages[i])
	}
	return result, nil
}

func (r *repository) ListByCampaignIDs(workspaceID string, campaignIDs []string) (map[string][]*stage.Stage, error) {
	result := make(map[string][]*stage.Stage, len(campaignIDs))
	if len(campaignIDs) == 0 {
		return result, nil
	}
	stagesByPipeline := make(map[string][]*stage.Stage)
	for _, cid := range campaignIDs {
		if strings.TrimSpace(cid) == "" {
			continue
		}
		pid, err := r.resolveConversationPipeline(workspaceID, cid, "")
		if err != nil {
			return nil, err
		}
		if pid == "" {
			continue
		}
		stages, ok := stagesByPipeline[pid]
		if !ok {
			stages, err = r.ListByPipeline(workspaceID, pid)
			if err != nil {
				return nil, err
			}
			stagesByPipeline[pid] = stages
		}
		result[cid] = stages
	}
	return result, nil
}

func (r *repository) NameExistsInCampaign(workspaceID, campaignID, campaignType, name string, excludeID *string) (bool, error) {
	pipelineID, err := r.resolveConversationPipeline(workspaceID, campaignID, campaignType)
	if err != nil {
		return false, err
	}
	query := r.db.Model(&schema.Stage{}).Where("workspace_id = ? AND pipeline_id = ? AND LOWER(name) = ?", workspaceID, pipelineID, strings.ToLower(name))
	if excludeID != nil && *excludeID != "" {
		query = query.Where("id <> ?", *excludeID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *repository) SetInitialStage(workspaceID, campaignID, campaignType, StageID string) error {
	pipelineID, err := r.resolveConversationPipeline(workspaceID, campaignID, campaignType)
	if err != nil {
		return err
	}
	if err := r.db.Model(&schema.Stage{}).
		Where("workspace_id = ? AND pipeline_id = ? AND is_initial = ?", workspaceID, pipelineID, true).
		Update("is_initial", false).Error; err != nil {
		return err
	}
	return r.db.Model(&schema.Stage{}).
		Where("id = ? AND workspace_id = ?", StageID, workspaceID).
		Update("is_initial", true).Error
}

func (r *repository) ClearInitialStage(workspaceID string) error {
	return r.db.Model(&schema.Stage{}).
		Where("workspace_id = ? AND is_initial = ?", workspaceID, true).
		Update("is_initial", false).Error
}

func (r *repository) GetInitialStage(workspaceID string) (*stage.Stage, error) {
	var dbStage schema.Stage
	if err := r.db.Where("workspace_id = ? AND is_initial = ?", workspaceID, true).
		Order("(CASE WHEN pipeline_id IS NULL THEN 1 ELSE 0 END) ASC, position ASC, created_at ASC").
		First(&dbStage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return mapTagToDomain(&dbStage), nil
}

func (r *repository) GetInitialStageForCampaign(workspaceID, campaignID, campaignType string) (*stage.Stage, error) {
	pipelineID, err := r.resolveConversationPipeline(workspaceID, campaignID, campaignType)
	if err != nil {
		return nil, err
	}
	if pipelineID == "" {
		return nil, nil
	}

	var dbStage schema.Stage
	err = r.db.Where("workspace_id = ? AND pipeline_id = ? AND is_initial = ?", workspaceID, pipelineID, true).
		Order("position ASC, created_at ASC").First(&dbStage).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err2 := r.db.Where("workspace_id = ? AND pipeline_id = ?", workspaceID, pipelineID).
			Order("position ASC, created_at ASC").First(&dbStage).Error; err2 != nil {
			if errors.Is(err2, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, err2
		}
		return mapTagToDomain(&dbStage), nil
	} else if err != nil {
		return nil, err
	}
	return mapTagToDomain(&dbStage), nil
}

func (r *repository) AssignStage(et *stage.EntryStage) error {
	if err := r.db.Where("entry_id = ? AND entry_type = ? AND workspace_id = ?", et.EntryID, et.EntryType, et.WorkspaceID).
		Delete(&schema.EntryStage{}).Error; err != nil {
		return err
	}

	dbET := schema.EntryStage{
		ID:          et.ID,
		StageID:     et.StageID,
		EntryID:     et.EntryID,
		EntryType:   et.EntryType,
		WorkspaceID: et.WorkspaceID,
	}
	if err := r.db.Create(&dbET).Error; err != nil {
		return err
	}
	et.CreatedAt = dbET.CreatedAt
	return nil
}

func (r *repository) RemoveStage(StageID, entryID, entryType, workspaceID string) error {
	result := r.db.Where("stage_id = ? AND entry_id = ? AND entry_type = ? AND workspace_id = ?", StageID, entryID, entryType, workspaceID).
		Delete(&schema.EntryStage{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return stage.ErrEntryTagNotFound
	}
	return nil
}

func (r *repository) RemoveEntryStage(entryID, entryType, workspaceID string) error {
	result := r.db.Where("entry_id = ? AND entry_type = ? AND workspace_id = ?", entryID, entryType, workspaceID).
		Delete(&schema.EntryStage{})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (r *repository) GetEntryStage(entryID, entryType, workspaceID string) (*stage.EntryStage, error) {
	var dbEntryStage schema.EntryStage
	if err := r.db.Preload("Stage").
		Where("entry_id = ? AND entry_type = ? AND workspace_id = ?", entryID, entryType, workspaceID).
		First(&dbEntryStage).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return mapEntryTagToDomain(&dbEntryStage), nil
}

func (r *repository) GetBatchEntryStages(entryIDs []string, entryType, workspaceID string) (map[string]*stage.EntryStage, error) {
	if len(entryIDs) == 0 {
		return make(map[string]*stage.EntryStage), nil
	}

	var dbEntryStages []schema.EntryStage
	if err := r.db.Preload("Stage").
		Where("entry_id IN ? AND entry_type = ? AND workspace_id = ?", entryIDs, entryType, workspaceID).
		Find(&dbEntryStages).Error; err != nil {
		return nil, err
	}

	result := make(map[string]*stage.EntryStage, len(entryIDs))
	for i := range dbEntryStages {
		et := mapEntryTagToDomain(&dbEntryStages[i])

		if _, exists := result[et.EntryID]; !exists {
			result[et.EntryID] = et
		}
	}
	return result, nil
}

func (r *repository) GetEntriesByStage(StageID, workspaceID string) ([]*stage.EntryStage, error) {
	var dbEntryStages []schema.EntryStage
	if err := r.db.Preload("Stage").
		Where("stage_id = ? AND workspace_id = ?", StageID, workspaceID).
		Find(&dbEntryStages).Error; err != nil {
		return nil, err
	}

	result := make([]*stage.EntryStage, len(dbEntryStages))
	for i := range dbEntryStages {
		result[i] = mapEntryTagToDomain(&dbEntryStages[i])
	}
	return result, nil
}

func (r *repository) EnsureDefaultStagesForCampaign(workspaceID, campaignID, campaignType string) error {
	_, err := r.ensureDefaultConversationPipeline(workspaceID)
	return err
}

func (r *repository) DeduplicateStages(workspaceID string) error {

	type dupGroup struct {
		LowerName string `gorm:"column:lower_name"`
		Count     int64  `gorm:"column:cnt"`
	}
	var groups []dupGroup
	if err := r.db.Raw(`
		SELECT LOWER(name) AS lower_name, COUNT(*) AS cnt
		FROM stages
		WHERE workspace_id = ? AND deleted_at IS NULL
		GROUP BY LOWER(name)
		HAVING COUNT(*) > 1
	`, workspaceID).Scan(&groups).Error; err != nil {
		return err
	}
	if len(groups) == 0 {
		return nil
	}

	return r.db.Transaction(func(tx *gorm.DB) error {
		for _, g := range groups {

			var tags []schema.Stage
			if err := tx.Where("workspace_id = ? AND LOWER(name) = ? AND deleted_at IS NULL", workspaceID, g.LowerName).
				Order("(CASE WHEN default_key IS NOT NULL AND default_key <> '' THEN 0 ELSE 1 END) ASC, created_at ASC").
				Find(&tags).Error; err != nil {
				return err
			}
			if len(tags) < 2 {
				continue
			}

			survivor := tags[0]
			for _, dup := range tags[1:] {

				if err := tx.Model(&schema.EntryStage{}).
					Where("stage_id = ? AND workspace_id = ? AND deleted_at IS NULL", dup.ID, workspaceID).
					Update("stage_id", survivor.ID).Error; err != nil {
					return err
				}

				if err := tx.Delete(&schema.Stage{}, "id = ?", dup.ID).Error; err != nil {
					return err
				}
			}

			for _, dup := range tags[1:] {
				if dup.DefaultKey != "" && survivor.DefaultKey == "" {
					tx.Model(&schema.Stage{}).Where("id = ?", survivor.ID).Updates(map[string]interface{}{
						"default_key": dup.DefaultKey,
						"is_default":  true,
					})
					break
				}
			}
		}
		return nil
	})
}

func (r *repository) ReorderStages(workspaceID string, tagIDs []string) error {
	if len(tagIDs) == 0 {
		return nil
	}

	caseSQL := "CASE id "
	args := make([]interface{}, 0, len(tagIDs))
	for i, StageID := range tagIDs {
		caseSQL += fmt.Sprintf("WHEN ? THEN %d ", i+1)
		args = append(args, StageID)
	}
	caseSQL += "END"

	result := r.db.Model(&schema.Stage{}).
		Where("id IN ? AND workspace_id = ?", tagIDs, workspaceID).
		Update("position", gorm.Expr(caseSQL, args...))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return stage.ErrTagNotFound
	}
	return nil
}

func mapTagToDomain(dbStage *schema.Stage) *stage.Stage {
	return &stage.Stage{
		ID:           dbStage.ID,
		WorkspaceID:  dbStage.WorkspaceID,
		CampaignID:   dbStage.CampaignID,
		CampaignType: dbStage.CampaignType,
		PipelineID:   dbStage.PipelineID,
		Name:         dbStage.Name,
		Description:  dbStage.Description,
		Color:        dbStage.Color,
		IsDefault:    dbStage.IsDefault,
		IsInitial:    dbStage.IsInitial,
		IsWon:        dbStage.IsWon,
		IsLost:       dbStage.IsLost,
		Probability:  dbStage.Probability,
		RotDays:      dbStage.RotDays,
		DefaultKey:   dbStage.DefaultKey,
		Position:     dbStage.Position,
		CreatedAt:    dbStage.CreatedAt,
		UpdatedAt:    dbStage.UpdatedAt,
	}
}

func nullableUUID(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func mapEntryTagToDomain(dbET *schema.EntryStage) *stage.EntryStage {
	et := &stage.EntryStage{
		ID:          dbET.ID,
		StageID:     dbET.StageID,
		EntryID:     dbET.EntryID,
		EntryType:   dbET.EntryType,
		WorkspaceID: dbET.WorkspaceID,
		CreatedAt:   dbET.CreatedAt,
	}
	if dbET.Stage.ID != "" {
		et.StageName = dbET.Stage.Name
		et.StageColor = dbET.Stage.Color
	}
	return et
}

func (r *repository) GetStageCountsForCampaign(workspaceID, campaignID, entryType string) (map[string]int64, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	campaignID = strings.TrimSpace(campaignID)
	if workspaceID == "" || campaignID == "" {
		return nil, nil
	}

	const entryTable = "whatsapp_campaign_entries"
	_ = entryType

	type tagCount struct {
		StageName string `gorm:"column:tag_name"`
		Count     int64  `gorm:"column:cnt"`
	}
	var results []tagCount

	query := `
		SELECT LOWER(t.name) AS tag_name, COUNT(*) AS cnt
		FROM entry_stages et
		JOIN stages t ON t.id = et.stage_id AND t.deleted_at IS NULL
		JOIN ` + entryTable + ` ce ON ce.id = et.entry_id AND ce.deleted_at IS NULL
		WHERE et.workspace_id = ? AND et.deleted_at IS NULL
		  AND ce.campaign_id = ?
		GROUP BY LOWER(t.name)
	`
	if err := r.db.Raw(query, workspaceID, campaignID).Scan(&results).Error; err != nil {
		return nil, err
	}

	counts := make(map[string]int64, len(results))
	for _, r := range results {
		counts[r.StageName] = r.Count
	}
	return counts, nil
}

func (r *repository) GetStageCountsForWorkspace(workspaceID, entryType string) (map[string]int64, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, nil
	}

	type tagCount struct {
		StageName string `gorm:"column:tag_name"`
		Count     int64  `gorm:"column:cnt"`
	}
	var results []tagCount

	args := []interface{}{workspaceID}

	var entryTypeFilter string
	if entryType != "" {
		subquery, ok := workspaceEntryIDSubqueries[shared.EntryType(entryType)]
		if !ok {
			return map[string]int64{}, nil
		}
		entryTypeFilter = "AND et.entry_id IN (" + subquery + ")"
		args = append(args, workspaceID)
	}

	query := fmt.Sprintf(`
		SELECT LOWER(t.name) AS tag_name, COUNT(*) AS cnt
		FROM entry_stages et
		JOIN stages t ON t.id = et.stage_id AND t.deleted_at IS NULL
		WHERE et.workspace_id = ? AND et.deleted_at IS NULL
		  %s
		  AND EXISTS (
		      SELECT 1 FROM conversation_messages cm
		      WHERE cm.entry_id = et.entry_id AND cm.deleted_at IS NULL
		  )
		GROUP BY LOWER(t.name)
	`, entryTypeFilter)
	if err := r.db.Raw(query, args...).Scan(&results).Error; err != nil {
		return nil, err
	}

	counts := make(map[string]int64, len(results))
	for _, r := range results {
		counts[r.StageName] = r.Count
	}
	return counts, nil
}
