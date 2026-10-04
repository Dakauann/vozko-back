package webchat

import (
	"time"

	wcdomain "vozko/domain/webchat"
	wcuc "vozko/usecases/webchat"
)

type WidgetResponse struct {
	ID                   string    `json:"id"`
	WorkspaceID          string    `json:"workspaceId"`
	DepartmentID         *string   `json:"departmentId,omitempty"`
	Name                 string    `json:"name"`
	PublicKey            string    `json:"publicKey"`
	Status               string    `json:"status" enums:"active,paused"`
	AllowedOrigins       []string  `json:"allowedOrigins"`
	AccentColor          string    `json:"accentColor"`
	Position             string    `json:"position" enums:"right,left"`
	LauncherLabel        string    `json:"launcherLabel,omitempty"`
	WelcomeTitle         string    `json:"welcomeTitle,omitempty"`
	WelcomeMessage       string    `json:"welcomeMessage,omitempty"`
	TeamName             string    `json:"teamName,omitempty"`
	AssistantName        string    `json:"assistantName,omitempty"`
	IntakeName           string    `json:"intakeName" enums:"hidden,optional,required"`
	IntakeEmail          string    `json:"intakeEmail" enums:"hidden,optional,required"`
	IntakePhone          string    `json:"intakePhone" enums:"hidden,optional,required"`
	PrivacyPolicyURL     string    `json:"privacyPolicyUrl,omitempty"`
	DefaultCountryCode   string    `json:"defaultCountryCode"`
	AllowHumanRequest    bool      `json:"allowHumanRequest"`
	AllowAttachments     bool      `json:"allowAttachments"`
	IdentityMode         string    `json:"identityMode" enums:"off,optional,required"`
	AgentID              *string   `json:"agentId,omitempty"`
	WorkflowID           *string   `json:"workflowId,omitempty"`
	PipelineID           *string   `json:"pipelineId,omitempty"`
	EnableAgentResponses bool      `json:"enableAgentResponses"`
	EnableWorkflow       bool      `json:"enableWorkflow"`
	EnableAnalysis       bool      `json:"enableAnalysis"`
	EnableAutoStaging    bool      `json:"enableAutoStaging"`
	EnableAutoMemory     bool      `json:"enableAutoMemory"`
	LoaderURL            string    `json:"loaderUrl,omitempty"`
	Snippet              string    `json:"snippet,omitempty"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

func toWidgetResponse(w *wcdomain.Widget, publicBase string) WidgetResponse {
	out := WidgetResponse{
		ID: w.ID, WorkspaceID: w.WorkspaceID, DepartmentID: w.DepartmentID, Name: w.Name, PublicKey: w.PublicKey,
		Status: string(w.Status), AllowedOrigins: w.AllowedOrigins, AccentColor: w.AccentColor,
		Position: string(w.Position), LauncherLabel: w.LauncherLabel, WelcomeTitle: w.WelcomeTitle,
		WelcomeMessage: w.WelcomeMessage, TeamName: w.TeamName, AssistantName: w.AssistantName,
		IntakeName: string(w.IntakeName), IntakeEmail: string(w.IntakeEmail), IntakePhone: string(w.IntakePhone),
		PrivacyPolicyURL: w.PrivacyPolicyURL, DefaultCountryCode: w.DefaultCountryCode,
		AllowHumanRequest: w.AllowHumanRequest, AllowAttachments: w.AllowAttachments,
		IdentityMode: string(w.IdentityMode), AgentID: w.AgentID, WorkflowID: w.WorkflowID, PipelineID: w.PipelineID,
		EnableAgentResponses: w.EnableAgentResponses, EnableWorkflow: w.EnableWorkflow,
		EnableAnalysis: w.EnableAnalysis, EnableAutoStaging: w.EnableAutoStaging, EnableAutoMemory: w.EnableAutoMemory,
		CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt,
	}
	if publicBase != "" {
		out.LoaderURL = publicBase + LoaderPath
		out.Snippet = `<script async src="` + out.LoaderURL + `" data-key="` + w.PublicKey + `"></script>`
	}
	if out.AllowedOrigins == nil {
		out.AllowedOrigins = []string{}
	}
	return out
}

type ChallengeResponse struct {
	Token     string    `json:"token"`
	Bits      int       `json:"bits"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type IntakeRulesResponse struct {
	Name             string `json:"name" enums:"hidden,optional,required"`
	Email            string `json:"email" enums:"hidden,optional,required"`
	Phone            string `json:"phone" enums:"hidden,optional,required"`
	PrivacyPolicyURL string `json:"privacyPolicyUrl,omitempty"`
}

type PublicWidgetResponse struct {
	Name              string              `json:"name"`
	AccentColor       string              `json:"accentColor"`
	Position          string              `json:"position" enums:"right,left"`
	LauncherLabel     string              `json:"launcherLabel,omitempty"`
	WelcomeTitle      string              `json:"welcomeTitle,omitempty"`
	WelcomeMessage    string              `json:"welcomeMessage,omitempty"`
	TeamName          string              `json:"teamName,omitempty"`
	AssistantName     string              `json:"assistantName,omitempty"`
	Intake            IntakeRulesResponse `json:"intake"`
	AllowHumanRequest bool                `json:"allowHumanRequest"`
	AllowAttachments  bool                `json:"allowAttachments"`
	IdentityMode      string              `json:"identityMode" enums:"off,optional,required"`
}

type VisitorStateResponse struct {
	IntakePending  bool   `json:"intakePending"`
	IntakeRequired bool   `json:"intakeRequired"`
	Verified       bool   `json:"verified"`
	Name           string `json:"name,omitempty"`
	Email          string `json:"email,omitempty"`
	Phone          string `json:"phone,omitempty"`
}

type SessionResponse struct {
	Token     string               `json:"token"`
	ExpiresAt time.Time            `json:"expiresAt"`
	Widget    PublicWidgetResponse `json:"widget"`
	Visitor   VisitorStateResponse `json:"visitor"`
}

type OptionResponse struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type MediaResponse struct {
	Kind     string `json:"kind"`
	URL      string `json:"url"`
	MimeType string `json:"mimeType,omitempty"`
	Filename string `json:"filename,omitempty"`
}

type MessageResponse struct {
	ID        string           `json:"id"`
	Author    string           `json:"author" enums:"visitor,team,assistant"`
	Text      string           `json:"text,omitempty"`
	Media     *MediaResponse   `json:"media,omitempty"`
	Options   []OptionResponse `json:"options,omitempty"`
	CreatedAt time.Time        `json:"createdAt"`
}

type HistoryResponse struct {
	Messages []MessageResponse `json:"messages"`
	Options  []OptionResponse  `json:"options,omitempty"`
	Human    bool              `json:"human"`
}

func toChallengeResponse(c wcdomain.Challenge) ChallengeResponse {
	return ChallengeResponse{Token: c.Token, Bits: c.Bits, ExpiresAt: c.ExpiresAt}
}

func toVisitorStateResponse(s wcuc.VisitorState) VisitorStateResponse {
	return VisitorStateResponse{
		IntakePending: s.IntakePending, IntakeRequired: s.IntakeRequired, Verified: s.Verified,
		Name: s.Name, Email: s.Email, Phone: s.Phone,
	}
}

func toSessionResponse(v *wcuc.SessionView) SessionResponse {
	w := v.Widget
	return SessionResponse{
		Token:     v.Token,
		ExpiresAt: v.ExpiresAt,
		Widget: PublicWidgetResponse{
			Name: w.Name, AccentColor: w.AccentColor, Position: string(w.Position), LauncherLabel: w.LauncherLabel,
			WelcomeTitle: w.WelcomeTitle, WelcomeMessage: w.WelcomeMessage, TeamName: w.TeamName,
			AssistantName: w.AssistantName,
			Intake: IntakeRulesResponse{
				Name: string(w.Intake.Name), Email: string(w.Intake.Email), Phone: string(w.Intake.Phone),
				PrivacyPolicyURL: w.Intake.PrivacyPolicyURL,
			},
			AllowHumanRequest: w.AllowHumanRequest, AllowAttachments: w.AllowAttachments,
			IdentityMode: string(w.IdentityMode),
		},
		Visitor: toVisitorStateResponse(v.Visitor),
	}
}

func toOptions(options []wcdomain.Option) []OptionResponse {
	if len(options) == 0 {
		return nil
	}
	out := make([]OptionResponse, len(options))
	for i, o := range options {
		out[i] = OptionResponse{ID: o.ID, Title: o.Title}
	}
	return out
}

func toMessageResponse(m wcdomain.VisitorMessage) MessageResponse {
	out := MessageResponse{ID: m.ID, Author: string(m.Author), Text: m.Text, Options: toOptions(m.Options), CreatedAt: m.CreatedAt}
	if m.Media != nil {
		out.Media = &MediaResponse{Kind: m.Media.Kind, URL: m.Media.URL, MimeType: m.Media.MimeType, Filename: m.Media.Filename}
	}
	return out
}

func toHistoryResponse(v *wcuc.HistoryView) HistoryResponse {
	messages := make([]MessageResponse, len(v.Messages))
	for i, m := range v.Messages {
		messages[i] = toMessageResponse(m)
	}
	return HistoryResponse{Messages: messages, Options: toOptions(v.Options), Human: v.Human}
}
