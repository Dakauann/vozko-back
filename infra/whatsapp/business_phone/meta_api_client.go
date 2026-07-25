package whatsapp_business_phone

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	businessphone "vozko/domain/whatsapp/business_phone"
)

type MetaAPIClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewMetaAPIClient(httpClient *http.Client) businessphone.MetaAPIService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	return &MetaAPIClient{
		baseURL:    "https://graph.facebook.com/v22.0",
		httpClient: httpClient,
	}
}

type MetaAPIErrorResponse struct {
	Error *MetaAPIErrorDetail `json:"error"`
}

type MetaAPIErrorDetail struct {
	Message        string `json:"message"`
	Type           string `json:"type"`
	Code           int    `json:"code"`
	ErrorSubcode   int    `json:"error_subcode"`
	FBTraceID      string `json:"fbtrace_id"`
	ErrorUserTitle string `json:"error_user_title"`
	ErrorUserMsg   string `json:"error_user_msg"`
	IsTransient    bool   `json:"is_transient"`
}

func parseMetaAPIError(statusCode int, body []byte) error {
	var errorResp MetaAPIErrorResponse
	if err := json.Unmarshal(body, &errorResp); err == nil && errorResp.Error != nil {
		detail := errorResp.Error
		errMsg := fmt.Sprintf("Meta API error (status=%d, code=%d", statusCode, detail.Code)
		if detail.ErrorSubcode != 0 {
			errMsg += fmt.Sprintf(", subcode=%d", detail.ErrorSubcode)
		}
		errMsg += fmt.Sprintf("): %s", detail.Message)
		if detail.ErrorUserTitle != "" {
			errMsg += fmt.Sprintf(" [user_title: %s]", detail.ErrorUserTitle)
		}
		if detail.ErrorUserMsg != "" {
			errMsg += fmt.Sprintf(" [user_message: %s]", detail.ErrorUserMsg)
		}
		if detail.IsTransient {
			errMsg += " [transient: true]"
		}
		if detail.Type != "" {
			errMsg += fmt.Sprintf(" [type: %s]", detail.Type)
		}
		if detail.FBTraceID != "" {
			errMsg += fmt.Sprintf(" [trace_id: %s]", detail.FBTraceID)
		}
		return fmt.Errorf("%w: %s", businessphone.ErrMetaAPIError, errMsg)
	}
	return fmt.Errorf("%w: status=%d body=%s", businessphone.ErrMetaAPIError, statusCode, string(body))
}

func parseMetaAPIErrorWithFallback(statusCode int, body []byte, fallbackErr error) error {
	var errorResp MetaAPIErrorResponse
	if err := json.Unmarshal(body, &errorResp); err == nil && errorResp.Error != nil {
		detail := errorResp.Error
		errMsg := fmt.Sprintf("Meta API error (status=%d, code=%d", statusCode, detail.Code)
		if detail.ErrorSubcode != 0 {
			errMsg += fmt.Sprintf(", subcode=%d", detail.ErrorSubcode)
		}
		errMsg += fmt.Sprintf("): %s", detail.Message)
		if detail.ErrorUserTitle != "" {
			errMsg += fmt.Sprintf(" [user_title: %s]", detail.ErrorUserTitle)
		}
		if detail.ErrorUserMsg != "" {
			errMsg += fmt.Sprintf(" [user_message: %s]", detail.ErrorUserMsg)
		}
		if detail.IsTransient {
			errMsg += " [transient: true]"
		}
		if detail.Type != "" {
			errMsg += fmt.Sprintf(" [type: %s]", detail.Type)
		}
		if detail.FBTraceID != "" {
			errMsg += fmt.Sprintf(" [trace_id: %s]", detail.FBTraceID)
		}
		return fmt.Errorf("%w: %s", fallbackErr, errMsg)
	}
	return fmt.Errorf("%w: status=%d body=%s", fallbackErr, statusCode, string(body))
}

func (c *MetaAPIClient) GetWABA(wabaID string, accessToken string) (*businessphone.MetaWABAInfo, error) {
	endpoint := fmt.Sprintf("%s/%s?fields=id,name,currency,timezone_id,message_template_namespace,account_review_status,business_verification_status,ownership_type,status,country",
		c.baseURL, url.PathEscape(wabaID))

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get WABA request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get WABA request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseMetaAPIError(resp.StatusCode, body)
	}

	var info businessphone.MetaWABAInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to parse WABA response: %w", err)
	}

	return &info, nil
}

type listPhoneNumbersResponse struct {
	Data []phoneNumberResponse `json:"data"`
}

