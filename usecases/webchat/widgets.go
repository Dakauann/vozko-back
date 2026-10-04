package webchat

import (
	"context"
	"strings"

	"vozko/domain/pipeline"
	"vozko/domain/shared"
	wcdomain "vozko/domain/webchat"
)

type WidgetInput struct {
	Name           *string
	DepartmentID   *string
	Status         *wcdomain.Status
	AllowedOrigins *[]string

	AccentColor    *string
	Position       *wcdomain.Position
	LauncherLabel  *string
	WelcomeTitle   *string
	WelcomeMessage *string
	TeamName       *string
	AssistantName  *string

	IntakeName         *wcdomain.FieldRule
	IntakeEmail        *wcdomain.FieldRule
	IntakePhone        *wcdomain.FieldRule
	PrivacyPolicyURL   *string
	DefaultCountryCode *string

	AllowHumanRequest *bool
	AllowAttachments  *bool
	IdentityMode      *wcdomain.IdentityMode

	AgentID              *string
	WorkflowID           *string
	PipelineID           *string
	EnableAgentResponses *bool
	EnableWorkflow       *bool
	EnableAnalysis       *bool
	EnableAutoStaging    *bool
	EnableAutoMemory     *bool
}

type References struct {
	Agents      AgentFinder
	Workflows   WorkflowFinder
	Pipelines   PipelineFinder
	Departments DepartmentFinder
}

type Widgets struct {
	widgets wcdomain.WidgetRepository
	refs    References
}

func NewWidgets(widgets wcdomain.WidgetRepository, refs References) *Widgets {
	return &Widgets{widgets: widgets, refs: refs}
}

func (uc *Widgets) List(ctx context.Context, in wcdomain.ListWidgetsInput) (*shared.PaginatedResult[*wcdomain.Widget], error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, wcdomain.ErrWorkspaceIDRequired
	}
	return uc.widgets.ListByWorkspace(ctx, in)
}

func (uc *Widgets) Get(ctx context.Context, workspaceID, id string) (*wcdomain.Widget, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, wcdomain.ErrWorkspaceIDRequired
	}
	return uc.widgets.FindByID(ctx, workspaceID, id)
}

func (uc *Widgets) Create(ctx context.Context, workspaceID string, in WidgetInput) (*wcdomain.Widget, error) {
	publicKey, err := wcdomain.GeneratePublicKey()
	if err != nil {
		return nil, err
	}
	w := &wcdomain.Widget{WorkspaceID: workspaceID, PublicKey: publicKey}
	if in.IdentityMode != nil && in.IdentityMode.Verifies() {
		if w.IdentitySecret, err = wcdomain.GenerateIdentitySecret(); err != nil {
			return nil, err
		}
	}
	if err := uc.apply(ctx, w, in); err != nil {
		return nil, err
	}
	if err := uc.widgets.Create(ctx, w); err != nil {
		return nil, err
	}
	return w, nil
}

func (uc *Widgets) Update(ctx context.Context, workspaceID, id string, in WidgetInput) (*wcdomain.Widget, error) {
	w, err := uc.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if in.IdentityMode != nil && in.IdentityMode.Verifies() && w.IdentitySecret == "" {
		if w.IdentitySecret, err = wcdomain.GenerateIdentitySecret(); err != nil {
			return nil, err
		}
	}
	if err := uc.apply(ctx, w, in); err != nil {
		return nil, err
	}
	if err := uc.widgets.Update(ctx, w); err != nil {
		return nil, err
	}
	return w, nil
}

func (uc *Widgets) Delete(ctx context.Context, workspaceID, id string) error {
	if strings.TrimSpace(workspaceID) == "" {
		return wcdomain.ErrWorkspaceIDRequired
	}
	return uc.widgets.Delete(ctx, workspaceID, id)
}

func (uc *Widgets) RotateIdentitySecret(ctx context.Context, workspaceID, id string) (string, error) {
	w, err := uc.Get(ctx, workspaceID, id)
	if err != nil {
		return "", err
	}
	if w.IdentitySecret, err = wcdomain.GenerateIdentitySecret(); err != nil {
		return "", err
	}
	if err := uc.widgets.Update(ctx, w); err != nil {
		return "", err
	}
	return w.IdentitySecret, nil
}

