package lead

import (
	"time"

	"vozko/domain/leadimport"
	"vozko/domain/unofficial_whatsapp"
	lead_usecase "vozko/usecases/lead"
)

type CreateLeadImportRequest struct {
	MediaID  string `json:"mediaId" example:"6f1e2d3c-4b5a-4968-8776-5a4b3c2d1e0f"`
	FileName string `json:"fileName,omitempty" example:"contatos.csv"`
}

type LeadImportColumnRequest struct {
	Index int    `json:"index" example:"0"`
	Field string `json:"field" example:"number"`
}

type LeadImportDryRunRequest struct {
	Columns           []LeadImportColumnRequest `json:"columns"`
	OnExisting        string                    `json:"onExisting,omitempty" example:"fill_empty" enums:"fill_empty,skip"`
	SeedInbox         bool                      `json:"seedInbox,omitempty"`
	SeedConversations *SeedConversationsSpec    `json:"seedConversations,omitempty" swaggerignore:"true"`
}

type LeadImportColumnResponse struct {
	Index  int    `json:"index" example:"0"`
	Header string `json:"header" example:"telefone"`
	Field  string `json:"field" example:"number"`
}

type LeadImportPreviewResponse struct {
	Headers []string                   `json:"headers"`
	Sample  [][]string                 `json:"sample"`
	Columns []LeadImportColumnResponse `json:"columns"`
}

type LeadImportFieldResponse struct {
	Key        string `json:"key" example:"phone:landline"`
	Group      string `json:"group" example:"phones" enums:"identity,phones,contact,address,owner,consent,custom,family"`
	Label      string `json:"label,omitempty" example:"Interesse"`
	Requires   string `json:"requires,omitempty" example:"leads:read_addresses"`
	Allowed    bool   `json:"allowed"`
	Sensitive  bool   `json:"sensitive"`
	Repeatable bool   `json:"repeatable"`
}

type LeadImportOptionsResponse struct {
	FillEmpty         bool `json:"fillEmpty"`
	SeedInbox         bool `json:"seedInbox"`
	SeedConversations bool `json:"seedConversations"`
}

type LeadImportSettingsResponse struct {
	Columns           []LeadImportColumnResponse `json:"columns"`
	OnExisting        string                     `json:"onExisting" example:"fill_empty"`
	SeedInbox         bool                       `json:"seedInbox"`
	SeedConversations bool                       `json:"seedConversations"`
	SeedScript        *SeedConversationsSpec     `json:"seedScript,omitempty" swaggerignore:"true"`
}

type LeadImportCountsResponse struct {
	Rows             int            `json:"rows" example:"8214"`
	Created          int            `json:"created" example:"4611"`
	Enriched         int            `json:"enriched" example:"482"`
	Unchanged        int            `json:"unchanged" example:"30"`
	Skipped          int            `json:"skipped" example:"0"`
	Rejected         int            `json:"rejected" example:"71"`
	Conflicting      int            `json:"conflicting" example:"12"`
	Blocked          int            `json:"blocked" example:"3"`
	AddressesAdded   int            `json:"addressesAdded" example:"5000"`
	AddressesLocated int            `json:"addressesLocated" example:"120"`
	AddressesFilled  int            `json:"addressesFilled" example:"340"`
	LinksPlanned     int            `json:"linksPlanned" example:"1130"`
	LinksCreated     int            `json:"linksCreated" example:"1126"`
	NoAddress        *int           `json:"noAddress,omitempty" example:"197"`
	Issues           map[string]int `json:"issues"`
}

type LeadImportSeedResponse struct {
	Queued         int    `json:"queued"`
	ScriptedQueued int    `json:"scriptedQueued,omitempty"`
	Error          string `json:"error,omitempty" enums:"seed_forbidden,seed_unavailable,seed_failed"`
	ScriptError    string `json:"scriptError,omitempty" enums:"script_forbidden"`
	Unconfirmed    int    `json:"unconfirmed,omitempty" example:"0"`
}

