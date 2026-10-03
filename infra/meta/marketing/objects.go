package marketing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

const (
	campaignFields = "id,name,status,effective_status,objective,special_ad_categories,daily_budget,lifetime_budget,budget_remaining,bid_strategy,start_time,stop_time,created_time,updated_time,issues_info"
	adSetFields    = "id,name,campaign_id,status,effective_status,daily_budget,lifetime_budget,budget_remaining,optimization_goal,destination_type,bid_strategy,start_time,end_time,created_time,updated_time,issues_info"
	adFields       = "id,name,campaign_id,adset_id,status,effective_status,created_time,updated_time,issues_info,ad_review_feedback,creative{id,title,body,image_url,thumbnail_url}"
	liveStatuses   = `["ACTIVE","PAUSED","PENDING_REVIEW","DISAPPROVED","PREAPPROVED","PENDING_BILLING_INFO","CAMPAIGN_PAUSED","ARCHIVED","ADSET_PAUSED","IN_PROCESS","WITH_ISSUES"]`
)

type levelEdge struct {
	edge   string
	fields string
}

var levelEdges = map[advertising.Level]levelEdge{
	advertising.LevelCampaign: {edge: "campaigns", fields: campaignFields},
	advertising.LevelAdSet:    {edge: "adsets", fields: adSetFields},
	advertising.LevelAd:       {edge: "ads", fields: adFields},
}

func edgeFor(level advertising.Level) (levelEdge, error) {
	edge, ok := levelEdges[level]
	if !ok {
		return levelEdge{}, fmt.Errorf("marketing: unknown level %q", level)
	}
	return edge, nil
}

type graphIssue struct {
	ErrorCode    int    `json:"error_code"`
	ErrorSummary string `json:"error_summary"`
	ErrorMessage string `json:"error_message"`
	Level        string `json:"level"`
}

type graphReviewFeedback struct {
	Global map[string]json.RawMessage `json:"global"`
}

type graphCreative struct {
	ID                     meta.GraphID          `json:"id"`
	Title                  string                `json:"title"`
	Body                   string                `json:"body"`
	ImageURL               string                `json:"image_url"`
	ThumbnailURL           string                `json:"thumbnail_url"`
	ObjectStoryID          string                `json:"object_story_id"`
	ObjectID               meta.GraphID          `json:"object_id"`
	InstagramUserID        meta.GraphID          `json:"instagram_user_id"`
	SourceInstagramMediaID meta.GraphID          `json:"source_instagram_media_id"`
	ProductSetID           meta.GraphID          `json:"product_set_id"`
	ObjectStorySpec        *graphObjectStorySpec `json:"object_story_spec"`
	AssetFeedSpec          *graphAssetFeed       `json:"asset_feed_spec"`
	DegreesOfFreedomSpec   *struct {
		CreativeFeaturesSpec map[string]enrollment `json:"creative_features_spec"`
	} `json:"degrees_of_freedom_spec"`
}

type graphObject struct {
	ID                meta.GraphID         `json:"id"`
	Name              string               `json:"name"`
	CampaignID        meta.GraphID         `json:"campaign_id"`
	AdSetID           meta.GraphID         `json:"adset_id"`
	Status            string               `json:"status"`
	EffectiveStatus   string               `json:"effective_status"`
	Objective         string               `json:"objective"`
	SpecialCategories []string             `json:"special_ad_categories"`
	DailyBudget       graphNumber          `json:"daily_budget"`
	LifetimeBudget    graphNumber          `json:"lifetime_budget"`
	BudgetRemaining   graphNumber          `json:"budget_remaining"`
	OptimizationGoal  string               `json:"optimization_goal"`
	DestinationType   string               `json:"destination_type"`
	BidStrategy       string               `json:"bid_strategy"`
	StartTime         string               `json:"start_time"`
	StopTime          string               `json:"stop_time"`
	EndTime           string               `json:"end_time"`
	CreatedTime       string               `json:"created_time"`
	UpdatedTime       string               `json:"updated_time"`
	IssuesInfo        []graphIssue         `json:"issues_info"`
	ReviewFeedback    *graphReviewFeedback `json:"ad_review_feedback"`
	Creative          *graphCreative       `json:"creative"`
	BidAmount         graphNumber          `json:"bid_amount"`
	BidConstraints    *struct {
		ROASAverageFloor graphNumber `json:"roas_average_floor"`
	} `json:"bid_constraints"`
	Targeting      json.RawMessage      `json:"targeting"`
	AdSetSchedule  []graphDayPart       `json:"adset_schedule"`
	PromotedObject *graphPromotedObject `json:"promoted_object"`
	AssetGroups    *graphAssetGroups    `json:"creative_asset_groups_spec"`
	AdSet          *struct {
		DestinationType string `json:"destination_type"`
	} `json:"adset"`
}

