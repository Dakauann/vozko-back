package cmcom_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/infra/cmcom"
	"vozko/infra/cmcom/cmfake"
)

func brandSender() rcs.Sender {
	return rcs.Sender{
		BrandName:       "Padaria Sol",
		Description:     "Atualizações de pedidos da Padaria Sol",
		LogoURL:         "https://cdn.example.com/logo.png",
		BannerURL:       "https://cdn.example.com/banner.png",
		Color:           "#FF8800",
		UseCase:         rcs.UseCasePromotional,
		Sector:          rcs.SectorRetail,
		ContactName:     "Maria Souza",
		ContactEmail:    "maria@padariasol.com.br",
		ContactPhone:    "+5511987654321",
		WebsiteURL:      "https://padariasol.com.br",
		PrivacyURL:      "https://padariasol.com.br/privacidade",
		TermsURL:        "https://padariasol.com.br/termos",
		CNPJ:            "12345678000195",
		BillingCategory: rcs.BillingConversational,
	}
}

func channelsRequests(fake *cmfake.Fake) []cmfake.Request {
	var out []cmfake.Request
	for _, r := range fake.Requests() {
		if strings.HasPrefix(r.Path, "/channels/") {
			out = append(out, r)
		}
	}
	return out
}

func TestSubmitSenderRunsTheChannelsFlow(t *testing.T) {
	client, fake := newClient(t)
	sub, err := client.SubmitSender(context.Background(), brandSender())
	if err != nil {
		t.Fatalf("SubmitSender: %v", err)
	}
	if sub.RequestID == "" || sub.ProfileID == "" {
		t.Fatalf("submission = %+v", sub)
	}
	want := map[string]string{
		"brandName":                 "Padaria Sol",
		"description":               "Atualizações de pedidos da Padaria Sol",
		"logoImage":                 "https://cdn.example.com/logo.png",
		"heroImage":                 "https://cdn.example.com/banner.png",
		"brandColor":                "#FF8800",
		"website":                   "https://padariasol.com.br",
		"privacyPolicyUrl":          "https://padariasol.com.br/privacidade",
		"termsOfServiceUrl":         "https://padariasol.com.br/termos",
		"contactName":               "Maria Souza",
		"contactEmail":              "maria@padariasol.com.br",
		"contactPhone":              "+5511987654321",
		"billingCategory":           "CONVERSATIONAL",
		"useCase":                   "PROMOTIONAL",
		"countries":                 "BR",
		"companyRegistrationNumber": "12345678000195",
	}
	if got := fake.FormInputsByKey(sub.ProfileID); !reflect.DeepEqual(got, want) {
		t.Fatalf("form inputs\n got %v\nwant %v", got, want)
	}

	account := "/channels/v1/accounts/" + fake.AccountID
	steps := []struct{ method, path string }{
		{http.MethodGet, account + "/channels/types"},
		{http.MethodGet, account + "/channels/types/" + cmfake.RCSChannelTypeID + "/categories"},
		{http.MethodPost, account + "/channels/requests"},
		{http.MethodPost, account + "/rcs/" + sub.RequestID + "/profiles"},
		{http.MethodGet, account + "/channels/types/" + cmfake.RCSChannelTypeID + "/requestform"},
		{http.MethodPut, account + "/rcs/" + sub.RequestID + "/profiles/" + sub.ProfileID + "/forminput"},
		{http.MethodGet, account + "/channels/types/" + cmfake.RCSChannelTypeID + "/requestform"},
		{http.MethodPut, account + "/rcs/" + sub.RequestID + "/profiles/" + sub.ProfileID + "/forminput"},
		{http.MethodGet, account + "/channels/types/" + cmfake.RCSChannelTypeID + "/requestform"},
	}
	reqs := channelsRequests(fake)
	if len(reqs) != len(steps) {
		for _, r := range reqs {
			t.Logf("%s %s?%s", r.Method, r.Path, r.Query.Encode())
		}
		t.Fatalf("got %d channels calls, want %d", len(reqs), len(steps))
	}
	for i, step := range steps {
		if reqs[i].Method != step.method || reqs[i].Path != step.path {
			t.Fatalf("call %d = %s %s, want %s %s", i, reqs[i].Method, reqs[i].Path, step.method, step.path)
		}
		if reqs[i].ProductToken != fake.ProductToken {
			t.Fatalf("call %d without the product token", i)
		}
	}
	if got := reqs[2].Query.Get("channelTypeId"); got != cmfake.RCSChannelTypeID {
		t.Fatalf("channel request type = %q", got)
	}
	first := reqs[4].Query
	if first.Get("channelTypeCategoryId") != cmfake.ProfileCategoryID || first.Get("channelCategoryRequestId") != sub.ProfileID || first.Get("omitTitles") != "true" {
		t.Fatalf("request form query = %v", first)
	}
	var body []map[string]any
	if err := json.Unmarshal(reqs[5].Body, &body); err != nil {
		t.Fatalf("form input body must be a JSON array: %v\n%s", err, reqs[5].Body)
	}
	for _, item := range body {
		if len(item) != 2 || item["formElementId"] == nil || item["inputValue"] == nil {
			t.Fatalf("form input item = %v", item)
		}
	}
	var second []map[string]any
	if err := json.Unmarshal(reqs[7].Body, &second); err != nil || len(second) != 1 {
		t.Fatalf("the second save must carry only the field the form revealed: %s", reqs[7].Body)
	}
}

