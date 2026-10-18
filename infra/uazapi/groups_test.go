package uazapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
)

func groupJSON(jid, name string) map[string]any {
	return map[string]any{"JID": jid, "Name": name, "OwnerIsAdmin": true}
}

func TestListGroupsPagesThroughThePaginatedList(t *testing.T) {
	var offsets []float64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/group/list" {
			t.Errorf("request = %s %s, want POST /group/list", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		offset := body["offset"].(float64)
		offsets = append(offsets, offset)
		if body["noParticipants"] != true {
			t.Errorf("noParticipants = %v", body["noParticipants"])
		}
		limit := int(body["limit"].(float64))
		groups := []map[string]any{}
		remaining := 3 - int(offset)
		for i := 0; i < limit && i < remaining; i++ {
			groups = append(groups, groupJSON(fmt.Sprintf("1203630%d@g.us", int(offset)+i), "G"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"groups": groups})
	}))
	defer server.Close()

	c := NewClient(Config{})
	c.groupPageSize = 2
	groups, err := c.ListGroups(context.Background(), uw.InstanceRef{BaseURL: server.URL, Token: "t"}, false)
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(groups) != 3 || len(offsets) != 2 || offsets[1] != 2 {
		t.Fatalf("groups = %d offsets = %v, want 3 groups over two pages", len(groups), offsets)
	}
}

func TestCreateGroupSendsNameAndParticipants(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/group/create" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{"group": groupJSON("120363000000000001@g.us", "Ofertas VIP #1"), "failed": []string{}})
	}))
	defer server.Close()

	g, err := NewClient(Config{}).CreateGroup(context.Background(), uw.InstanceRef{BaseURL: server.URL, Token: "t"},
		"Ofertas VIP #1", []string{"5511970001202"})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if body["name"] != "Ofertas VIP #1" || len(body["participants"].([]any)) != 1 {
		t.Fatalf("body = %v", body)
	}
	if g.JID != "120363000000000001@g.us" || !g.WeAreAdmin {
		t.Fatalf("group = %+v", g)
	}
}

func TestCreateGroupRefusesAnInvalidName(t *testing.T) {
	_, err := NewClient(Config{}).CreateGroup(context.Background(), uw.InstanceRef{BaseURL: "http://unused", Token: "t"}, "", nil)
	if err != uw.ErrGroupNameRequired {
		t.Fatalf("CreateGroup with no name = %v", err)
	}
}

func TestJoinAndInviteInfoSendTheInviteLink(t *testing.T) {
	for _, tc := range []struct {
		path string
		call func(c *Client, ref uw.InstanceRef) (*uw.Group, error)
	}{
		{"/group/join", func(c *Client, ref uw.InstanceRef) (*uw.Group, error) {
			return c.JoinGroup(context.Background(), ref, "https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234")
		}},
		{"/group/inviteInfo", func(c *Client, ref uw.InstanceRef) (*uw.Group, error) {
			return c.InviteInfo(context.Background(), ref, "https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234")
		}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					t.Errorf("path = %s", r.URL.Path)
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				g := groupJSON("120363000000000001@g.us", "Feira de Trocas")
				g["Participants"] = []map[string]any{{"JID": "5511981124410@s.whatsapp.net"}, {"JID": "5511992307781@s.whatsapp.net"}}
				_ = json.NewEncoder(w).Encode(map[string]any{"response": "ok", "group": g})
			}))
			defer server.Close()

			g, err := tc.call(NewClient(Config{}), uw.InstanceRef{BaseURL: server.URL, Token: "t"})
			if err != nil {
				t.Fatal(err)
			}
			if body["invitecode"] != "https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234" {
				t.Fatalf("body = %v", body)
			}
			if g.JID != "120363000000000001@g.us" || g.ParticipantCount != 2 {
				t.Fatalf("group = %+v", g)
			}
		})
	}
}