type LeadImportSummaryResponse struct {
	ID          string                    `json:"id"`
	Status      string                    `json:"status" example:"importing" enums:"uploaded,analyzing,analyzed,importing,done,failed"`
	Stage       string                    `json:"stage,omitempty" example:"rows" enums:"rows,links,seed"`
	FileName    string                    `json:"fileName" example:"contatos.csv"`
	SizeBytes   int64                     `json:"sizeBytes"`
	TotalRows   int                       `json:"totalRows" example:"8214"`
	Processed   int                       `json:"processed" example:"5093"`
	DryRun      *LeadImportCountsResponse `json:"dryRun,omitempty"`
	Result      *LeadImportCountsResponse `json:"result,omitempty"`
	Seed        *LeadImportSeedResponse   `json:"seed,omitempty"`
	FailureCode string                    `json:"failureCode,omitempty" enums:"stalled,file_unavailable,forbidden,internal,interrupted"`
	CreatedAt   string                    `json:"createdAt"`
	StartedAt   *string                   `json:"startedAt,omitempty"`
	FinishedAt  *string                   `json:"finishedAt,omitempty"`
	ExpiresAt   string                    `json:"expiresAt"`
}

type LeadImportPlacementResponse struct {
	OnMap         int `json:"onMap" example:"3088"`
	Approximate   int `json:"approximate" example:"1920"`
	Pending       int `json:"pending" example:"709"`
	NotFound      int `json:"notFound" example:"12"`
	QuotaExceeded int `json:"quotaExceeded" example:"0"`
	Refused       int `json:"refused" example:"0"`
}

type LeadImportResponse struct {
	LeadImportSummaryResponse
	Preview   LeadImportPreviewResponse    `json:"preview"`
	Fields    []LeadImportFieldResponse    `json:"fields"`
	Options   LeadImportOptionsResponse    `json:"options"`
	Settings  *LeadImportSettingsResponse  `json:"settings,omitempty"`
	Placement *LeadImportPlacementResponse `json:"placement,omitempty"`
}

type LeadImportLimitsResponse struct {
	MaxBytes               int `json:"maxBytes" example:"20971520"`
	MaxMegabytes           int `json:"maxMegabytes" example:"20"`
	MaxRows                int `json:"maxRows" example:"200000"`
	MaxSeededConversations int `json:"maxSeededConversations" example:"200"`
	RetentionDays          int `json:"retentionDays" example:"7"`
	MaxUnusedUploads       int `json:"maxUnusedUploads" example:"5"`
}

type LeadImportListResponse struct {
	Items  []LeadImportSummaryResponse `json:"items"`
	Limits LeadImportLimitsResponse    `json:"limits"`
}

func columnResponses(columns []leadimport.Column, headers []string) []LeadImportColumnResponse {
	out := make([]LeadImportColumnResponse, 0, len(columns))
	for _, c := range columns {
		header := c.Header
		if header == "" && c.Index >= 0 && c.Index < len(headers) {
			header = headers[c.Index]
		}
		out = append(out, LeadImportColumnResponse{Index: c.Index, Header: header, Field: c.Field})
	}
	return out
}

func countsResponse(c *leadimport.Counts) *LeadImportCountsResponse {
	if c == nil {
		return nil
	}
	issues := c.Issues
	if issues == nil {
		issues = map[string]int{}
	}
	return &LeadImportCountsResponse{
		Rows: c.Rows, Created: c.Created, Enriched: c.Enriched, Unchanged: c.Unchanged, Skipped: c.Skipped, Rejected: c.Rejected,
		Conflicting: c.Conflicting, Blocked: c.Blocked, AddressesAdded: c.AddressesAdded, AddressesLocated: c.AddressesLocated, AddressesFilled: c.AddressesFilled,
		LinksPlanned: c.LinksPlanned, LinksCreated: c.LinksCreated, NoAddress: c.NoAddress, Issues: issues,
	}
}

func optionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	formatted := fmtRFC3339(*t)
	return &formatted
}

func seedResponse(s *leadimport.SeedOutcome) *LeadImportSeedResponse {
	if s == nil {
		return nil
	}
	return &LeadImportSeedResponse{Queued: s.Queued, ScriptedQueued: s.ScriptedQueued, Error: s.Error, ScriptError: s.ScriptError, Unconfirmed: s.Unconfirmed}
}

