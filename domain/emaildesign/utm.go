package emaildesign

import (
	"net/url"
	"strings"
)

type UTM struct {
	Source   string `json:"source,omitempty"`
	Medium   string `json:"medium,omitempty"`
	Campaign string `json:"campaign,omitempty"`
	Content  string `json:"content,omitempty"`
}

func ApplyUTM(href string, utm UTM) string {
	u, err := url.Parse(href)
	if err != nil || u.Host == "" {
		return href
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return href
	}
	base, fragment, hasFragment := strings.Cut(href, "#")
	path, query, _ := strings.Cut(base, "?")
	present := existingQueryKeys(query)
	params := []struct {
		key   string
		value string
	}{
		{key: "utm_source", value: utm.Source},
		{key: "utm_medium", value: utm.Medium},
		{key: "utm_campaign", value: utm.Campaign},
		{key: "utm_content", value: utm.Content},
	}
	var added []string
	for _, p := range params {
		value := strings.TrimSpace(p.value)
		if value == "" || present[p.key] {
			continue
		}
		added = append(added, p.key+"="+url.QueryEscape(value))
	}
	if len(added) == 0 {
		return href
	}
	extra := strings.Join(added, "&")
	if query != "" {
		extra = query + "&" + extra
	}
	out := path + "?" + extra
	if hasFragment {
		out += "#" + fragment
	}
	return out
}

func existingQueryKeys(query string) map[string]bool {
	present := map[string]bool{}
	if query == "" {
		return present
	}
	for _, pair := range strings.Split(query, "&") {
		key, _, _ := strings.Cut(pair, "=")
		if unescaped, err := url.QueryUnescape(key); err == nil {
			key = unescaped
		}
		present[strings.ToLower(key)] = true
	}
	return present
}
