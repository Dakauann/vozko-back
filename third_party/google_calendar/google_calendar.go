package google_calendar

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/calendar"
)

const (
	tokenURL       = "https://oauth2.googleapis.com/token"
	userInfoURL    = "https://openidconnect.googleapis.com/v1/userinfo"
	authURL        = "https://accounts.google.com/o/oauth2/v2/auth"
	calendarAPIURL = "https://www.googleapis.com/calendar/v3"

	scopeCalendar         = "https://www.googleapis.com/auth/calendar"
	scopeCalendarEvents   = "https://www.googleapis.com/auth/calendar.events"
	scopeCalendarReadonly = "https://www.googleapis.com/auth/calendar.readonly"
	scopeEmail            = "email"
	scopeProfile          = "profile"
)

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

type Service struct {
	clientID     string
	clientSecret string
	httpClient   *http.Client
}

var _ calendar.GoogleOAuthService = (*Service)(nil)

func NewService(clientID, clientSecret string) *Service {
	return &Service{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (s *Service) GetAuthURL(redirectURI, state string) string {
	scopes := strings.Join([]string{
		scopeCalendar,
		scopeCalendarEvents,
		scopeCalendarReadonly,
		scopeEmail,
		scopeProfile,
	}, " ")

	params := url.Values{
		"client_id":     {s.clientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {scopes},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
		"state":         {state},
	}

	return authURL + "?" + params.Encode()
}

func (s *Service) ExchangeCode(code, redirectURI string) (*calendar.OAuthTokenResponse, error) {
	data := url.Values{
		"code":          {code},
		"client_id":     {s.clientID},
		"client_secret": {s.clientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		var errResp struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		json.NewDecoder(res.Body).Decode(&errResp)
		return nil, fmt.Errorf("token exchange failed (%d): %s – %s", res.StatusCode, errResp.Error, errResp.Description)
	}

	var tokenResp tokenResponse
	if err := json.NewDecoder(res.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}

	return &calendar.OAuthTokenResponse{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresIn:    tokenResp.ExpiresIn,
	}, nil
}

func (s *Service) RefreshAccessToken(refreshToken string) (*calendar.OAuthTokenResponse, error) {
	data := url.Values{
		"refresh_token": {refreshToken},
		"client_id":     {s.clientID},
		"client_secret": {s.clientSecret},
		"grant_type":    {"refresh_token"},
	}

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed with status %d", res.StatusCode)
	}

	var tokenResp tokenResponse
	if err := json.NewDecoder(res.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}

	return &calendar.OAuthTokenResponse{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresIn:    tokenResp.ExpiresIn,
	}, nil
}

func (s *Service) GetUserInfo(accessToken string) (*calendar.OAuthUserInfo, error) {
	req, err := http.NewRequest(http.MethodGet, userInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	res, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo failed with status %d", res.StatusCode)
	}

	var info struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}

	return &calendar.OAuthUserInfo{
		Email:   info.Email,
		Name:    info.Name,
		Picture: info.Picture,
	}, nil
}

func (s *Service) RevokeToken(token string) error {
	req, err := http.NewRequest(http.MethodPost, "https://oauth2.googleapis.com/revoke?token="+url.QueryEscape(token), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	return nil
}

type googleDateTime struct {
	DateTime string `json:"dateTime,omitempty"`
	Date     string `json:"date,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

type googleAttendee struct {
	Email    string `json:"email"`
	Optional bool   `json:"optional,omitempty"`
}

type googleConferenceRequest struct {
	RequestID             string                      `json:"requestId"`
	ConferenceSolutionKey googleConferenceSolutionKey `json:"conferenceSolutionKey"`
}

type googleConferenceSolutionKey struct {
	Type string `json:"type"`
}

type googleConferenceData struct {
	EntryPoints []googleEntryPoint `json:"entryPoints,omitempty"`
}

type googleEntryPoint struct {
	EntryPointType string `json:"entryPointType"`
	URI            string `json:"uri"`
}

type googleEvent struct {
	ID                  string                   `json:"id,omitempty"`
	Summary             string                   `json:"summary"`
	Description         string                   `json:"description,omitempty"`
	Location            string                   `json:"location,omitempty"`
	Start               googleDateTime           `json:"start"`
	End                 googleDateTime           `json:"end"`
	Attendees           []googleAttendee         `json:"attendees,omitempty"`
	ColorID             string                   `json:"colorId,omitempty"`
	Status              string                   `json:"status,omitempty"`
	HangoutLink         string                   `json:"hangoutLink,omitempty"`
	ConferenceData      *googleConferenceData    `json:"conferenceData,omitempty"`
	CreateConferenceReq *googleConferenceRequest `json:"conferenceDataVersion,omitempty"`
}

type googleEventRequest struct {
	Summary                 string                   `json:"summary"`
	Description             string                   `json:"description,omitempty"`
	Location                string                   `json:"location,omitempty"`
	Start                   googleDateTime           `json:"start"`
	End                     googleDateTime           `json:"end"`
	Attendees               []googleAttendee         `json:"attendees,omitempty"`
	ColorID                 string                   `json:"colorId,omitempty"`
	ConferenceData          *googleConferenceReqData `json:"conferenceData,omitempty"`
	GuestsCanModify         bool                     `json:"guestsCanModify"`
	GuestsCanInviteOthers   *bool                    `json:"guestsCanInviteOthers"`
	GuestsCanSeeOtherGuests *bool                    `json:"guestsCanSeeOtherGuests"`
	Visibility              string                   `json:"visibility,omitempty"`
	Transparency            string                   `json:"transparency,omitempty"`
	Recurrence              []string                 `json:"recurrence,omitempty"`
	Reminders               *googleReminders         `json:"reminders,omitempty"`
}

type googleReminders struct {
	UseDefault bool                     `json:"useDefault"`
	Overrides  []googleReminderOverride `json:"overrides,omitempty"`
}

type googleReminderOverride struct {
	Method  string `json:"method"`
	Minutes int    `json:"minutes"`
}

type googleConferenceReqData struct {
	CreateRequest *googleConferenceRequest `json:"createRequest,omitempty"`
}

type googleEventResponse struct {
	ID             string                `json:"id"`
	HangoutLink    string                `json:"hangoutLink,omitempty"`
	ConferenceData *googleConferenceData `json:"conferenceData,omitempty"`
	Status         string                `json:"status,omitempty"`
}

func buildGoogleEvent(event *calendar.CalendarEvent, createMeet bool) googleEventRequest {
	ge := googleEventRequest{
		Summary:                 event.Title,
		Description:             event.Description,
		Location:                event.Location,
		GuestsCanModify:         event.GuestsCanModify,
		GuestsCanInviteOthers:   boolPtr(event.GuestsCanInviteOthers),
		GuestsCanSeeOtherGuests: boolPtr(event.GuestsCanSeeOtherGuests),
		Visibility:              event.Visibility,
		Transparency:            event.Transparency,
		Recurrence:              event.Recurrence,
	}

	tz := event.TimeZone
	if tz == "" {
		tz = "UTC"
	}

	if event.AllDay {
		ge.Start = googleDateTime{Date: event.StartTime.Format("2006-01-02"), TimeZone: tz}
		ge.End = googleDateTime{Date: event.EndTime.Format("2006-01-02"), TimeZone: tz}
	} else {
		ge.Start = googleDateTime{DateTime: event.StartTime.Format(time.RFC3339), TimeZone: tz}
		ge.End = googleDateTime{DateTime: event.EndTime.Format(time.RFC3339), TimeZone: tz}
	}

	for _, a := range event.Attendees {
		ge.Attendees = append(ge.Attendees, googleAttendee{
			Email:    a.Email,
			Optional: a.Optional,
		})
	}

	if event.Color != "" {
		ge.ColorID = mapColorToGoogleColorID(event.Color)
	}

	if createMeet {
		ge.ConferenceData = &googleConferenceReqData{
			CreateRequest: &googleConferenceRequest{
				RequestID:             uuid.New().String(),
				ConferenceSolutionKey: googleConferenceSolutionKey{Type: "hangoutsMeet"},
			},
		}
	}

	if event.RemindersUseDefault || len(event.ReminderOverrides) > 0 {
		rem := &googleReminders{UseDefault: event.RemindersUseDefault}
		for _, o := range event.ReminderOverrides {
			rem.Overrides = append(rem.Overrides, googleReminderOverride{
				Method:  o.Method,
				Minutes: o.Minutes,
			})
		}
		ge.Reminders = rem
	}

	return ge
}

func boolPtr(val bool) *bool {
	v := val
	return &v
}

func derefBoolDefault(ptr *bool, def bool) bool {
	if ptr == nil {
		return def
	}
	return *ptr
}

func mapColorToGoogleColorID(color string) string {
	colorMap := map[string]string{
		"lavender": "1", "sage": "2", "grape": "3", "flamingo": "4",
		"banana": "5", "tangerine": "6", "peacock": "7", "graphite": "8",
		"blueberry": "9", "basil": "10", "tomato": "11",
	}
	if id, ok := colorMap[strings.ToLower(color)]; ok {
		return id
	}
	return ""
}

func (s *Service) CreateGoogleEvent(accessToken string, event *calendar.CalendarEvent, createMeet bool, sendUpdates string) (string, string, error) {
	ge := buildGoogleEvent(event, createMeet)

	body, err := json.Marshal(ge)
	if err != nil {
		return "", "", fmt.Errorf("marshal google event: %w", err)
	}

	params := url.Values{}
	if createMeet {
		params.Set("conferenceDataVersion", "1")
	}
	if sendUpdates != "" {
		params.Set("sendUpdates", sendUpdates)
	}
	apiURL := calendarAPIURL + "/calendars/primary/events"
	if len(params) > 0 {
		apiURL += "?" + params.Encode()
	}

	req, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(string(body)))
	if err != nil {
		return "", "", fmt.Errorf("create google event request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("google calendar create request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		var errBody json.RawMessage
		json.NewDecoder(res.Body).Decode(&errBody)
		return "", "", fmt.Errorf("google calendar create failed (%d): %s", res.StatusCode, string(errBody))
	}

	var resp googleEventResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return "", "", fmt.Errorf("decode google event response: %w", err)
	}

	meetLink := resp.HangoutLink
	if meetLink == "" && resp.ConferenceData != nil {
		for _, ep := range resp.ConferenceData.EntryPoints {
			if ep.EntryPointType == "video" {
				meetLink = ep.URI
				break
			}
		}
	}

	return resp.ID, meetLink, nil
}

func (s *Service) GetGoogleEvent(accessToken string, googleEventID string) (*calendar.CalendarEvent, error) {
	apiURL := calendarAPIURL + "/calendars/primary/events/" + url.PathEscape(googleEventID)

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create google get event request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	res, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google calendar get event request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone {
		return nil, calendar.ErrEventNotFound
	}
	if res.StatusCode != http.StatusOK {
		var errBody json.RawMessage
		json.NewDecoder(res.Body).Decode(&errBody)
		return nil, fmt.Errorf("google calendar get event failed (%d): %s", res.StatusCode, string(errBody))
	}

	var item googleListItem
	if err := json.NewDecoder(res.Body).Decode(&item); err != nil {
		return nil, fmt.Errorf("decode google event response: %w", err)
	}

	return mapGoogleListItemToEvent(item), nil
}

func (s *Service) UpdateGoogleEvent(accessToken string, googleEventID string, event *calendar.CalendarEvent, sendUpdates string) error {
	ge := buildGoogleEvent(event, false)

	body, err := json.Marshal(ge)
	if err != nil {
		return fmt.Errorf("marshal google event: %w", err)
	}

	apiURL := calendarAPIURL + "/calendars/primary/events/" + url.PathEscape(googleEventID)
	if sendUpdates != "" {
		apiURL += "?sendUpdates=" + url.QueryEscape(sendUpdates)
	}

	req, err := http.NewRequest(http.MethodPut, apiURL, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("create google update request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("google calendar update request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		var errBody json.RawMessage
		json.NewDecoder(res.Body).Decode(&errBody)
		return fmt.Errorf("google calendar update failed (%d): %s", res.StatusCode, string(errBody))
	}

	return nil
}

func (s *Service) DeleteGoogleEvent(accessToken string, googleEventID string, sendUpdates string) error {
	apiURL := calendarAPIURL + "/calendars/primary/events/" + url.PathEscape(googleEventID)
	if sendUpdates != "" {
		apiURL += "?sendUpdates=" + url.QueryEscape(sendUpdates)
	}

	req, err := http.NewRequest(http.MethodDelete, apiURL, nil)
	if err != nil {
		return fmt.Errorf("create google delete request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	res, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("google calendar delete request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusGone {
		var errBody json.RawMessage
		json.NewDecoder(res.Body).Decode(&errBody)
		return fmt.Errorf("google calendar delete failed (%d): %s", res.StatusCode, string(errBody))
	}

	return nil
}

type googleListResponse struct {
	Items []googleListItem `json:"items"`
}

type googleListItem struct {
	ID             string                `json:"id"`
	Summary        string                `json:"summary"`
	Description    string                `json:"description"`
	Location       string                `json:"location"`
	Start          googleDateTime        `json:"start"`
	End            googleDateTime        `json:"end"`
	Status         string                `json:"status"`
	HangoutLink    string                `json:"hangoutLink"`
	ConferenceData *googleConferenceData `json:"conferenceData"`
	Attendees      []struct {
		Email          string `json:"email"`
		DisplayName    string `json:"displayName"`
		ResponseStatus string `json:"responseStatus"`
		Optional       bool   `json:"optional"`
	} `json:"attendees"`
	ColorID                 string   `json:"colorId"`
	Visibility              string   `json:"visibility"`
	Transparency            string   `json:"transparency"`
	Recurrence              []string `json:"recurrence"`
	GuestsCanModify         bool     `json:"guestsCanModify"`
	GuestsCanInviteOthers   *bool    `json:"guestsCanInviteOthers"`
	GuestsCanSeeOtherGuests *bool    `json:"guestsCanSeeOtherGuests"`
	Reminders               *struct {
		UseDefault bool `json:"useDefault"`
		Overrides  []struct {
			Method  string `json:"method"`
			Minutes int    `json:"minutes"`
		} `json:"overrides"`
	} `json:"reminders"`
	Created string `json:"created"`
	Updated string `json:"updated"`
}

func (s *Service) ListGoogleEvents(accessToken string, timeMin, timeMax time.Time, query string, maxResults int) ([]*calendar.CalendarEvent, error) {
	params := url.Values{
		"singleEvents": {"true"},
		"orderBy":      {"startTime"},
	}
	if !timeMin.IsZero() {
		params.Set("timeMin", timeMin.Format(time.RFC3339))
	}
	if !timeMax.IsZero() {
		params.Set("timeMax", timeMax.Format(time.RFC3339))
	}
	if query != "" {
		params.Set("q", query)
	}
	if maxResults > 0 {
		params.Set("maxResults", fmt.Sprintf("%d", maxResults))
	} else {
		params.Set("maxResults", "250")
	}

	apiURL := calendarAPIURL + "/calendars/primary/events?" + params.Encode()

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create list request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	res, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google calendar list request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		var errBody json.RawMessage
		json.NewDecoder(res.Body).Decode(&errBody)
		return nil, fmt.Errorf("google calendar list failed (%d): %s", res.StatusCode, string(errBody))
	}

	var listResp googleListResponse
	if err := json.NewDecoder(res.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("decode list response: %w", err)
	}

	events := make([]*calendar.CalendarEvent, 0, len(listResp.Items))
	for _, item := range listResp.Items {
		if item.Status == "cancelled" {
			continue
		}
		ev := &calendar.CalendarEvent{
			GoogleEventID:           item.ID,
			Title:                   item.Summary,
			Description:             item.Description,
			Location:                item.Location,
			Status:                  item.Status,
			Visibility:              item.Visibility,
			Transparency:            item.Transparency,
			Recurrence:              item.Recurrence,
			GuestsCanModify:         item.GuestsCanModify,
			GuestsCanInviteOthers:   derefBoolDefault(item.GuestsCanInviteOthers, true),
			GuestsCanSeeOtherGuests: derefBoolDefault(item.GuestsCanSeeOtherGuests, true),
		}

		if item.Start.DateTime != "" {
			if t, err := time.Parse(time.RFC3339, item.Start.DateTime); err == nil {
				ev.StartTime = t
			}
		} else if item.Start.Date != "" {
			if t, err := time.Parse("2006-01-02", item.Start.Date); err == nil {
				ev.StartTime = t
				ev.AllDay = true
			}
		}
		if item.End.DateTime != "" {
			if t, err := time.Parse(time.RFC3339, item.End.DateTime); err == nil {
				ev.EndTime = t
			}
		} else if item.End.Date != "" {
			if t, err := time.Parse("2006-01-02", item.End.Date); err == nil {
				ev.EndTime = t
			}
		}
		ev.TimeZone = item.Start.TimeZone

		ev.MeetingLink = item.HangoutLink
		if ev.MeetingLink == "" && item.ConferenceData != nil {
			for _, ep := range item.ConferenceData.EntryPoints {
				if ep.EntryPointType == "video" {
					ev.MeetingLink = ep.URI
					break
				}
			}
		}

		for _, a := range item.Attendees {
			ev.Attendees = append(ev.Attendees, calendar.Attendee{
				Email:    a.Email,
				Name:     a.DisplayName,
				Status:   a.ResponseStatus,
				Optional: a.Optional,
			})
		}

		ev.Color = mapGoogleColorIDToName(item.ColorID)

		if item.Reminders != nil {
			ev.RemindersUseDefault = item.Reminders.UseDefault
			for _, o := range item.Reminders.Overrides {
				ev.ReminderOverrides = append(ev.ReminderOverrides, calendar.ReminderOverride{
					Method:  o.Method,
					Minutes: o.Minutes,
				})
			}
		}

		if item.Created != "" {
			if t, err := time.Parse(time.RFC3339, item.Created); err == nil {
				ev.CreatedAt = t
			}
		}
		if item.Updated != "" {
			if t, err := time.Parse(time.RFC3339, item.Updated); err == nil {
				ev.UpdatedAt = t
			}
		}

		events = append(events, ev)
	}

	return events, nil
}

func mapGoogleColorIDToName(colorID string) string {
	reverseMap := map[string]string{
		"1": "lavender", "2": "sage", "3": "grape", "4": "flamingo",
		"5": "banana", "6": "tangerine", "7": "peacock", "8": "graphite",
		"9": "blueberry", "10": "basil", "11": "tomato",
	}
	return reverseMap[colorID]
}

type watchRequest struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Address string `json:"address"`
	Token   string `json:"token,omitempty"`
}

type watchResponse struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	ResourceID string `json:"resourceId"`
	Expiration string `json:"expiration"`
}

func (s *Service) WatchEvents(accessToken, channelID, token, webhookURL string) (*calendar.WatchResponse, error) {
	body, err := json.Marshal(watchRequest{
		ID:      channelID,
		Type:    "web_hook",
		Address: webhookURL,
		Token:   token,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal watch request: %w", err)
	}

	apiURL := calendarAPIURL + "/calendars/primary/events/watch"
	req, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create watch request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("watch events request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		var errBody json.RawMessage
		json.NewDecoder(res.Body).Decode(&errBody)
		return nil, fmt.Errorf("watch events failed (%d): %s", res.StatusCode, string(errBody))
	}

	var resp watchResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode watch response: %w", err)
	}

	expirationMs, _ := strconv.ParseInt(resp.Expiration, 10, 64)
	expiration := time.Unix(0, expirationMs*int64(time.Millisecond))

	return &calendar.WatchResponse{
		ChannelID:  resp.ID,
		ResourceID: resp.ResourceID,
		Expiration: expiration,
	}, nil
}

type stopChannelRequest struct {
	ID         string `json:"id"`
	ResourceID string `json:"resourceId"`
}

func (s *Service) StopChannel(accessToken, channelID, resourceID string) error {
	body, err := json.Marshal(stopChannelRequest{
		ID:         channelID,
		ResourceID: resourceID,
	})
	if err != nil {
		return fmt.Errorf("marshal stop channel request: %w", err)
	}

	apiURL := "https://www.googleapis.com/calendar/v3/channels/stop"
	req, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("create stop channel request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("stop channel request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusNotFound {
		var errBody json.RawMessage
		json.NewDecoder(res.Body).Decode(&errBody)
		return fmt.Errorf("stop channel failed (%d): %s", res.StatusCode, string(errBody))
	}
	return nil
}

type googleIncrementalListResponse struct {
	Items         []googleListItem `json:"items"`
	NextSyncToken string           `json:"nextSyncToken"`
	NextPageToken string           `json:"nextPageToken"`
}

func (s *Service) ListEventsIncremental(accessToken, syncToken string) (*calendar.IncrementalSyncResult, error) {
	var allItems []googleListItem
	pageToken := ""

	for {
		params := url.Values{}
		if syncToken != "" {
			params.Set("syncToken", syncToken)
		} else {

			params.Set("maxResults", "1")
			params.Set("singleEvents", "true")
		}
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}

		apiURL := calendarAPIURL + "/calendars/primary/events?" + params.Encode()
		req, err := http.NewRequest(http.MethodGet, apiURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create incremental list request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)

		res, err := s.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("incremental list request: %w", err)
		}

		if res.StatusCode == http.StatusGone {
			res.Body.Close()
			return &calendar.IncrementalSyncResult{SyncExpired: true}, nil
		}

		if res.StatusCode != http.StatusOK {
			var errBody json.RawMessage
			json.NewDecoder(res.Body).Decode(&errBody)
			res.Body.Close()
			return nil, fmt.Errorf("incremental list failed (%d): %s", res.StatusCode, string(errBody))
		}

		var listResp googleIncrementalListResponse
		if err := json.NewDecoder(res.Body).Decode(&listResp); err != nil {
			res.Body.Close()
			return nil, fmt.Errorf("decode incremental list response: %w", err)
		}
		res.Body.Close()

		allItems = append(allItems, listResp.Items...)

		if listResp.NextPageToken == "" {

			events := make([]*calendar.CalendarEvent, 0, len(allItems))
			for _, item := range allItems {
				ev := mapGoogleListItemToEvent(item)
				events = append(events, ev)
			}
			return &calendar.IncrementalSyncResult{
				Events:        events,
				NextSyncToken: listResp.NextSyncToken,
			}, nil
		}
		pageToken = listResp.NextPageToken
	}
}

func mapGoogleListItemToEvent(item googleListItem) *calendar.CalendarEvent {
	ev := &calendar.CalendarEvent{
		GoogleEventID:           item.ID,
		Title:                   item.Summary,
		Description:             item.Description,
		Location:                item.Location,
		Status:                  item.Status,
		Visibility:              item.Visibility,
		Transparency:            item.Transparency,
		Recurrence:              item.Recurrence,
		GuestsCanModify:         item.GuestsCanModify,
		GuestsCanInviteOthers:   derefBoolDefault(item.GuestsCanInviteOthers, true),
		GuestsCanSeeOtherGuests: derefBoolDefault(item.GuestsCanSeeOtherGuests, true),
	}

	if item.Start.DateTime != "" {
		if t, err := time.Parse(time.RFC3339, item.Start.DateTime); err == nil {
			ev.StartTime = t
		}
	} else if item.Start.Date != "" {
		if t, err := time.Parse("2006-01-02", item.Start.Date); err == nil {
			ev.StartTime = t
			ev.AllDay = true
		}
	}
	if item.End.DateTime != "" {
		if t, err := time.Parse(time.RFC3339, item.End.DateTime); err == nil {
			ev.EndTime = t
		}
	} else if item.End.Date != "" {
		if t, err := time.Parse("2006-01-02", item.End.Date); err == nil {
			ev.EndTime = t
		}
	}
	ev.TimeZone = item.Start.TimeZone

	ev.MeetingLink = item.HangoutLink
	if ev.MeetingLink == "" && item.ConferenceData != nil {
		for _, ep := range item.ConferenceData.EntryPoints {
			if ep.EntryPointType == "video" {
				ev.MeetingLink = ep.URI
				break
			}
		}
	}

	for _, a := range item.Attendees {
		ev.Attendees = append(ev.Attendees, calendar.Attendee{
			Email:    a.Email,
			Name:     a.DisplayName,
			Status:   a.ResponseStatus,
			Optional: a.Optional,
		})
	}

	ev.Color = mapGoogleColorIDToName(item.ColorID)

	if item.Reminders != nil {
		ev.RemindersUseDefault = item.Reminders.UseDefault
		for _, o := range item.Reminders.Overrides {
			ev.ReminderOverrides = append(ev.ReminderOverrides, calendar.ReminderOverride{
				Method:  o.Method,
				Minutes: o.Minutes,
			})
		}
	}

	if item.Created != "" {
		if t, err := time.Parse(time.RFC3339, item.Created); err == nil {
			ev.CreatedAt = t
		}
	}
	if item.Updated != "" {
		if t, err := time.Parse(time.RFC3339, item.Updated); err == nil {
			ev.UpdatedAt = t
		}
	}

	return ev
}