type phoneNumberResponse struct {
	ID                     string `json:"id"`
	DisplayPhoneNumber     string `json:"display_phone_number"`
	VerifiedName           string `json:"verified_name"`
	Status                 string `json:"status"`
	QualityRating          string `json:"quality_rating"`
	NameStatus             string `json:"name_status"`
	CodeVerificationStatus string `json:"code_verification_status"`
	IsOfficialBusiness     bool   `json:"is_official_business_account"`
}

func (c *MetaAPIClient) ListPhoneNumbers(wabaID string, accessToken string) ([]businessphone.MetaPhoneNumberInfo, error) {
	endpoint := fmt.Sprintf("%s/%s/phone_numbers?fields=id,display_phone_number,verified_name,status,quality_rating,name_status,code_verification_status,is_official_business_account", c.baseURL, wabaID)

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create list phone numbers request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list phone numbers request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseMetaAPIError(resp.StatusCode, body)
	}

	var listResp listPhoneNumbersResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	result := make([]businessphone.MetaPhoneNumberInfo, len(listResp.Data))
	for i, pn := range listResp.Data {
		result[i] = businessphone.MetaPhoneNumberInfo{
			ID:                     pn.ID,
			DisplayPhoneNumber:     pn.DisplayPhoneNumber,
			VerifiedName:           pn.VerifiedName,
			Status:                 pn.Status,
			QualityRating:          pn.QualityRating,
			NameStatus:             pn.NameStatus,
			CodeVerificationStatus: pn.CodeVerificationStatus,
			IsOfficialBusiness:     pn.IsOfficialBusiness,
		}
	}

	return result, nil
}

func (c *MetaAPIClient) GetPhoneNumber(phoneNumberID string, accessToken string) (*businessphone.MetaPhoneNumberInfo, error) {
	endpoint := fmt.Sprintf("%s/%s?fields=id,display_phone_number,verified_name,status,quality_rating,name_status,code_verification_status,is_official_business_account", c.baseURL, phoneNumberID)

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get phone number request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get phone number request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode == 404 {
		return nil, businessphone.ErrPhoneNumberNotFound
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseMetaAPIError(resp.StatusCode, body)
	}

	var pn phoneNumberResponse
	if err := json.Unmarshal(body, &pn); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &businessphone.MetaPhoneNumberInfo{
		ID:                     pn.ID,
		DisplayPhoneNumber:     pn.DisplayPhoneNumber,
		VerifiedName:           pn.VerifiedName,
		Status:                 pn.Status,
		QualityRating:          pn.QualityRating,
		NameStatus:             pn.NameStatus,
		CodeVerificationStatus: pn.CodeVerificationStatus,
		IsOfficialBusiness:     pn.IsOfficialBusiness,
	}, nil
}

func (c *MetaAPIClient) RequestVerificationCode(phoneNumberID string, method string, language string, accessToken string) error {
	if strings.TrimSpace(method) == "" {
		method = "SMS"
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if language == "" {
		language = "en_US"
	}
	language = strings.TrimSpace(language)

	endpoint := fmt.Sprintf("%s/%s/request_code?code_method=%s&language=%s", c.baseURL, phoneNumberID, url.QueryEscape(method), url.QueryEscape(language))

	req, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request verification code request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request verification code failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseMetaAPIError(resp.StatusCode, body)
	}

	return nil
}

func (c *MetaAPIClient) VerifyCode(phoneNumberID string, code string, accessToken string) (bool, error) {
	endpoint := fmt.Sprintf("%s/%s/verify_code", c.baseURL, phoneNumberID)
	payload := map[string]string{"code": code}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("failed to marshal verify code payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return false, fmt.Errorf("failed to create verify code request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("verify code request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if strings.Contains(string(body), "invalid") || strings.Contains(string(body), "expired") {
			return false, businessphone.ErrVerificationCodeInvalid
		}
		return false, parseMetaAPIError(resp.StatusCode, body)
	}

	var result struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return true, nil
	}

	return result.Success, nil
}

