package flows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"vozko/domain/whatsappform"
	"vozko/infra/meta"
)

const (
	maxFlowJSONBytes = 10 << 20
	maxPageRequests  = 200
	pageSize         = "100"
	flowFields       = "id,name,status,categories,validation_errors,json_version,preview.invalidate(false)"
	listFields       = "id,name,status"
	flowJSONAsset    = "FLOW_JSON"
	flowJSONFileName = "flow.json"

	firstFlowsErrorCode = 139000
	lastFlowsErrorCode  = 139004
)

var previewTimeLayouts = []string{"2006-01-02T15:04:05-0700", time.RFC3339}

var (
	errNotConfirmed     = errors.New("flows: Meta did not confirm the change")
	errNoFlowID         = errors.New("flows: Meta created no flow id")
	errNoPreview        = errors.New("flows: Meta returned no preview link")
	errNoFlowJSON       = errors.New("flows: the flow has no FLOW_JSON asset")
	errUnsafeDownload   = errors.New("flows: the FLOW_JSON download link is not https")
	errFlowJSONTooBig   = fmt.Errorf("flows: the FLOW_JSON asset is larger than %d bytes", maxFlowJSONBytes)
	errPagingUnfinished = fmt.Errorf("flows: listing did not finish after %d pages", maxPageRequests)
	errPagingNoCursor   = errors.New("flows: Meta returned a next page without a cursor")
)

type Gateway struct {
	client *meta.Client
}

func NewGateway(client *meta.Client) *Gateway {
	return &Gateway{client: client}
}

var _ whatsappform.FlowsGateway = (*Gateway)(nil)

type pointerWire struct {
	LineStart   int    `json:"line_start"`
	LineEnd     int    `json:"line_end"`
	ColumnStart int    `json:"column_start"`
	ColumnEnd   int    `json:"column_end"`
	Path        string `json:"path"`
}

type validationErrorWire struct {
	Error       string        `json:"error"`
	ErrorType   string        `json:"error_type"`
	Message     string        `json:"message"`
	LineStart   int           `json:"line_start"`
	LineEnd     int           `json:"line_end"`
	ColumnStart int           `json:"column_start"`
	ColumnEnd   int           `json:"column_end"`
	Pointers    []pointerWire `json:"pointers"`
}

type previewWire struct {
	PreviewURL string `json:"preview_url"`
	ExpiresAt  string `json:"expires_at"`
}

type flowWire struct {
	ID               meta.GraphID          `json:"id"`
	Name             string                `json:"name"`
	Status           string                `json:"status"`
	Success          *bool                 `json:"success"`
	ValidationErrors []validationErrorWire `json:"validation_errors"`
	Preview          *previewWire          `json:"preview"`
}

type successWire struct {
	Success bool `json:"success"`
}

type assetWire struct {
	Name        string `json:"name"`
	AssetType   string `json:"asset_type"`
	DownloadURL string `json:"download_url"`
}

type page[T any] struct {
	Data   []T `json:"data"`
	Paging struct {
		Cursors struct {
			After string `json:"after"`
		} `json:"cursors"`
		Next string `json:"next"`
	} `json:"paging"`
}

func (g *Gateway) Create(ctx context.Context, account whatsappform.FlowAccount, in whatsappform.CreateFlowInput) (whatsappform.RemoteFlow, error) {
	categories, err := json.Marshal(in.Categories)
	if err != nil {
		return whatsappform.RemoteFlow{}, err
	}
	form := url.Values{
		"name":       {in.Name},
		"categories": {string(categories)},
		"publish":    {strconv.FormatBool(in.Publish)},
	}
	if len(in.JSON) > 0 {
		form.Set("flow_json", string(in.JSON))
	}
	var out flowWire
	if err := g.do(ctx, meta.Request{Method: http.MethodPost, Path: "/" + url.PathEscape(account.MetaWABAID) + "/flows", Token: account.AccessToken, Form: form}, &out); err != nil {
		return whatsappform.RemoteFlow{}, err
	}
	if out.ID == "" {
		return whatsappform.RemoteFlow{}, errNoFlowID
	}
	return whatsappform.RemoteFlow{ID: out.ID.String(), Name: in.Name, ValidationErrors: validationErrors(out.ValidationErrors)}, nil
}

