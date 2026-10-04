package marketing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

func (g *Gateway) MetaMediaURLs(ctx context.Context, token, metaAccountID string, refs []advertising.MediaRef) (map[string]string, error) {
	urls := map[string]string{}
	var hashes []string
	for _, ref := range refs {
		metaID, ok := ref.MetaID()
		if !ok {
			continue
		}
		if ref.Kind == advertising.MediaVideo {
			picture, err := g.videoPicture(ctx, token, metaID)
			if err != nil {
				return nil, err
			}
			urls[ref.MediaID] = picture
			continue
		}
		hashes = append(hashes, metaID)
	}
	if len(hashes) == 0 {
		return urls, nil
	}
	images, err := g.imageURLs(ctx, token, metaAccountID, hashes)
	if err != nil {
		return nil, err
	}
	for hash, address := range images {
		urls[advertising.MetaMediaRef(advertising.MediaImage, hash).MediaID] = address
	}
	return urls, nil
}

func (g *Gateway) imageURLs(ctx context.Context, token, metaAccountID string, hashes []string) (map[string]string, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(hashes)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("hashes", string(raw))
	q.Set("fields", "hash,url")
	var out struct {
		Data []struct {
			Hash string `json:"hash"`
			URL  string `json:"url"`
		} `json:"data"`
	}
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: path + "/adimages", Token: token, Query: q}, &out); err != nil {
		return nil, err
	}
	urls := make(map[string]string, len(out.Data))
	for _, image := range out.Data {
		urls[image.Hash] = image.URL
	}
	return urls, nil
}

func (g *Gateway) videoPicture(ctx context.Context, token, videoID string) (string, error) {
	path, err := objectPath(videoID)
	if err != nil {
		return "", err
	}
	var out struct {
		Picture string `json:"picture"`
	}
	if err := g.get(ctx, path, token, "picture", &out); err != nil {
		return "", err
	}
	return out.Picture, nil
}