func (c *MetaAPIClient) RegisterPhone(phoneNumberID string, pin string, accessToken string) error {
	endpoint := fmt.Sprintf("%s/%s/register", c.baseURL, phoneNumberID)

	payload := map[string]string{
		"messaging_product": "whatsapp",
		"pin":               pin,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal register phone payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create register phone request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("register phone request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseMetaAPIError(resp.StatusCode, body)
	}

	return nil
}

func (c *MetaAPIClient) DeregisterPhone(phoneNumberID string, accessToken string) error {
	endpoint := fmt.Sprintf("%s/%s/deregister", c.baseURL, phoneNumberID)

	req, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create deregister phone request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("deregister phone request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseMetaAPIError(resp.StatusCode, body)
	}

	return nil
}

func (c *MetaAPIClient) UnsubscribeApp(wabaID string, accessToken string) error {
	endpoint := fmt.Sprintf("%s/%s/subscribed_apps", c.baseURL, wabaID)

	req, err := http.NewRequest(http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create unsubscribe app request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("unsubscribe app request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseMetaAPIError(resp.StatusCode, body)
	}

	return nil
}

type businessProfileResponse struct {
	Data []struct {
		About             string   `json:"about"`
		Address           string   `json:"address"`
		Description       string   `json:"description"`
		Email             string   `json:"email"`
		ProfilePictureURL string   `json:"profile_picture_url"`
		Websites          []string `json:"websites"`
		Vertical          string   `json:"vertical"`
	} `json:"data"`
}

func (c *MetaAPIClient) GetBusinessProfile(phoneNumberID string, accessToken string) (*businessphone.MetaBusinessProfile, error) {
	endpoint := fmt.Sprintf("%s/%s/whatsapp_business_profile?fields=about,address,description,email,profile_picture_url,websites,vertical", c.baseURL, phoneNumberID)

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get business profile request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get business profile request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseMetaAPIError(resp.StatusCode, body)
	}

	var profileResp businessProfileResponse
	if err := json.Unmarshal(body, &profileResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(profileResp.Data) == 0 {
		return &businessphone.MetaBusinessProfile{}, nil
	}

	data := profileResp.Data[0]
	return &businessphone.MetaBusinessProfile{
		About:             data.About,
		Address:           data.Address,
		Description:       data.Description,
		Email:             data.Email,
		ProfilePictureURL: data.ProfilePictureURL,
		Websites:          data.Websites,
		Vertical:          data.Vertical,
	}, nil
}

type updateBusinessProfileRequest struct {
	MessagingProduct     string   `json:"messaging_product"`
	About                string   `json:"about,omitempty"`
	Address              string   `json:"address,omitempty"`
	Description          string   `json:"description,omitempty"`
	Email                string   `json:"email,omitempty"`
	Websites             []string `json:"websites,omitempty"`
	Vertical             string   `json:"vertical,omitempty"`
	ProfilePictureHandle string   `json:"profile_picture_handle,omitempty"`
}

func (c *MetaAPIClient) UpdateBusinessProfile(phoneNumberID string, profile businessphone.MetaBusinessProfile, accessToken string) error {
	endpoint := fmt.Sprintf("%s/%s/whatsapp_business_profile", c.baseURL, phoneNumberID)

	payload := updateBusinessProfileRequest{
		MessagingProduct:     "whatsapp",
		About:                profile.About,
		Address:              profile.Address,
		Description:          profile.Description,
		Email:                profile.Email,
		Websites:             profile.Websites,
		Vertical:             profile.Vertical,
		ProfilePictureHandle: profile.ProfilePictureHandle,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal update profile payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create update business profile request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("update business profile request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if metaErr := parseMetaAPIErrorWithFallback(resp.StatusCode, body, businessphone.ErrBusinessProfileUpdateFailed); metaErr != nil {
			return metaErr
		}
	}

	return nil
}

func (c *MetaAPIClient) UploadProfilePicture(data []byte, fileName string, accessToken string) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("profile picture data is empty")
	}

	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		mimeType = "image/jpeg"
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("messaging_product", "whatsapp"); err != nil {
		return "", fmt.Errorf("failed to write messaging_product field: %w", err)
	}

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, fileName))
	h.Set("Content-Type", mimeType)

	filePart, err := writer.CreatePart(h)
	if err != nil {
		return "", fmt.Errorf("failed to create file part: %w", err)
	}

	if _, err := filePart.Write(data); err != nil {
		return "", fmt.Errorf("failed to write image data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	endpoint := fmt.Sprintf("%s/app/uploads", c.baseURL)

	req, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		return "", fmt.Errorf("failed to create upload request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read upload response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if metaErr := parseMetaAPIErrorWithFallback(resp.StatusCode, respBody, businessphone.ErrProfilePictureUploadFailed); metaErr != nil {
			return "", metaErr
		}
	}

	var uploadResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &uploadResp); err != nil {
		return "", fmt.Errorf("failed to parse upload response: %w", err)
	}

	return uploadResp.ID, nil
}

