package marketing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

const (
	locationTypes    = `["country","region","city"]`
	searchLocale     = "pt_BR"
	pageTokenTTL     = 10 * time.Minute
	recentPostsLimit = "100"
	platformFacebook = "facebook"
	platformIG       = "instagram"
)

var _ advertising.AssetGateway = (*Gateway)(nil)

type graphLocation struct {
	Key         meta.GraphID `json:"key"`
	Name        string       `json:"name"`
	Type        string       `json:"type"`
	CountryCode string       `json:"country_code"`
	Region      string       `json:"region"`
}

func (g *Gateway) SearchLocations(ctx context.Context, token, query string) ([]advertising.RemoteLocation, error) {
	q := url.Values{}
	q.Set("type", "adgeolocation")
	q.Set("location_types", locationTypes)
	q.Set("q", query)
	q.Set("limit", "20")
	var out graphPage[graphLocation]
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: "/search", Token: token, Query: q}, &out); err != nil {
		return nil, err
	}
	locations := make([]advertising.RemoteLocation, 0, len(out.Data))
	for _, row := range out.Data {
		kind := advertising.LocationKind(row.Type)
		if kind != advertising.LocationCountry && kind != advertising.LocationRegion && kind != advertising.LocationCity {
			continue
		}
		if row.Key == "" {
			return nil, fmt.Errorf("marketing: location %q without key", row.Name)
		}
		locations = append(locations, advertising.RemoteLocation{
			Kind:    kind,
			Key:     row.Key.String(),
			Name:    row.Name,
			Region:  row.Region,
			Country: row.CountryCode,
		})
	}
	return locations, nil
}

type graphOption struct {
	ID    meta.GraphID `json:"id"`
	Key   meta.GraphID `json:"key"`
	Name  string       `json:"name"`
	Path  []string     `json:"path"`
	Lower graphNumber  `json:"audience_size_lower_bound"`
	Upper graphNumber  `json:"audience_size_upper_bound"`
}

func (o graphOption) toDomain(kind advertising.TargetingSearchKind) (advertising.TargetingOption, error) {
	id := o.ID
	if kind == advertising.SearchLanguages {
		id = o.Key
	}
	if id == "" {
		return advertising.TargetingOption{}, fmt.Errorf("marketing: %s option %q without id", kind, o.Name)
	}
	lower, err := o.Lower.wholePart("audience_size_lower_bound")
	if err != nil {
		return advertising.TargetingOption{}, err
	}
	upper, err := o.Upper.wholePart("audience_size_upper_bound")
	if err != nil {
		return advertising.TargetingOption{}, err
	}
	return advertising.TargetingOption{ID: id.String(), Name: o.Name, Path: o.Path, AudienceMin: lower, AudienceMax: upper}, nil
}

func (g *Gateway) SearchTargeting(ctx context.Context, token, metaAccountID string, kind advertising.TargetingSearchKind, query string) ([]advertising.TargetingOption, error) {
	q := url.Values{}
	q.Set("locale", searchLocale)
	var rows []graphOption
	switch kind {
	case advertising.SearchInterests, advertising.SearchLanguages:
		q.Set("type", "adinterest")
		if kind == advertising.SearchLanguages {
			q.Set("type", "adlocale")
		}
		q.Set("q", query)
		q.Set("limit", "25")
		var page graphPage[graphOption]
		if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: "/search", Token: token, Query: q}, &page); err != nil {
			return nil, err
		}
		rows = page.Data
	case advertising.SearchBehaviors:
		q.Set("type", "adTargetingCategory")
		q.Set("class", "behaviors")
		all, err := collect[graphOption](ctx, g, "/search", token, q)
		if err != nil {
			return nil, err
		}
		needle := strings.ToLower(strings.TrimSpace(query))
		for _, row := range all {
			if strings.Contains(strings.ToLower(row.Name), needle) {
				rows = append(rows, row)
			}
		}
	default:
		return nil, fmt.Errorf("marketing: unknown targeting search %q", kind)
	}
	options := make([]advertising.TargetingOption, 0, len(rows))
	for _, row := range rows {
		option, err := row.toDomain(kind)
		if err != nil {
			return nil, err
		}
		options = append(options, option)
	}
	return options, nil
}

func (g *Gateway) EstimateReach(ctx context.Context, token, metaAccountID string, t advertising.Targeting, p advertising.Placements, goal advertising.OptimizationGoal) (*advertising.ReachEstimate, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return nil, err
	}
	spec, err := targetingParam(t, p)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("targeting_spec", spec)
	if goal != "" {
		q.Set("optimization_goal", string(goal))
	}
	var out struct {
		Data *struct {
			Lower *int64 `json:"users_lower_bound"`
			Upper *int64 `json:"users_upper_bound"`
			Ready *bool  `json:"estimate_ready"`
		} `json:"data"`
	}
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: path + "/reachestimate", Token: token, Query: q}, &out); err != nil {
		return nil, err
	}
	if out.Data == nil || out.Data.Lower == nil || out.Data.Upper == nil || out.Data.Ready == nil {
		return nil, fmt.Errorf("marketing: reach estimate without bounds")
	}
	if *out.Data.Lower < 0 || *out.Data.Upper < 0 || !*out.Data.Ready {
		return &advertising.ReachEstimate{}, nil
	}
	return &advertising.ReachEstimate{Lower: *out.Data.Lower, Upper: *out.Data.Upper, Ready: true}, nil
}

