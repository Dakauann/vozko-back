package geolocation

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type ipAPIResponse struct {
	Status  string `json:"status"`
	City    string `json:"city"`
	Region  string `json:"regionName"`
	Country string `json:"country"`
}

var httpClient = &http.Client{Timeout: 3 * time.Second}

func LookupIP(ip string) string {
	if ip == "" || isPrivateIP(ip) {
		return ""
	}

	url := fmt.Sprintf("http://ip-api.com/json/%s?fields=status,city,regionName,country", ip)
	resp, err := httpClient.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var data ipAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || data.Status != "success" {
		return ""
	}

	parts := make([]string, 0, 3)
	if data.City != "" {
		parts = append(parts, data.City)
	}
	if data.Region != "" {
		parts = append(parts, data.Region)
	}
	if data.Country != "" {
		parts = append(parts, data.Country)
	}
	return strings.Join(parts, ", ")
}

func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return true
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}