func (uc *Widgets) RevealIdentitySecret(ctx context.Context, workspaceID, id string) (string, error) {
	w, err := uc.Get(ctx, workspaceID, id)
	if err != nil {
		return "", err
	}
	return w.IdentitySecret, nil
}

func (uc *Widgets) apply(ctx context.Context, w *wcdomain.Widget, in WidgetInput) error {
	setValue(&w.Name, in.Name)
	setValue(&w.AccentColor, in.AccentColor)
	setValue(&w.LauncherLabel, in.LauncherLabel)
	setValue(&w.WelcomeTitle, in.WelcomeTitle)
	setValue(&w.WelcomeMessage, in.WelcomeMessage)
	setValue(&w.TeamName, in.TeamName)
	setValue(&w.AssistantName, in.AssistantName)
	setValue(&w.PrivacyPolicyURL, in.PrivacyPolicyURL)
	setValue(&w.DefaultCountryCode, in.DefaultCountryCode)
	setValue(&w.Status, in.Status)
	setValue(&w.Position, in.Position)
	setValue(&w.IntakeName, in.IntakeName)
	setValue(&w.IntakeEmail, in.IntakeEmail)
	setValue(&w.IntakePhone, in.IntakePhone)
	setValue(&w.IdentityMode, in.IdentityMode)
	setValue(&w.AllowHumanRequest, in.AllowHumanRequest)
	setValue(&w.AllowAttachments, in.AllowAttachments)
	setValue(&w.EnableAgentResponses, in.EnableAgentResponses)
	setValue(&w.EnableWorkflow, in.EnableWorkflow)
	setValue(&w.EnableAnalysis, in.EnableAnalysis)
	setValue(&w.EnableAutoStaging, in.EnableAutoStaging)
	setValue(&w.EnableAutoMemory, in.EnableAutoMemory)
	if in.AllowedOrigins != nil {
		w.AllowedOrigins = append([]string(nil), (*in.AllowedOrigins)...)
	}
	if in.DepartmentID != nil {
		w.DepartmentID = shared.OptionalID(in.DepartmentID)
	}
	if in.AgentID != nil {
		w.AgentID = shared.OptionalID(in.AgentID)
	}
	if in.WorkflowID != nil {
		w.WorkflowID = shared.OptionalID(in.WorkflowID)
	}
	if in.PipelineID != nil {
		w.PipelineID = shared.OptionalID(in.PipelineID)
	}

	w.Normalize()
	if err := w.Validate(); err != nil {
		return err
	}
	return uc.refs.ownedBy(ctx, w)
}

func (r References) ownedBy(ctx context.Context, w *wcdomain.Widget) error {
	ws := w.WorkspaceID
	if w.AgentID != nil {
		a, err := r.Agents.FindByID(*w.AgentID)
		if err != nil || a == nil || a.WorkspaceID != ws {
			return wcdomain.ErrReferenceNotInWorkspace
		}
	}
	if w.WorkflowID != nil {
		wf, err := r.Workflows.FindByID(*w.WorkflowID)
		if err != nil || wf == nil || wf.WorkspaceID != ws {
			return wcdomain.ErrReferenceNotInWorkspace
		}
	}
	if w.PipelineID != nil {
		p, err := r.Pipelines.GetByID(ws, *w.PipelineID)
		if err != nil || p == nil || p.WorkspaceID != ws || p.ObjectType != pipeline.ObjectConversation {
			return wcdomain.ErrReferenceNotInWorkspace
		}
	}
	if w.DepartmentID != nil {
		d, err := r.Departments.GetDepartmentByID(*w.DepartmentID)
		if err != nil || d == nil || d.WorkspaceID != ws {
			return wcdomain.ErrReferenceNotInWorkspace
		}
	}
	return nil
}

func setValue[T any](dst *T, src *T) {
	if src != nil {
		*dst = *src
	}
}
