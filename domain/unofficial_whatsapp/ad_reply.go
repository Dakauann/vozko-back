package unofficial_whatsapp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
)

type AdReply struct {
	Title        string
	Body         string
	SourceID     string
	SourceURL    string
	ThumbnailURL string
	Thumbnail    []byte
}

type externalAdReply struct {
	Title        string `json:"title"`
	Body         string `json:"body"`
	SourceID     string `json:"sourceID"`
	SourceURL    string `json:"sourceURL"`
	ThumbnailURL string `json:"thumbnailURL"`
	Thumbnail    string `json:"thumbnail"`
}

type adContext struct {
	ContextInfo *struct {
		ExternalAdReply *externalAdReply `json:"externalAdReply"`
	} `json:"contextInfo"`
}

func adReplyIn(raw []byte) *AdReply {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	if reply := directAdReply(raw); reply != nil {
		return reply
	}
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(raw, &nested); err != nil {
		return nil
	}
	for _, value := range nested {
		if reply := directAdReply(bytes.TrimSpace(value)); reply != nil {
			return reply
		}
	}
	return nil
}

func directAdReply(raw []byte) *AdReply {
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	var ctx adContext
	if err := json.Unmarshal(raw, &ctx); err != nil || ctx.ContextInfo == nil || ctx.ContextInfo.ExternalAdReply == nil {
		return nil
	}
	ext := ctx.ContextInfo.ExternalAdReply
	reply := &AdReply{
		Title:        strings.TrimSpace(ext.Title),
		Body:         strings.TrimSpace(ext.Body),
		SourceID:     strings.TrimSpace(ext.SourceID),
		SourceURL:    strings.TrimSpace(ext.SourceURL),
		ThumbnailURL: strings.TrimSpace(ext.ThumbnailURL),
	}
	if decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(ext.Thumbnail)); err == nil {
		reply.Thumbnail = decoded
	}
	if reply.Title == "" && reply.Body == "" && reply.SourceID == "" {
		return nil
	}
	return reply
}