func seedScriptSpec(s *unofficial_whatsapp.SeedScript) *SeedConversationsSpec {
	if s == nil {
		return nil
	}
	out := &SeedConversationsSpec{Bodies: append([]string{}, s.Bodies...), MaxMessages: s.MaxMessages, Context: s.Context}
	if a := s.Attachment; a != nil {
		out.Attachment = &SeedAttachmentSpec{MediaID: a.MediaID, Kind: string(a.Kind)}
	}
	return out
}

func toLeadImportSummary(j *leadimport.Job) LeadImportSummaryResponse {
	return LeadImportSummaryResponse{
		ID: j.ID, Status: string(j.Status), Stage: string(j.Stage), FileName: j.File.Name, SizeBytes: j.File.SizeBytes,
		TotalRows: j.TotalRows, Processed: j.Processed, FailureCode: string(j.FailureCode),
		DryRun: countsResponse(j.DryRun), Result: countsResponse(j.Result), Seed: seedResponse(j.Seed),
		CreatedAt: fmtRFC3339(j.CreatedAt), StartedAt: optionalTime(j.StartedAt), FinishedAt: optionalTime(j.FinishedAt), ExpiresAt: fmtRFC3339(j.ExpiresAt),
	}
}

func toLeadImportListResponse(jobs []leadimport.Job, limits leadimport.Limits) LeadImportListResponse {
	out := LeadImportListResponse{Items: make([]LeadImportSummaryResponse, 0, len(jobs)), Limits: LeadImportLimitsResponse{
		MaxBytes: limits.MaxBytes, MaxMegabytes: limits.MaxMegabytes, MaxRows: limits.MaxRows,
		MaxSeededConversations: limits.MaxSeededConversations, RetentionDays: limits.RetentionDays, MaxUnusedUploads: limits.MaxUnusedUploads,
	}}
	for idx := range jobs {
		out.Items = append(out.Items, toLeadImportSummary(&jobs[idx]))
	}
	return out
}

func toLeadImportResponse(detail lead_usecase.ImportDetail, catalog lead_usecase.ImportCatalog) LeadImportResponse {
	j := detail.Job
	out := LeadImportResponse{
		LeadImportSummaryResponse: toLeadImportSummary(j),
		Preview:                   LeadImportPreviewResponse{Headers: j.Preview.Headers, Sample: j.Preview.Sample, Columns: columnResponses(j.Preview.Columns, j.Preview.Headers)},
		Fields:                    make([]LeadImportFieldResponse, 0, len(catalog.Fields)),
		Options:                   LeadImportOptionsResponse{FillEmpty: catalog.FillEmpty, SeedInbox: catalog.SeedInbox, SeedConversations: catalog.SeedScript},
	}
	if out.Preview.Headers == nil {
		out.Preview.Headers = []string{}
	}
	if out.Preview.Sample == nil {
		out.Preview.Sample = [][]string{}
	}
	for _, f := range catalog.Fields {
		requires := ""
		if f.Requires != "" {
			requires = "leads:" + string(f.Requires)
		}
		out.Fields = append(out.Fields, LeadImportFieldResponse{Key: f.Key, Group: string(f.Group), Label: f.Label, Requires: requires,
			Allowed: f.Allowed, Sensitive: f.Sensitive, Repeatable: f.Repeatable})
	}
	if s := j.Settings; s != nil {
		out.Settings = &LeadImportSettingsResponse{Columns: columnResponses(s.Columns, j.Preview.Headers), OnExisting: string(s.Policy),
			SeedInbox: s.SeedInbox, SeedConversations: s.Script != nil, SeedScript: seedScriptSpec(s.Script)}
	}
	if p := detail.Placement; p != nil {
		out.Placement = &LeadImportPlacementResponse{OnMap: p.OnMap, Approximate: p.Approximate, Pending: p.Pending,
			NotFound: p.NotFound, QuotaExceeded: p.QuotaExceeded, Refused: p.Refused}
	}
	return out
}