func TestSubmitSenderReusesKnownProviderIDs(t *testing.T) {
	client, fake := newClient(t)
	fake.SeedProfile("8f1d0c4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f", "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d")
	s := brandSender()
	s.ProviderRequestID = "8f1d0c4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
	s.ProviderProfileID = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	sub, err := client.SubmitSender(context.Background(), s)
	if err != nil {
		t.Fatalf("SubmitSender: %v", err)
	}
	if sub.RequestID != s.ProviderRequestID || sub.ProfileID != s.ProviderProfileID {
		t.Fatalf("submission = %+v", sub)
	}
	for _, r := range channelsRequests(fake) {
		if r.Method == http.MethodPost {
			t.Fatalf("a known request and profile must not be created again: %s %s", r.Method, r.Path)
		}
	}
	if len(fake.FormInputsByKey(s.ProviderProfileID)) == 0 {
		t.Fatalf("form input was not submitted")
	}
}

func TestSubmitSenderCreatesAProfileUnderAKnownRequest(t *testing.T) {
	client, fake := newClient(t)
	fake.SeedRequest("8f1d0c4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f")
	s := brandSender()
	s.ProviderRequestID = "8f1d0c4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
	sub, err := client.SubmitSender(context.Background(), s)
	if err != nil {
		t.Fatalf("SubmitSender: %v", err)
	}
	if sub.RequestID != s.ProviderRequestID || sub.ProfileID == "" {
		t.Fatalf("submission = %+v", sub)
	}
	for _, r := range channelsRequests(fake) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.Path, "/channels/requests") {
			t.Fatalf("a known channel request must not be created again")
		}
	}
}