func (o graphObject) toDomain(level advertising.Level) (*advertising.Object, error) {
	if o.ID == "" {
		return nil, fmt.Errorf("marketing: %s without id", level)
	}
	out := &advertising.Object{
		MetaID:          o.ID.String(),
		Level:           level,
		Name:            o.Name,
		Status:          advertising.ConfiguredStatus(o.Status),
		EffectiveStatus: advertising.EffectiveStatus(o.EffectiveStatus),
		BidStrategy:     o.BidStrategy,
	}
	switch level {
	case advertising.LevelCampaign:
		out.Objective = o.Objective
		category, err := specialCategoryOf(o.SpecialCategories)
		if err != nil {
			return nil, err
		}
		out.SpecialCategory = category
	case advertising.LevelAdSet:
		out.CampaignMetaID = o.CampaignID.String()
		out.OptimizationGoal = o.OptimizationGoal
		out.DestinationType = o.DestinationType
	case advertising.LevelAd:
		out.CampaignMetaID = o.CampaignID.String()
		out.AdSetMetaID = o.AdSetID.String()
		out.Creative = o.creative()
		out.ReviewFeedback = o.reviewFeedback()
	}
	if err := o.fillBudgets(out); err != nil {
		return nil, err
	}
	if err := o.fillTimes(out, level); err != nil {
		return nil, err
	}
	for _, issue := range o.IssuesInfo {
		out.Issues = append(out.Issues, advertising.Issue{
			Code:    issue.ErrorCode,
			Summary: issue.ErrorSummary,
			Message: issue.ErrorMessage,
			Level:   issue.Level,
		})
	}
	return out, nil
}

func (o graphObject) fillBudgets(out *advertising.Object) error {
	var err error
	if out.DailyBudget, err = o.DailyBudget.minorUnits("daily_budget"); err != nil {
		return err
	}
	if out.LifetimeBudget, err = o.LifetimeBudget.minorUnits("lifetime_budget"); err != nil {
		return err
	}
	out.BudgetRemaining, err = o.BudgetRemaining.minorUnits("budget_remaining")
	return err
}

func (o graphObject) fillTimes(out *advertising.Object, level advertising.Level) error {
	end, endField := o.EndTime, "end_time"
	if level == advertising.LevelCampaign {
		end, endField = o.StopTime, "stop_time"
	}
	var err error
	if out.StartTime, err = graphTime("start_time", o.StartTime); err != nil {
		return err
	}
	if out.EndTime, err = graphTime(endField, end); err != nil {
		return err
	}
	if out.CreatedTime, err = graphTime("created_time", o.CreatedTime); err != nil {
		return err
	}
	out.UpdatedTime, err = graphTime("updated_time", o.UpdatedTime)
	return err
}

func (o graphObject) creative() *advertising.Creative {
	if o.Creative == nil {
		return nil
	}
	return &advertising.Creative{
		ID:           o.Creative.ID.String(),
		Title:        o.Creative.Title,
		Body:         o.Creative.Body,
		ImageURL:     o.Creative.ImageURL,
		ThumbnailURL: o.Creative.ThumbnailURL,
	}
}

func (o graphObject) reviewFeedback() map[string]string {
	if o.ReviewFeedback == nil || len(o.ReviewFeedback.Global) == 0 {
		return nil
	}
	out := make(map[string]string, len(o.ReviewFeedback.Global))
	for key, raw := range o.ReviewFeedback.Global {
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			out[key] = text
			continue
		}
		out[key] = string(raw)
	}
	return out
}

func (g *Gateway) ListObjects(ctx context.Context, token, metaAccountID string, level advertising.Level) ([]*advertising.Object, error) {
	edge, err := edgeFor(level)
	if err != nil {
		return nil, err
	}
	path, err := accountPath(metaAccountID)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("fields", edge.fields)
	q.Set("limit", "200")
	q.Set("effective_status", liveStatuses)
	rows, err := collect[graphObject](ctx, g, path+"/"+edge.edge, token, q)
	if err != nil {
		return nil, err
	}
	objects := make([]*advertising.Object, 0, len(rows))
	for _, row := range rows {
		object, err := row.toDomain(level)
		if err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func (g *Gateway) GetObject(ctx context.Context, token, metaID string, level advertising.Level) (*advertising.Object, error) {
	edge, err := edgeFor(level)
	if err != nil {
		return nil, err
	}
	path, err := objectPath(metaID)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("fields", edge.fields)
	var row graphObject
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: path, Token: token, Query: q}, &row); err != nil {
		return nil, err
	}
	return row.toDomain(level)
}

var knownCategories = []advertising.SpecialCategory{
	advertising.CategoryNone, advertising.CategoryHousing, advertising.CategoryEmployment,
	advertising.CategoryFinancial, advertising.CategoryPolitics, advertising.CategoryGambling,
}

const legacyCreditCategory = "CREDIT"

func specialCategoryOf(categories []string) (advertising.SpecialCategory, error) {
	if len(categories) == 0 {
		return advertising.CategoryNone, nil
	}
	category := advertising.SpecialCategory(categories[0])
	if categories[0] == legacyCreditCategory {
		category = advertising.CategoryFinancial
	}
	if !slices.Contains(knownCategories, category) {
		return "", fmt.Errorf("marketing: unknown special ad category %q", categories[0])
	}
	return category, nil
}