func (g *Gateway) UploadJSON(ctx context.Context, account whatsappform.FlowAccount, flowID string, raw []byte) ([]whatsappform.MetaValidationError, error) {
	var out flowWire
	err := g.do(ctx, meta.Request{
		Method:     http.MethodPost,
		Path:       flowPath(flowID) + "/assets",
		Token:      account.AccessToken,
		Form:       url.Values{"name": {flowJSONFileName}, "asset_type": {flowJSONAsset}},
		File:       &meta.FilePart{Field: "file", FileName: flowJSONFileName, ContentType: "application/json", Data: raw},
		Idempotent: true,
	}, &out)
	if err != nil {
		return nil, err
	}
	if out.Success != nil && !*out.Success && len(out.ValidationErrors) == 0 {
		return nil, errNotConfirmed
	}
	return validationErrors(out.ValidationErrors), nil
}

func (g *Gateway) Publish(ctx context.Context, account whatsappform.FlowAccount, flowID string) error {
	return g.confirm(ctx, account, flowPath(flowID)+"/publish")
}

func (g *Gateway) Deprecate(ctx context.Context, account whatsappform.FlowAccount, flowID string) error {
	return g.confirm(ctx, account, flowPath(flowID)+"/deprecate")
}

func (g *Gateway) Get(ctx context.Context, account whatsappform.FlowAccount, flowID string) (whatsappform.RemoteFlow, error) {
	return g.read(ctx, account, flowID, flowFields)
}

func (g *Gateway) Preview(ctx context.Context, account whatsappform.FlowAccount, flowID string, refresh bool) (whatsappform.RemoteFlow, error) {
	flow, err := g.read(ctx, account, flowID, "preview.invalidate("+strconv.FormatBool(refresh)+")")
	if err != nil {
		return whatsappform.RemoteFlow{}, err
	}
	if flow.PreviewURL == "" {
		return whatsappform.RemoteFlow{}, errNoPreview
	}
	if flow.ID == "" {
		flow.ID = flowID
	}
	return flow, nil
}

func (g *Gateway) FindByName(ctx context.Context, account whatsappform.FlowAccount, name string) (*whatsappform.RemoteFlow, error) {
	var found *whatsappform.RemoteFlow
	err := g.eachFlow(ctx, account, func(flow whatsappform.RemoteFlow) bool {
		if flow.Name == name {
			found = &flow
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

func (g *Gateway) List(ctx context.Context, account whatsappform.FlowAccount) ([]whatsappform.RemoteFlow, error) {
	var flows []whatsappform.RemoteFlow
	err := g.eachFlow(ctx, account, func(flow whatsappform.RemoteFlow) bool {
		flows = append(flows, flow)
		return true
	})
	if err != nil {
		return nil, err
	}
	return flows, nil
}

func (g *Gateway) DownloadJSON(ctx context.Context, account whatsappform.FlowAccount, flowID string) ([]byte, error) {
	var assets page[assetWire]
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: flowPath(flowID) + "/assets", Token: account.AccessToken}, &assets); err != nil {
		return nil, err
	}
	link := ""
	for _, asset := range assets.Data {
		if asset.AssetType == flowJSONAsset {
			link = asset.DownloadURL
			break
		}
	}
	if link == "" {
		return nil, errNoFlowJSON
	}
	target, err := url.Parse(link)
	if err != nil || target.Scheme != "https" || target.Host == "" {
		return nil, errUnsafeDownload
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	if g.isGraphHost(target.Host) {
		req.Header.Set("Authorization", "Bearer "+account.AccessToken)
	}
	var raw json.RawMessage
	if err := g.client.DoRaw(ctx, req, &raw); err != nil {
		return nil, err
	}
	if len(raw) > maxFlowJSONBytes {
		return nil, errFlowJSONTooBig
	}
	return raw, nil
}

func (g *Gateway) isGraphHost(host string) bool {
	base, err := url.Parse(g.client.BaseURL())
	return err == nil && strings.EqualFold(base.Host, host)
}

func (g *Gateway) confirm(ctx context.Context, account whatsappform.FlowAccount, path string) error {
	var out successWire
	if err := g.do(ctx, meta.Request{Method: http.MethodPost, Path: path, Token: account.AccessToken}, &out); err != nil {
		return err
	}
	if !out.Success {
		return errNotConfirmed
	}
	return nil
}

func (g *Gateway) read(ctx context.Context, account whatsappform.FlowAccount, flowID, fields string) (whatsappform.RemoteFlow, error) {
	var out flowWire
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: flowPath(flowID), Token: account.AccessToken, Query: url.Values{"fields": {fields}}}, &out); err != nil {
		return whatsappform.RemoteFlow{}, err
	}
	return remoteFlow(out)
}

func (g *Gateway) eachFlow(ctx context.Context, account whatsappform.FlowAccount, visit func(whatsappform.RemoteFlow) bool) error {
	after := ""
	for range maxPageRequests {
		query := url.Values{"fields": {listFields}, "limit": {pageSize}}
		if after != "" {
			query.Set("after", after)
		}
		var out page[flowWire]
		if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: "/" + url.PathEscape(account.MetaWABAID) + "/flows", Token: account.AccessToken, Query: query}, &out); err != nil {
			return err
		}
		for _, wire := range out.Data {
			flow, err := remoteFlow(wire)
			if err != nil {
				return err
			}
			if !visit(flow) {
				return nil
			}
		}
		if out.Paging.Next == "" {
			return nil
		}
		if out.Paging.Cursors.After == "" {
			return errPagingNoCursor
		}
		after = out.Paging.Cursors.After
	}
	return errPagingUnfinished
}