type graphCatalog struct {
	ID          meta.GraphID `json:"id"`
	Name        string       `json:"name"`
	ProductSets graphList[struct {
		ID           meta.GraphID `json:"id"`
		Name         string       `json:"name"`
		ProductCount graphNumber  `json:"product_count"`
	}] `json:"product_sets"`
}

func (c graphCatalog) toDomain() (advertising.RemoteCatalog, error) {
	if c.ID == "" {
		return advertising.RemoteCatalog{}, fmt.Errorf("marketing: catalog %q without id", c.Name)
	}
	out := advertising.RemoteCatalog{ID: c.ID.String(), Name: c.Name}
	for _, set := range c.ProductSets {
		if set.ID == "" {
			return advertising.RemoteCatalog{}, fmt.Errorf("marketing: product set %q without id", set.Name)
		}
		count, err := set.ProductCount.wholePart("product_count")
		if err != nil {
			return advertising.RemoteCatalog{}, err
		}
		out.ProductSets = append(out.ProductSets, advertising.RemoteProductSet{ID: set.ID.String(), Name: set.Name, ProductCount: count})
	}
	return out, nil
}

func (g *Gateway) ListCatalogs(ctx context.Context, token, businessID string) ([]advertising.RemoteCatalog, error) {
	path, err := objectPath(businessID)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("fields", "id,name,product_sets.limit(100){id,name,product_count}")
	q.Set("limit", "100")
	var catalogs []advertising.RemoteCatalog
	seen := map[string]bool{}
	for _, edge := range []string{"/owned_product_catalogs", "/client_product_catalogs"} {
		rows, err := collect[graphCatalog](ctx, g, path+edge, token, q)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			catalog, err := row.toDomain()
			if err != nil {
				return nil, err
			}
			if seen[catalog.ID] {
				continue
			}
			seen[catalog.ID] = true
			catalogs = append(catalogs, catalog)
		}
	}
	return catalogs, nil
}

type graphApp struct {
	ID              meta.GraphID   `json:"id"`
	Name            string         `json:"name"`
	IconURL         string         `json:"icon_url"`
	ObjectStoreURLs map[string]any `json:"object_store_urls"`
}

func (g *Gateway) ListApps(ctx context.Context, token, metaAccountID string) ([]advertising.RemoteApp, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("fields", "id,name,icon_url,object_store_urls")
	q.Set("limit", "100")
	rows, err := collect[graphApp](ctx, g, path+"/advertisable_applications", token, q)
	if err != nil {
		return nil, err
	}
	apps := make([]advertising.RemoteApp, 0, len(rows))
	for _, row := range rows {
		if row.ID == "" {
			return nil, fmt.Errorf("marketing: app %q without id", row.Name)
		}
		app := advertising.RemoteApp{ID: row.ID.String(), Name: row.Name, IconURL: row.IconURL}
		for _, v := range row.ObjectStoreURLs {
			if link, ok := v.(string); ok && link != "" {
				app.StoreURLs = append(app.StoreURLs, link)
			}
		}
		sort.Strings(app.StoreURLs)
		apps = append(apps, app)
	}
	return apps, nil
}

func pageTokenKey(token, pageID string) string {
	sum := sha256.Sum256([]byte(token + "|" + pageID))
	return hex.EncodeToString(sum[:])
}

func (g *Gateway) pageAccessToken(ctx context.Context, token, pageID string) (string, error) {
	path, err := objectPath(pageID)
	if err != nil {
		return "", err
	}
	key := pageTokenKey(token, pageID)
	g.mu.Lock()
	cached, ok := g.pageTokens[key]
	g.mu.Unlock()
	if ok && g.now().Before(cached.expires) {
		return cached.value, nil
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := g.get(ctx, path, token, "access_token", &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("marketing: page %s returned no access token", pageID)
	}
	g.mu.Lock()
	g.pageTokens[key] = pageToken{value: out.AccessToken, expires: g.now().Add(pageTokenTTL)}
	g.mu.Unlock()
	return out.AccessToken, nil
}

type graphPost struct {
	ID          meta.GraphID `json:"id"`
	Message     string       `json:"message"`
	CreatedTime string       `json:"created_time"`
	FullPicture string       `json:"full_picture"`
	Permalink   string       `json:"permalink_url"`
	Eligible    *bool        `json:"is_eligible_for_promotion"`
}

func (g *Gateway) ListPagePosts(ctx context.Context, token, pageID string) ([]advertising.RemotePost, error) {
	path, pageToken, err := g.pageToken(ctx, token, pageID)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("fields", pagePostFields+",is_eligible_for_promotion")
	q.Set("limit", recentPostsLimit)
	var page graphPage[graphPost]
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: path + "/published_posts", Token: pageToken, Query: q}, &page); err != nil {
		return nil, err
	}
	posts := make([]advertising.RemotePost, 0, len(page.Data))
	for _, row := range page.Data {
		if row.Eligible == nil || !*row.Eligible {
			continue
		}
		post, err := remotePost(row.ID, platformFacebook, row.Message, row.FullPicture, row.Permalink, row.CreatedTime)
		if err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}
	return posts, nil
}

