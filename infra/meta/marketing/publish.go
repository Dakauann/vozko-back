package marketing

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

var _ advertising.PublishGateway = (*Gateway)(nil)

func (g *Gateway) UploadImage(ctx context.Context, token, metaAccountID string, image []byte, fileName string) (string, error) {
	path, file, err := uploadPart(metaAccountID, "filename", image, fileName)
	if err != nil {
		return "", err
	}
	var out struct {
		Images map[string]struct {
			Hash string `json:"hash"`
		} `json:"images"`
	}
	req := meta.Request{Method: http.MethodPost, Path: path + "/adimages", Token: token, Idempotent: true, File: file}
	if err := g.do(ctx, req, &out); err != nil {
		return "", err
	}
	if len(out.Images) != 1 {
		return "", fmt.Errorf("marketing: adimages returned %d images for one upload", len(out.Images))
	}
	for _, uploaded := range out.Images {
		if uploaded.Hash == "" {
			return "", fmt.Errorf("marketing: adimages returned an empty hash")
		}
		return uploaded.Hash, nil
	}
	return "", fmt.Errorf("marketing: adimages returned no image")
}

func (g *Gateway) UploadVideo(ctx context.Context, token, metaAccountID string, video []byte, fileName string) (string, error) {
	path, file, err := uploadPart(metaAccountID, "source", video, fileName)
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("name", fileName)
	return g.created(ctx, meta.Request{Method: http.MethodPost, Path: path + "/advideos", Token: token, Form: form, File: file}, "ad video")
}

func uploadPart(metaAccountID, field string, data []byte, fileName string) (string, *meta.FilePart, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return "", nil, err
	}
	if len(data) == 0 {
		return "", nil, fmt.Errorf("marketing: %s upload is empty", field)
	}
	if strings.TrimSpace(fileName) == "" {
		return "", nil, fmt.Errorf("marketing: upload file name is required")
	}
	return path, &meta.FilePart{Field: field, FileName: fileName, ContentType: http.DetectContentType(data), Data: data}, nil
}

type graphVideo struct {
	ID     meta.GraphID `json:"id"`
	Status *struct {
		VideoStatus string `json:"video_status"`
	} `json:"status"`
	Thumbnails graphList[struct {
		URI         string `json:"uri"`
		IsPreferred bool   `json:"is_preferred"`
	}] `json:"thumbnails"`
}

var videoStates = map[string]advertising.VideoState{
	"processing": advertising.VideoProcessing,
	"ready":      advertising.VideoReady,
	"error":      advertising.VideoError,
}

func (g *Gateway) VideoStatus(ctx context.Context, token, videoID string) (*advertising.RemoteVideo, error) {
	path, err := objectPath(videoID)
	if err != nil {
		return nil, err
	}
	var row graphVideo
	if err := g.get(ctx, path, token, "id,status,thumbnails{uri,is_preferred}", &row); err != nil {
		return nil, err
	}
	if row.Status == nil {
		return nil, fmt.Errorf("marketing: video %s without status", videoID)
	}
	state, ok := videoStates[row.Status.VideoStatus]
	if !ok {
		return nil, fmt.Errorf("marketing: video %s has unknown status %q", videoID, row.Status.VideoStatus)
	}
	out := &advertising.RemoteVideo{ID: strings.TrimSpace(videoID), State: state}
	for _, thumb := range row.Thumbnails {
		if thumb.IsPreferred {
			out.ThumbnailURL = thumb.URI
			break
		}
	}
	return out, nil
}

func budgetField(b advertising.Budget) (string, error) {
	switch b.Kind {
	case advertising.BudgetDaily:
		return "daily_budget", nil
	case advertising.BudgetLifetime:
		return "lifetime_budget", nil
	}
	return "", fmt.Errorf("marketing: unknown budget kind %q", b.Kind)
}

func setBudget(form url.Values, b advertising.Budget) error {
	field, err := budgetField(b)
	if err != nil {
		return err
	}
	if b.Amount <= 0 {
		return fmt.Errorf("%w: %d", advertising.ErrInvalidBudget, b.Amount)
	}
	form.Set(field, strconv.FormatInt(b.Amount, 10))
	return nil
}

func setBid(form url.Values, b advertising.Bid) error {
	form.Set("bid_strategy", string(b.Strategy))
	switch b.Strategy {
	case advertising.BidLowestCost:
		return nil
	case advertising.BidCap, advertising.BidCostCap:
		if b.Amount <= 0 {
			return fmt.Errorf("marketing: %s needs a bid amount", b.Strategy)
		}
		form.Set("bid_amount", strconv.FormatInt(b.Amount, 10))
		return nil
	case advertising.BidMinROAS:
		constraints, err := jsonValue(map[string]int64{"roas_average_floor": b.ROASFloorParam()})
		if err != nil {
			return err
		}
		form.Set("bid_constraints", constraints)
		return nil
	}
	return fmt.Errorf("marketing: unknown bid strategy %q", b.Strategy)
}

func (g *Gateway) CreateCampaign(ctx context.Context, token, metaAccountID string, spec advertising.CampaignSpec) (string, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return "", err
	}
	categories := make([]string, 0, len(spec.SpecialCategories))
	for _, c := range spec.SpecialCategories {
		categories = append(categories, string(c))
	}
	encodedCategories, err := jsonValue(categories)
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("name", spec.Name)
	form.Set("objective", string(spec.Objective))
	form.Set("status", string(spec.Status))
	form.Set("special_ad_categories", encodedCategories)
	form.Set("buying_type", "AUCTION")
	if spec.Budget == nil {
		form.Set("is_adset_budget_sharing_enabled", "false")
	} else {
		if err := setBudget(form, *spec.Budget); err != nil {
			return "", err
		}
		if err := setBid(form, spec.Bid); err != nil {
			return "", err
		}
	}
	if spec.ProductCatalogID != "" {
		promoted, err := jsonValue(map[string]string{"product_catalog_id": spec.ProductCatalogID})
		if err != nil {
			return "", err
		}
		form.Set("promoted_object", promoted)
	}
	return g.created(ctx, meta.Request{Method: http.MethodPost, Path: path + "/campaigns", Token: token, Form: form}, "campaign")
}

