package buttonaction_repository

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"

	"vozko/domain/buttonaction"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/exitcolumns"
)

const (
	selectCampaignRoutesSQL = `SELECT campaign_id, workspace_id, template_name, language, routes, version, updated_by, updated_at, exit_risk_acknowledged_by, exit_risk_acknowledged_at
		FROM whatsapp_campaign_button_routes WHERE campaign_id = ? AND workspace_id = ?`
	insertCampaignRoutesSQL = `INSERT INTO whatsapp_campaign_button_routes AS r (campaign_id, workspace_id, template_name, language, routes, version, updated_by, updated_at, exit_risk_acknowledged_by, exit_risk_acknowledged_at)
		VALUES (?, ?, ?, ?, ?, 2, ?, NOW(), ?, ?)
		ON CONFLICT (campaign_id) DO NOTHING
		RETURNING version, updated_at`
	updateCampaignRoutesSQL = `UPDATE whatsapp_campaign_button_routes SET template_name = ?, language = ?, routes = ?, version = version + 1, updated_by = ?, updated_at = NOW(),
			exit_risk_acknowledged_by = COALESCE(?, exit_risk_acknowledged_by), exit_risk_acknowledged_at = COALESCE(?, exit_risk_acknowledged_at)
		WHERE campaign_id = ? AND workspace_id = ? AND version = ?
		RETURNING version, updated_at`
)

type CampaignRoutesStore struct {
	db *gorm.DB
}

var _ buttonaction.CampaignRoutesStore = (*CampaignRoutesStore)(nil)

func NewCampaignRoutesStore(db *gorm.DB) *CampaignRoutesStore {
	return &CampaignRoutesStore{db: db}
}

type storedRoute struct {
	ButtonIndex  int                 `json:"buttonIndex"`
	ButtonText   string              `json:"buttonText"`
	Kind         string              `json:"kind"`
	Params       buttonaction.Params `json:"params"`
	Confirmation string              `json:"confirmation,omitempty"`
}

func (s *CampaignRoutesStore) Get(ctx context.Context, workspaceID, campaignID string) (buttonaction.CampaignRoutes, error) {
	if !exitcolumns.ValidID(workspaceID) {
		return buttonaction.CampaignRoutes{}, buttonaction.ErrWorkspaceRequired
	}
	if !exitcolumns.ValidID(campaignID) {
		return buttonaction.CampaignRoutes{}, buttonaction.ErrCampaignNotFound
	}
	routes := buttonaction.CampaignRoutes{WorkspaceID: workspaceID, CampaignID: campaignID}
	var rows []schema.WhatsAppCampaignButtonRoutes
	if err := s.db.WithContext(ctx).Raw(selectCampaignRoutesSQL, campaignID, workspaceID).Scan(&rows).Error; err != nil {
		return buttonaction.CampaignRoutes{}, err
	}
	if len(rows) == 1 {
		if err := applyRoutesRow(&routes, rows[0]); err != nil {
			return buttonaction.CampaignRoutes{}, err
		}
	}
	return routes.Normalized(), nil
}

func applyRoutesRow(routes *buttonaction.CampaignRoutes, row schema.WhatsAppCampaignButtonRoutes) error {
	ref := buttonaction.TemplateRef{Name: row.TemplateName, Language: row.Language}
	var stored []storedRoute
	if len(row.Routes) > 0 {
		if err := json.Unmarshal(row.Routes, &stored); err != nil {
			return err
		}
	}
	for _, r := range stored {
		routes.Routes = append(routes.Routes, buttonaction.ButtonAction{
			Template: ref, ButtonIndex: r.ButtonIndex, ButtonText: r.ButtonText, Kind: buttonaction.Kind(r.Kind), Params: r.Params,
			Confirmation: r.Confirmation, Version: row.Version, UpdatedBy: row.UpdatedBy, UpdatedAt: row.UpdatedAt,
		})
	}
	routes.Template, routes.Version, routes.UpdatedBy, routes.UpdatedAt = ref, row.Version, row.UpdatedBy, row.UpdatedAt
	routes.ExitRisk = exitcolumns.AcknowledgementOf(row.ExitRiskAcknowledgedBy, row.ExitRiskAcknowledgedAt)
	return nil
}

func (s *CampaignRoutesStore) Save(ctx context.Context, routes *buttonaction.CampaignRoutes, expectedVersion int64) error {
	if !exitcolumns.ValidID(routes.WorkspaceID) {
		return buttonaction.ErrWorkspaceRequired
	}
	if !exitcolumns.ValidID(routes.CampaignID) {
		return buttonaction.ErrCampaignNotFound
	}
	if routes.Template.Empty() {
		return buttonaction.ErrTemplateNotFound
	}
	payload, err := json.Marshal(storedRoutes(routes.Routes))
	if err != nil {
		return err
	}
	by, at := exitcolumns.AcknowledgementColumns(routes.ExitRisk)
	ref := routes.Template
	var saved []savedHead
	if expectedVersion == buttonaction.FirstVersion {
		err = s.db.WithContext(ctx).Raw(insertCampaignRoutesSQL, routes.CampaignID, routes.WorkspaceID, ref.Name, ref.Language, payload,
			routes.UpdatedBy, by, at).Scan(&saved).Error
	} else {
		err = s.db.WithContext(ctx).Raw(updateCampaignRoutesSQL, ref.Name, ref.Language, payload, routes.UpdatedBy, by, at,
			routes.CampaignID, routes.WorkspaceID, expectedVersion).Scan(&saved).Error
	}
	if err != nil {
		return err
	}
	if len(saved) != 1 {
		return buttonaction.ErrVersionConflict
	}
	routes.Version, routes.UpdatedAt = saved[0].Version, saved[0].UpdatedAt
	for i := range routes.Routes {
		routes.Routes[i].Version, routes.Routes[i].UpdatedBy, routes.Routes[i].UpdatedAt = routes.Version, routes.UpdatedBy, routes.UpdatedAt
	}
	return nil
}

func storedRoutes(actions []buttonaction.ButtonAction) []storedRoute {
	out := make([]storedRoute, 0, len(actions))
	for _, a := range actions {
		out = append(out, storedRoute{ButtonIndex: a.ButtonIndex, ButtonText: a.ButtonText, Kind: string(a.Kind), Params: a.Params, Confirmation: a.Confirmation})
	}
	return out
}