func TestSubmitSenderFailsClosed(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(*cmfake.Fake)
		sender  func() rcs.Sender
		wantErr error
		names   string
	}{
		{
			name: "required element we cannot fill",
			setup: func(f *cmfake.Fake) {
				f.SetForm(append(cmfake.DefaultForm(), cmfake.FormElement{ID: "7d1e2f3a-4b5c-4d6e-8f70-819203a4b5c6", InputKey: "googleVerificationToken", Required: true}))
			},
			wantErr: cmcom.ErrFormUnmapped,
			names:   "googleVerificationToken",
		},
		{
			name:    "required element left empty",
			sender:  func() rcs.Sender { s := brandSender(); s.ContactName = ""; return s },
			wantErr: cmcom.ErrFormUnmapped,
			names:   "contactName",
		},
		{
			name:    "form never completes",
			setup:   func(f *cmfake.Fake) { f.ForceFormState("INCOMPLETE") },
			wantErr: cmcom.ErrFormIncomplete,
		},
		{
			name:    "unknown form state",
			setup:   func(f *cmfake.Fake) { f.ForceFormState("ALMOST") },
			wantErr: cmcom.ErrProviderResponse,
		},
		{
			name: "no rcs channel type",
			setup: func(f *cmfake.Fake) {
				f.SetChannelTypes([]cmfake.ChannelType{{ID: "2b3c4d5e-6f70-4182-93a4-b5c6d7e8f901", PrettyName: "WhatsApp"}})
			},
			wantErr: cmcom.ErrProviderResponse,
		},
		{
			name: "two rcs channel types",
			setup: func(f *cmfake.Fake) {
				f.SetChannelTypes([]cmfake.ChannelType{
					{ID: cmfake.RCSChannelTypeID, PrettyName: "RCS"},
					{ID: "2b3c4d5e-6f70-4182-93a4-b5c6d7e8f901", PrettyName: "rcs"},
				})
			},
			wantErr: cmcom.ErrProviderResponse,
		},
		{
			name: "no profile category",
			setup: func(f *cmfake.Fake) {
				f.SetCategories([]cmfake.Category{{ID: "3c4d5e6f-7081-4293-a4b5-c6d7e8f90a1b", Name: "Account"}})
			},
			wantErr: cmcom.ErrProviderResponse,
		},
		{
			name:    "channels api down",
			setup:   func(f *cmfake.Fake) { f.RespondChannelsRaw(http.StatusServiceUnavailable, "") },
			wantErr: carriermessaging.ErrProviderUnavailable,
		},
		{
			name:    "channels api refuses",
			setup:   func(f *cmfake.Fake) { f.RespondChannelsRaw(http.StatusForbidden, `{"title":"Forbidden","status":403}`) },
			wantErr: carriermessaging.ErrProviderRefused,
		},
		{
			name: "profile id without request id",
			sender: func() rcs.Sender {
				s := brandSender()
				s.ProviderProfileID = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
				return s
			},
			wantErr: cmcom.ErrSenderIDs,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, fake := newClient(t)
			if tc.setup != nil {
				tc.setup(fake)
			}
			s := brandSender()
			if tc.sender != nil {
				s = tc.sender()
			}
			sub, err := client.SubmitSender(context.Background(), s)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("SubmitSender = %+v, %v; want %v", sub, err, tc.wantErr)
			}
			if tc.names != "" && !strings.Contains(err.Error(), tc.names) {
				t.Fatalf("error %q must name %q", err, tc.names)
			}
			if tc.wantErr == cmcom.ErrFormUnmapped {
				for _, r := range channelsRequests(fake) {
					if r.Method == http.MethodPut {
						t.Fatalf("nothing may be saved when a required field cannot be filled")
					}
				}
			}
		})
	}
}

