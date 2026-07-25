package whatsapp_business_phone

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// channelsPageJSON builds a /channels page with `count` channels starting at `start`,
// reporting `total` overall — matching the shape the client decodes.
func channelsPageJSON(start, count, total int) string {
	page := dialog360ChannelPage{Total: total}
	for i := 0; i < count; i++ {
		var dto dialog360ChannelDTO
		dto.ID = fmt.Sprintf("ch%d", start+i)
		dto.SetupInfo.PhoneNumber = fmt.Sprintf("155500%05d", start+i)
		dto.HubStatus = "live"
		page.Channels = append(page.Channels, dto)
	}
	b, _ := json.Marshal(page)
	return string(b)
}

// TestListChannels_PagesThroughEveryChannel is the regression test for the large-fleet
// bug: the old ListChannels fetched only the first page, so the reconcile silently
// missed every channel beyond it. It must page until the fleet is exhausted.
func TestListChannels_PagesThroughEveryChannel(t *testing.T) {
	const total = 450 // pages of 200 -> 200, 200, 50
	var offsetsSeen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		offsetsSeen = append(offsetsSeen, q.Get("offset"))
		off := 0
		fmt.Sscanf(q.Get("offset"), "%d", &off)
		count := 200
		if off+200 > total {
			count = total - off
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(channelsPageJSON(off, count, total)))
	}))
	defer srv.Close()

	c := NewDialog360PartnerClient(srv.URL, "P1", "key", "sol", srv.Client())
	chs, err := c.ListChannels()
	if err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	if len(chs) != total {
		t.Fatalf("want all %d channels across pages, got %d (offsets: %v)", total, len(chs), offsetsSeen)
	}
	if chs[0].ID != "ch0" || chs[total-1].ID != fmt.Sprintf("ch%d", total-1) {
		t.Fatalf("boundary channels missing: first=%s last=%s", chs[0].ID, chs[total-1].ID)
	}
	if len(offsetsSeen) != 3 || offsetsSeen[0] != "0" || offsetsSeen[1] != "200" || offsetsSeen[2] != "400" {
		t.Fatalf("expected exactly 3 pages at offsets 0/200/400, got %v", offsetsSeen)
	}
}

// A partial first page must stop after one request (no needless extra call).
func TestListChannels_SinglePartialPage(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(channelsPageJSON(0, 3, 3)))
	}))
	defer srv.Close()
	c := NewDialog360PartnerClient(srv.URL, "P1", "key", "sol", srv.Client())
	chs, err := c.ListChannels()
	if err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	if len(chs) != 3 || calls != 1 {
		t.Fatalf("a partial page must return 3 in 1 call, got %d channels in %d calls", len(chs), calls)
	}
}

// GetChannel must fetch a single channel via the id filter — one call, no paging.
func TestGetChannel_SingleCallViaFilter(t *testing.T) {
	var sawFilter bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "filters=") {
			sawFilter = true
		}
		_, _ = w.Write([]byte(`{"partner_channels":[{"id":"chX","setup_info":{"phone_number":"15550009","phone_name":"Acme"},"current_limit":"TIER_1K","hub_status":"live"}],"total":1}`))
	}))
	defer srv.Close()
	c := NewDialog360PartnerClient(srv.URL, "P1", "key", "sol", srv.Client())

	ch, err := c.GetChannel("chX")
	if err != nil {
		t.Fatalf("GetChannel: %v", err)
	}
	if !sawFilter {
		t.Fatal("GetChannel must query with a filter, not fetch the whole listing")
	}
	if ch == nil || ch.ID != "chX" || ch.PhoneNumber != "15550009" || ch.MessagingTier != "TIER_1K" {
		t.Fatalf("GetChannel decoded wrong: %+v", ch)
	}
}

func TestGetChannel_NotFoundReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"partner_channels":[],"total":0}`))
	}))
	defer srv.Close()
	c := NewDialog360PartnerClient(srv.URL, "P1", "key", "sol", srv.Client())
	ch, err := c.GetChannel("missing")
	if err != nil || ch != nil {
		t.Fatalf("absent channel must be (nil, nil), got ch=%+v err=%v", ch, err)
	}
}