func (c *MetaAPIClient) SubscribeWebhooks(wabaID string, accessToken string) error {
	endpoint := fmt.Sprintf("%s/%s/subscribed_apps", c.baseURL, wabaID)

	form := url.Values{}
	form.Set("subscribed_fields", strings.Join(businessphone.SubscriptionWebhookFields(), ","))

	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create subscribe request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("subscribe request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read subscribe response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if metaErr := parseMetaAPIErrorWithFallback(resp.StatusCode, body, businessphone.ErrMetaAPIError); metaErr != nil {
			return metaErr
		}
		return businessphone.ErrMetaAPIError
	}
	return nil
}

type phoneSettingsResponse struct {
	Calling struct {
		Status string `json:"status"`
	} `json:"calling"`
}

func (c *MetaAPIClient) GetCallingStatus(phoneNumberID string, accessToken string) (bool, error) {
	endpoint := fmt.Sprintf("%s/%s/settings", c.baseURL, phoneNumberID)

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create get settings request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("get settings request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read settings response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if metaErr := parseMetaAPIErrorWithFallback(resp.StatusCode, body, businessphone.ErrMetaAPIError); metaErr != nil {
			return false, metaErr
		}
		return false, businessphone.ErrMetaAPIError
	}

	var settings phoneSettingsResponse
	if err := json.Unmarshal(body, &settings); err != nil {
		return false, fmt.Errorf("failed to parse settings response: %w", err)
	}

	return strings.EqualFold(settings.Calling.Status, "ENABLED"), nil
}

// BlockUser adds a contact number to the phone's blocklist so Meta stops
// delivering that user's inbound messages to the webhook. Note Meta only allows
// blocking a user that messaged the business within the last 24h.
func (c *MetaAPIClient) BlockUser(phoneNumberID string, userNumber string, accessToken string) error {
	return c.mutateBlockUsers(http.MethodPost, phoneNumberID, userNumber, accessToken)
}

// UnblockUser removes a contact number from the phone's blocklist.
func (c *MetaAPIClient) UnblockUser(phoneNumberID string, userNumber string, accessToken string) error {
	return c.mutateBlockUsers(http.MethodDelete, phoneNumberID, userNumber, accessToken)
}

func (c *MetaAPIClient) mutateBlockUsers(method string, phoneNumberID string, userNumber string, accessToken string) error {
	phoneNumberID = strings.TrimSpace(phoneNumberID)
	userNumber = strings.TrimSpace(userNumber)
	if phoneNumberID == "" || userNumber == "" {
		return fmt.Errorf("%w: phone number id and user number are required", businessphone.ErrMetaAPIError)
	}

	endpoint := fmt.Sprintf("%s/%s/block_users", c.baseURL, phoneNumberID)
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"block_users": []map[string]string{
			{"user": userNumber},
		},
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal block users payload: %w", err)
	}

	req, err := http.NewRequest(method, endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create block users request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("block users request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read block users response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if metaErr := parseMetaAPIErrorWithFallback(resp.StatusCode, body, businessphone.ErrMetaAPIError); metaErr != nil {
			return metaErr
		}
		return businessphone.ErrMetaAPIError
	}

	return nil
}

func (c *MetaAPIClient) SetCallingStatus(phoneNumberID string, enabled bool, accessToken string) error {
	log.Printf("[meta-api] SetCallingStatus: metaPhoneID=%s enabled=%v", phoneNumberID, enabled)
	endpoint := fmt.Sprintf("%s/%s/settings", c.baseURL, phoneNumberID)

	calling := map[string]any{"status": "DISABLED"}
	if enabled {
		calling["status"] = "ENABLED"

		calling["audio"] = map[string]any{
			"additional_codecs": []string{"PCMU", "PCMA"},
		}
	}
	payload := map[string]any{
		"calling": calling,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal calling settings payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create set settings request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("[meta-api] SetCallingStatus transport error: metaPhoneID=%s enabled=%v: %v", phoneNumberID, enabled, err)
		return fmt.Errorf("set settings request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read settings response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[meta-api] SetCallingStatus rejected by Meta: metaPhoneID=%s enabled=%v status=%d body=%s", phoneNumberID, enabled, resp.StatusCode, string(body))
		if metaErr := parseMetaAPIErrorWithFallback(resp.StatusCode, body, businessphone.ErrMetaAPIError); metaErr != nil {
			return metaErr
		}
		return businessphone.ErrMetaAPIError
	}

	log.Printf("[meta-api] SetCallingStatus ok: metaPhoneID=%s enabled=%v", phoneNumberID, enabled)
	return nil
}