func (g *Gateway) do(ctx context.Context, req meta.Request, out any) error {
	return rejection(g.client.Do(ctx, req, out))
}

func rejection(err error) error {
	me, ok := meta.AsError(err)
	if !ok || !isRefusal(me) {
		return err
	}
	message := me.Message
	if me.UserMsg != "" {
		message = me.UserMsg
	}
	return &whatsappform.MetaRejection{Code: me.Code, Message: message}
}

func isRefusal(me *meta.Error) bool {
	if me.Code >= firstFlowsErrorCode && me.Code <= lastFlowsErrorCode {
		return true
	}
	if me.Code == meta.CodeInvalidParam && me.HTTPStatus < http.StatusInternalServerError {
		return true
	}
	return me.HTTPStatus >= http.StatusBadRequest && me.HTTPStatus < http.StatusInternalServerError && !me.Retryable()
}

func flowPath(flowID string) string {
	return "/" + url.PathEscape(flowID)
}

func remoteFlow(wire flowWire) (whatsappform.RemoteFlow, error) {
	flow := whatsappform.RemoteFlow{
		ID:               wire.ID.String(),
		Name:             wire.Name,
		Status:           wire.Status,
		ValidationErrors: validationErrors(wire.ValidationErrors),
	}
	if wire.Preview == nil {
		return flow, nil
	}
	flow.PreviewURL = wire.Preview.PreviewURL
	if wire.Preview.ExpiresAt == "" {
		return flow, nil
	}
	expires, err := parsePreviewTime(wire.Preview.ExpiresAt)
	if err != nil {
		return whatsappform.RemoteFlow{}, err
	}
	flow.PreviewExpiresAt = &expires
	return flow, nil
}

func parsePreviewTime(raw string) (time.Time, error) {
	for _, layout := range previewTimeLayouts {
		if at, err := time.Parse(layout, raw); err == nil {
			return at.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("flows: preview expiry %q is not a known time format", raw)
}

func validationErrors(wire []validationErrorWire) []whatsappform.MetaValidationError {
	if len(wire) == 0 {
		return nil
	}
	out := make([]whatsappform.MetaValidationError, 0, len(wire))
	for _, w := range wire {
		message := strings.TrimSpace(w.Message)
		if message == "" {
			message = strings.TrimSpace(w.Error)
		}
		var paths []string
		for _, pointer := range w.Pointers {
			if path := strings.TrimSpace(pointer.Path); path != "" {
				paths = append(paths, path)
			}
		}
		out = append(out, whatsappform.MetaValidationError{Message: message, Paths: paths})
	}
	return out
}