func TestSenderStatusMapping(t *testing.T) {
	cases := []struct {
		status cmfake.ProfileStatus
		want   carriermessaging.SenderStatusReport
	}{
		{cmfake.ProfileStatus{EntityStatus: "ACTIVE"}, carriermessaging.SenderStatusReport{State: rcs.ProviderLaunched}},
		{cmfake.ProfileStatus{EntityStatus: "PENDING"}, carriermessaging.SenderStatusReport{State: rcs.ProviderPending}},
		{cmfake.ProfileStatus{EntityStatus: "ELIGIBLE"}, carriermessaging.SenderStatusReport{State: rcs.ProviderPending}},
		{cmfake.ProfileStatus{EntityStatus: "NOT_VALIDATED"}, carriermessaging.SenderStatusReport{State: rcs.ProviderAwaitingBrand}},
		{cmfake.ProfileStatus{EntityStatus: "CUSTOMER_ACTION_REQUIRED"}, carriermessaging.SenderStatusReport{State: rcs.ProviderAwaitingBrand}},
		{cmfake.ProfileStatus{EntityStatus: "REJECTED", RejectionReason: "Logo is blurry"}, carriermessaging.SenderStatusReport{State: rcs.ProviderRejected, Reason: "Logo is blurry"}},
		{cmfake.ProfileStatus{EntityStatus: "EXPIRED"}, carriermessaging.SenderStatusReport{State: rcs.ProviderRejected}},
		{cmfake.ProfileStatus{EntityStatus: "CANCELLED", RejectionReason: "Cancelled by customer"}, carriermessaging.SenderStatusReport{State: rcs.ProviderRejected, Reason: "Cancelled by customer"}},
		{cmfake.ProfileStatus{EntityStatus: "DELETED"}, carriermessaging.SenderStatusReport{State: rcs.ProviderRejected}},
	}
	for _, tc := range cases {
		t.Run(tc.status.EntityStatus, func(t *testing.T) {
			client, fake := newClient(t)
			s := seededSender(fake, tc.status)
			got, err := client.SenderStatus(context.Background(), s)
			if err != nil || got != tc.want {
				t.Fatalf("SenderStatus = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

func seededSender(fake *cmfake.Fake, statuses ...cmfake.ProfileStatus) rcs.Sender {
	s := brandSender()
	s.ProviderRequestID = "8f1d0c4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
	s.ProviderProfileID = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	fake.SeedProfile(s.ProviderRequestID, s.ProviderProfileID, statuses...)
	return s
}

func TestSenderStatusRefusals(t *testing.T) {
	cases := []struct {
		name    string
		status  cmfake.ProfileStatus
		sender  func(rcs.Sender) rcs.Sender
		wantErr error
	}{
		{name: "unknown entity status", status: cmfake.ProfileStatus{EntityStatus: "SUSPENDED_BY_GOOGLE"}, wantErr: cmcom.ErrProviderResponse},
		{name: "empty entity status", status: cmfake.ProfileStatus{EntityStatus: ""}, wantErr: cmcom.ErrProviderResponse},
		{name: "no profile id", status: cmfake.ProfileStatus{EntityStatus: "ACTIVE"}, sender: func(s rcs.Sender) rcs.Sender { s.ProviderProfileID = ""; return s }, wantErr: cmcom.ErrSenderIDs},
		{name: "no request id", status: cmfake.ProfileStatus{EntityStatus: "ACTIVE"}, sender: func(s rcs.Sender) rcs.Sender { s.ProviderRequestID = ""; return s }, wantErr: cmcom.ErrSenderIDs},
		{name: "profile unknown to the provider", status: cmfake.ProfileStatus{EntityStatus: "ACTIVE"}, sender: func(s rcs.Sender) rcs.Sender {
			s.ProviderProfileID = "99999999-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
			return s
		}, wantErr: carriermessaging.ErrProviderRefused},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, fake := newClient(t)
			s := seededSender(fake, tc.status)
			if tc.sender != nil {
				s = tc.sender(s)
			}
			got, err := client.SenderStatus(context.Background(), s)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("SenderStatus = %+v, %v; want %v", got, err, tc.wantErr)
			}
		})
	}
}

func TestSenderStatusFollowsAProgression(t *testing.T) {
	client, fake := newClient(t)
	s := seededSender(fake)
	fake.SetProfileStatuses(s.ProviderProfileID,
		cmfake.ProfileStatus{EntityStatus: "PENDING"},
		cmfake.ProfileStatus{EntityStatus: "NOT_VALIDATED"},
		cmfake.ProfileStatus{EntityStatus: "ACTIVE"},
	)
	want := []rcs.ProviderState{rcs.ProviderPending, rcs.ProviderAwaitingBrand, rcs.ProviderLaunched, rcs.ProviderLaunched}
	for i, w := range want {
		got, err := client.SenderStatus(context.Background(), s)
		if err != nil || got.State != w {
			t.Fatalf("poll %d = %+v, %v; want %s", i, got, err, w)
		}
	}
}

func TestSubmittedProfileStartsPending(t *testing.T) {
	client, _ := newClient(t)
	s := brandSender()
	sub, err := client.SubmitSender(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	s.ProviderRequestID, s.ProviderProfileID = sub.RequestID, sub.ProfileID
	got, err := client.SenderStatus(context.Background(), s)
	if err != nil || got.State != rcs.ProviderPending {
		t.Fatalf("SenderStatus = %+v, %v", got, err)
	}
}
