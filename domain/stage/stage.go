package stage

import (
	"errors"
	"strings"
	"time"
)

type Stage struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspaceId"`
	CampaignID   string    `json:"campaignId,omitempty"`
	CampaignType string    `json:"campaignType,omitempty"`
	PipelineID   string    `json:"pipelineId,omitempty"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Color        string    `json:"color,omitempty"`
	IsDefault    bool      `json:"isDefault"`
	IsInitial    bool      `json:"isInitial"`
	IsWon        bool      `json:"isWon"`
	IsLost       bool      `json:"isLost"`
	Probability  *int      `json:"probability,omitempty"`
	RotDays      *int      `json:"rotDays,omitempty"`
	DefaultKey   string    `json:"defaultKey,omitempty"`
	Position     int       `json:"position"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type EntryStage struct {
	ID          string    `json:"id"`
	StageID     string    `json:"stageId"`
	EntryID     string    `json:"entryId"`
	EntryType   string    `json:"entryType"`
	WorkspaceID string    `json:"workspaceId"`
	CreatedAt   time.Time `json:"createdAt"`

	StageName  string `json:"stageName,omitempty"`
	StageColor string `json:"stageColor,omitempty"`
}

const (
	DefaultStageRecebido      = "recebido"
	DefaultStageEmAtendimento = "em atendimento"
	DefaultStageAgendamento   = "agendamento"
	DefaultStageFinalizado    = "finalizado"
)

var DefaultStages = []struct {
	Key         string
	Name        string
	Description string
	Color       string
	Position    int
}{
	{Key: DefaultStageRecebido, Name: DefaultStageRecebido, Description: "Lead recém recebido, ainda sem contato ou interação.", Color: "#3b82f6", Position: 1},
	{Key: DefaultStageEmAtendimento, Name: DefaultStageEmAtendimento, Description: "Lead em atendimento ativo pelo agente de IA ou atendente humano.", Color: "#f59e0b", Position: 2},
	{Key: DefaultStageAgendamento, Name: DefaultStageAgendamento, Description: "Lead com reunião, pagamento ou retorno agendado.", Color: "#8b5cf6", Position: 3},
	{Key: DefaultStageFinalizado, Name: DefaultStageFinalizado, Description: "Atendimento concluído, lead finalizado.", Color: "#10b981", Position: 4},
}

var (
	ErrTagNotFound      = errors.New("tag not found")
	ErrTagNameRequired  = errors.New("tag name is required")
	ErrTagDescRequired  = errors.New("tag description is required")
	ErrTagNameExists    = errors.New("tag with this name already exists for this user")
	ErrTagDefaultDelete = errors.New("cannot delete a default tag")
	ErrTagDefaultUpdate = errors.New("cannot edit a default tag")
	ErrEntryTagNotFound = errors.New("entry tag assignment not found")
	ErrEntryTagExists   = errors.New("this tag is already assigned to this entry")
	ErrEntryNotFound    = errors.New("entry not found")
	ErrInvalidEntryType = errors.New("entry_type must be 'voice', 'whatsapp', or 'support'")
	ErrUnauthorized     = errors.New("unauthorized access to this tag")

	ErrTagGroupNotFound     = errors.New("tag group not found")
	ErrTagGroupNameRequired = errors.New("tag group name is required")
)

func (t *Stage) Normalize() {
	t.Name = strings.TrimSpace(strings.ToLower(t.Name))
	t.Color = strings.TrimSpace(t.Color)
}

func (t *Stage) Validate() error {
	if strings.TrimSpace(t.Name) == "" {
		return ErrTagNameRequired
	}
	if strings.TrimSpace(t.Description) == "" {
		return ErrTagDescRequired
	}
	return nil
}

func ValidateEntryType(entryType string) error {
	if entryType != "voice" && entryType != "whatsapp" && entryType != "support" {
		return ErrInvalidEntryType
	}
	return nil
}

type StageGroup struct {
	ID           string           `json:"id"`
	WorkspaceID  string           `json:"workspaceId"`
	DepartmentID string           `json:"departmentId,omitempty"`
	Name         string           `json:"name"`
	Items        []StageGroupItem `json:"items"`
}

type StageGroupItem struct {
	ID           string `json:"id"`
	StageGroupID string `json:"StageGroupID"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Color        string `json:"color"`
	Position     int    `json:"position"`
}