const (
	pagePostFields       = "id,message,created_time,full_picture,permalink_url"
	instagramMediaFields = "id,caption,media_type,media_url,thumbnail_url,permalink,timestamp"
)

func (g *Gateway) GetPagePost(ctx context.Context, token, pageID, postID string) (advertising.RemotePost, error) {
	_, pageToken, err := g.pageToken(ctx, token, pageID)
	if err != nil {
		return advertising.RemotePost{}, err
	}
	path, err := objectPath(postID)
	if err != nil {
		return advertising.RemotePost{}, err
	}
	q := url.Values{}
	q.Set("fields", pagePostFields)
	var row graphPost
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: path, Token: pageToken, Query: q}, &row); err != nil {
		return advertising.RemotePost{}, err
	}
	return remotePost(row.ID, platformFacebook, row.Message, row.FullPicture, row.Permalink, row.CreatedTime)
}

func (g *Gateway) GetInstagramMedia(ctx context.Context, token, mediaID string) (advertising.RemotePost, error) {
	path, err := objectPath(mediaID)
	if err != nil {
		return advertising.RemotePost{}, err
	}
	q := url.Values{}
	q.Set("fields", instagramMediaFields+",owner{id}")
	var row struct {
		graphMedia
		Owner *struct {
			ID meta.GraphID `json:"id"`
		} `json:"owner"`
	}
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: path, Token: token, Query: q}, &row); err != nil {
		return advertising.RemotePost{}, err
	}
	post, err := row.graphMedia.post()
	if err != nil {
		return advertising.RemotePost{}, err
	}
	if row.Owner != nil {
		post.OwnerID = row.Owner.ID.String()
	}
	return post, nil
}

func remotePost(id meta.GraphID, platform, message, picture, permalink, created string) (advertising.RemotePost, error) {
	if id == "" {
		return advertising.RemotePost{}, fmt.Errorf("marketing: %s post without id", platform)
	}
	createdAt, err := graphTime("created_time", created)
	if err != nil {
		return advertising.RemotePost{}, err
	}
	return advertising.RemotePost{ID: id.String(), Platform: platform, Message: message, PictureURL: picture, Permalink: permalink, CreatedTime: createdAt}, nil
}

type graphMedia struct {
	ID           meta.GraphID `json:"id"`
	Caption      string       `json:"caption"`
	MediaType    string       `json:"media_type"`
	MediaURL     string       `json:"media_url"`
	ThumbnailURL string       `json:"thumbnail_url"`
	Permalink    string       `json:"permalink"`
	Timestamp    string       `json:"timestamp"`
	Boost        *struct {
		Eligible *bool `json:"eligible_to_boost"`
	} `json:"boost_eligibility_info"`
}

func (m graphMedia) post() (advertising.RemotePost, error) {
	picture := m.MediaURL
	if m.ThumbnailURL != "" {
		picture = m.ThumbnailURL
	}
	return remotePost(m.ID, platformIG, m.Caption, picture, m.Permalink, m.Timestamp)
}

func (g *Gateway) ListInstagramMedia(ctx context.Context, token, instagramUserID string) ([]advertising.RemotePost, error) {
	path, err := objectPath(instagramUserID)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("fields", instagramMediaFields+",boost_eligibility_info")
	q.Set("limit", recentPostsLimit)
	var page graphPage[graphMedia]
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: path + "/media", Token: token, Query: q}, &page); err != nil {
		return nil, err
	}
	posts := make([]advertising.RemotePost, 0, len(page.Data))
	for _, row := range page.Data {
		if row.Boost == nil || row.Boost.Eligible == nil || !*row.Boost.Eligible {
			continue
		}
		post, err := row.post()
		if err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}
	return posts, nil
}

func (g *Gateway) ListInstantExperiences(ctx context.Context, token, pageID string) ([]advertising.RemoteInstantExperience, error) {
	path, pageToken, err := g.pageToken(ctx, token, pageID)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("fields", "id,name,is_published")
	q.Set("limit", "100")
	rows, err := collect[struct {
		ID        meta.GraphID `json:"id"`
		Name      string       `json:"name"`
		Published *bool        `json:"is_published"`
	}](ctx, g, path+"/canvases", pageToken, q)
	if err != nil {
		return nil, err
	}
	experiences := make([]advertising.RemoteInstantExperience, 0, len(rows))
	for _, row := range rows {
		if row.ID == "" {
			return nil, fmt.Errorf("marketing: instant experience %q without id", row.Name)
		}
		if row.Published == nil || !*row.Published {
			continue
		}
		experiences = append(experiences, advertising.RemoteInstantExperience{ID: row.ID.String(), Name: row.Name})
	}
	return experiences, nil
}
