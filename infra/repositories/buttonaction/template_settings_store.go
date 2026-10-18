package buttonaction_repository

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"vozko/domain/buttonaction"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/exitcolumns"
)

const (
	selectTemplateExitSQL = `SELECT confirmation, version, updated_by, updated_at
		FROM template_exit_settings WHERE workspace_id = ? AND template_name = ? AND language = ?`
	selectTemplateActionsSQL = `SELECT button_index, kind, params, confirmation, button_text, version, updated_by, updated_at FROM template_button_actions
		WHERE workspace_id = ? AND template_name = ? AND language = ? ORDER BY button_index`
	insertTemplateExitSQL = `INSERT INTO template_exit_settings AS s (workspace_id, template_name, language, confirmation, version, updated_by, updated_at)
		VALUES (?, ?, ?, ?, 2, ?, NOW())
		ON CONFLICT (workspace_id, template_name, language) DO UPDATE SET confirmation = EXCLUDED.confirmation,
			version = s.version + 1, updated_by = EXCLUDED.updated_by, updated_at = NOW()
		WHERE s.version = 1
		RETURNING version, updated_at`
	updateTemplateExitSQL = `UPDATE template_exit_settings SET confirmation = ?, version = version + 1, updated_by = ?, updated_at = NOW()
		WHERE workspace_id = ? AND template_name = ? AND language = ? AND version = ?
		RETURNING version, updated_at`
	deleteTemplateActionsSQL = `DELETE FROM template_button_actions WHERE workspace_id = ? AND template_name = ? AND language = ?`
)

type TemplateSettingsStore struct {
	db *gorm.DB
}

var _ buttonaction.TemplateSettingsStore = (*TemplateSettingsStore)(nil)

func NewTemplateSettingsStore(db *gorm.DB) *TemplateSettingsStore {
	return &TemplateSettingsStore{db: db}
}

type savedHead struct {
	Version   int64
	UpdatedAt time.Time
}

func (s *TemplateSettingsStore) Get(ctx context.Context, workspaceID string, ref buttonaction.TemplateRef) (buttonaction.TemplateSettings, error) {
	if !exitcolumns.ValidID(workspaceID) {
		return buttonaction.TemplateSettings{}, buttonaction.ErrWorkspaceRequired
	}
	if ref.Empty() {
		return buttonaction.TemplateSettings{}, buttonaction.ErrTemplateNotFound
	}
	settings := buttonaction.TemplateSettings{WorkspaceID: workspaceID, Template: ref}
	var heads []schema.TemplateExitSettings
	if err := s.db.WithContext(ctx).Raw(selectTemplateExitSQL, workspaceID, ref.Name, ref.Language).Scan(&heads).Error; err != nil {
		return buttonaction.TemplateSettings{}, err
	}
	if len(heads) == 1 {
		applyHead(&settings, heads[0])
	}
	var rows []schema.TemplateButtonAction
	if err := s.db.WithContext(ctx).Raw(selectTemplateActionsSQL, workspaceID, ref.Name, ref.Language).Scan(&rows).Error; err != nil {
		return buttonaction.TemplateSettings{}, err
	}
	for _, row := range rows {
		action, err := actionOf(ref, row.ButtonIndex, row.Kind, row.Params, row.Confirmation, row.ButtonText)
		if err != nil {
			return buttonaction.TemplateSettings{}, err
		}
		action.Version, action.UpdatedBy, action.UpdatedAt = row.Version, row.UpdatedBy, row.UpdatedAt
		settings.Actions = append(settings.Actions, action)
	}
	return settings.Normalized(), nil
}

func applyHead(settings *buttonaction.TemplateSettings, head schema.TemplateExitSettings) {
	settings.ExitConfirmation = head.Confirmation
	settings.Version, settings.UpdatedBy, settings.UpdatedAt = head.Version, head.UpdatedBy, head.UpdatedAt
}

func (s *TemplateSettingsStore) Save(ctx context.Context, settings *buttonaction.TemplateSettings, expectedVersion int64) error {
	if !exitcolumns.ValidID(settings.WorkspaceID) {
		return buttonaction.ErrWorkspaceRequired
	}
	if settings.Template.Empty() {
		return buttonaction.ErrTemplateNotFound
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		head, err := saveHead(tx, settings, expectedVersion)
		if err != nil {
			return err
		}
		ref := settings.Template
		if err := tx.Exec(deleteTemplateActionsSQL, settings.WorkspaceID, ref.Name, ref.Language).Error; err != nil {
			return err
		}
		rows, err := actionRows(settings, head)
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			if err := tx.Create(&rows).Error; err != nil {
				return err
			}
		}
		settings.Version, settings.UpdatedAt = head.Version, head.UpdatedAt
		for i := range settings.Actions {
			settings.Actions[i].Version, settings.Actions[i].UpdatedBy, settings.Actions[i].UpdatedAt = head.Version, settings.UpdatedBy, head.UpdatedAt
		}
		return nil
	})
}

func saveHead(tx *gorm.DB, settings *buttonaction.TemplateSettings, expectedVersion int64) (savedHead, error) {
	ref := settings.Template
	var saved []savedHead
	var err error
	if expectedVersion == buttonaction.FirstVersion {
		err = tx.Raw(insertTemplateExitSQL, settings.WorkspaceID, ref.Name, ref.Language, settings.ExitConfirmation, settings.UpdatedBy).Scan(&saved).Error
	} else {
		err = tx.Raw(updateTemplateExitSQL, settings.ExitConfirmation, settings.UpdatedBy,
			settings.WorkspaceID, ref.Name, ref.Language, expectedVersion).Scan(&saved).Error
	}
	if err != nil {
		return savedHead{}, err
	}
	if len(saved) != 1 {
		return savedHead{}, buttonaction.ErrVersionConflict
	}
	return saved[0], nil
}

func actionRows(settings *buttonaction.TemplateSettings, head savedHead) ([]schema.TemplateButtonAction, error) {
	rows := make([]schema.TemplateButtonAction, 0, len(settings.Actions))
	for _, action := range settings.Actions {
		params, err := json.Marshal(action.Params)
		if err != nil {
			return nil, err
		}
		rows = append(rows, schema.TemplateButtonAction{
			WorkspaceID: settings.WorkspaceID, TemplateName: settings.Template.Name, Language: settings.Template.Language,
			ButtonIndex: action.ButtonIndex, Kind: string(action.Kind), Params: params, Confirmation: action.Confirmation,
			ButtonText: action.ButtonText, Version: head.Version, UpdatedBy: settings.UpdatedBy, UpdatedAt: head.UpdatedAt,
		})
	}
	return rows, nil
}

func actionOf(ref buttonaction.TemplateRef, index int, kind string, rawParams []byte, confirmation, text string) (buttonaction.ButtonAction, error) {
	var params buttonaction.Params
	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &params); err != nil {
			return buttonaction.ButtonAction{}, err
		}
	}
	return buttonaction.ButtonAction{
		Template: ref, ButtonIndex: index, ButtonText: text, Kind: buttonaction.Kind(kind), Params: params, Confirmation: confirmation,
	}, nil
}