type graphPromotedObject struct {
	PageID              string `json:"page_id,omitempty"`
	WhatsAppPhoneNumber string `json:"whatsapp_phone_number,omitempty"`
	PixelID             string `json:"pixel_id,omitempty"`
	CustomEventType     string `json:"custom_event_type,omitempty"`
	ApplicationID       string `json:"application_id,omitempty"`
	ObjectStoreURL      string `json:"object_store_url,omitempty"`
	ProductSetID        string `json:"product_set_id,omitempty"`
}

func promotedObjectOf(p advertising.PromotedObject) graphPromotedObject {
	return graphPromotedObject{
		PageID:              p.PageID,
		WhatsAppPhoneNumber: p.WhatsAppPhoneNumber,
		PixelID:             p.PixelID,
		CustomEventType:     string(p.PixelEvent),
		ApplicationID:       p.AppID,
		ObjectStoreURL:      p.AppStoreURL,
		ProductSetID:        p.ProductSetID,
	}
}

type graphDayPart struct {
	StartMinute  int    `json:"start_minute"`
	EndMinute    int    `json:"end_minute"`
	Days         []int  `json:"days"`
	TimezoneType string `json:"timezone_type"`
}

const viewerTimezone = "USER"

func setSchedule(form url.Values, parts []advertising.DayPart) error {
	schedule := make([]graphDayPart, 0, len(parts))
	for _, p := range parts {
		schedule = append(schedule, graphDayPart{StartMinute: p.StartMinute, EndMinute: p.EndMinute, Days: p.Days, TimezoneType: viewerTimezone})
	}
	encoded, err := jsonValue(schedule)
	if err != nil {
		return err
	}
	pacing := `["standard"]`
	if len(parts) > 0 {
		pacing = `["day_parting"]`
	}
	form.Set("adset_schedule", encoded)
	form.Set("pacing_type", pacing)
	return nil
}

func (g *Gateway) CreateAdSet(ctx context.Context, token, metaAccountID string, spec advertising.AdSetSpec) (string, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return "", err
	}
	targeting, err := targetingParam(spec.Targeting, spec.Placements)
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("name", spec.Name)
	form.Set("campaign_id", spec.CampaignID)
	form.Set("billing_event", spec.BillingEvent)
	form.Set("optimization_goal", string(spec.Goal))
	form.Set("status", string(spec.Status))
	form.Set("targeting", targeting)
	if spec.Budget != nil {
		if err := setBudget(form, *spec.Budget); err != nil {
			return "", err
		}
		if err := setBid(form, spec.Bid); err != nil {
			return "", err
		}
	}
	if destination := spec.Destination.MetaDestinationType(); destination != "" {
		form.Set("destination_type", destination)
	}
	if promoted := promotedObjectOf(spec.PromotedObject); promoted != (graphPromotedObject{}) {
		encoded, err := jsonValue(promoted)
		if err != nil {
			return "", err
		}
		form.Set("promoted_object", encoded)
	}
	if spec.DynamicCreative {
		form.Set("is_dynamic_creative", "true")
	}
	if spec.StartTime != nil {
		form.Set("start_time", timeParam(*spec.StartTime))
	}
	if spec.EndTime != nil {
		form.Set("end_time", timeParam(*spec.EndTime))
	}
	if len(spec.Schedule) > 0 {
		if err := setSchedule(form, spec.Schedule); err != nil {
			return "", err
		}
	}
	return g.created(ctx, meta.Request{Method: http.MethodPost, Path: path + "/adsets", Token: token, Form: form}, "ad set")
}

func (g *Gateway) CreateAd(ctx context.Context, token, metaAccountID string, spec advertising.AdSpec) (string, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return "", err
	}
	creative, err := jsonValue(map[string]string{"creative_id": spec.CreativeID})
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("name", spec.Name)
	form.Set("adset_id", spec.AdSetID)
	form.Set("creative", creative)
	form.Set("status", string(spec.Status))
	if spec.AssetGroups != nil {
		groups, err := assetGroupsOf(*spec.AssetGroups)
		if err != nil {
			return "", err
		}
		encoded, err := jsonValue(groups)
		if err != nil {
			return "", err
		}
		form.Set("creative_asset_groups_spec", encoded)
	}
	return g.created(ctx, meta.Request{Method: http.MethodPost, Path: path + "/ads", Token: token, Form: form}, "ad")
}

func (g *Gateway) SetStatus(ctx context.Context, token, metaID string, status advertising.ConfiguredStatus) error {
	path, err := objectPath(metaID)
	if err != nil {
		return err
	}
	if status == "" {
		return fmt.Errorf("marketing: status is required")
	}
	form := url.Values{}
	form.Set("status", string(status))
	return g.acknowledged(ctx, meta.Request{Method: http.MethodPost, Path: path, Token: token, Form: form, Idempotent: true})
}

func (g *Gateway) DeleteObject(ctx context.Context, token, metaID string) error {
	path, err := objectPath(metaID)
	if err != nil {
		return err
	}
	return g.acknowledged(ctx, meta.Request{Method: http.MethodDelete, Path: path, Token: token})
}
