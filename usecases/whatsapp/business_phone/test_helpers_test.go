package businessphone_usecase

import (
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/waba"
)

type mockRepository struct {
	phoneNumbers        map[string]*businessphone.WhatsAppBusinessPhoneNumber
	deletedPhoneNumbers map[string]*businessphone.WhatsAppBusinessPhoneNumber
	createErr           error
	updateErr           error
	findErr             error
	deleteErr           error
	listErr             error
}

func newMockRepo() *mockRepository {
	return &mockRepository{
		phoneNumbers:        make(map[string]*businessphone.WhatsAppBusinessPhoneNumber),
		deletedPhoneNumbers: make(map[string]*businessphone.WhatsAppBusinessPhoneNumber),
	}
}

func (m *mockRepository) Create(phoneNumber *businessphone.WhatsAppBusinessPhoneNumber) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.phoneNumbers[phoneNumber.ID] = phoneNumber
	return nil
}

func (m *mockRepository) Update(id string, phoneNumber *businessphone.WhatsAppBusinessPhoneNumber) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.phoneNumbers[id] = phoneNumber
	return nil
}

func (m *mockRepository) Delete(id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.phoneNumbers, id)
	return nil
}

func (m *mockRepository) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	phone, ok := m.phoneNumbers[id]
	if !ok {
		return nil, businessphone.ErrPhoneNumberNotFound
	}
	return phone, nil
}

func (m *mockRepository) FindByMetaPhoneNumberID(metaPhoneNumberID string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	for _, p := range m.phoneNumbers {
		if p.MetaPhoneNumberID == metaPhoneNumberID {
			return p, nil
		}
	}
	return nil, businessphone.ErrPhoneNumberNotFound
}

func (m *mockRepository) FindByDisplayPhoneNumber(displayPhoneNumber string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	for _, p := range m.phoneNumbers {
		if p.DisplayPhoneNumber == displayPhoneNumber {
			return p, nil
		}
	}
	return nil, businessphone.ErrPhoneNumberNotFound
}

func (m *mockRepository) List(input businessphone.ListInput) (*shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber], error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	result := make([]*businessphone.WhatsAppBusinessPhoneNumber, 0)
	for _, p := range m.phoneNumbers {
		if input.OwnerWorkspaceID != "" || len(input.AccessPhoneIDs) > 0 {
			ownerAllowed := input.OwnerWorkspaceID != "" && p.OwnerWorkspaceID == input.OwnerWorkspaceID
			grantedAllowed := false
			for _, phoneID := range input.AccessPhoneIDs {
				if phoneID == p.ID {
					grantedAllowed = true
					break
				}
			}
			if !ownerAllowed && !grantedAllowed {
				continue
			}
		}
		if input.WABAId != "" && p.WABAId != input.WABAId {
			continue
		}
		if input.Status != "" && p.Status != input.Status {
			continue
		}
		if input.QualityRating != "" && p.QualityRating != input.QualityRating {
			continue
		}
		result = append(result, p)
	}
	return &shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber]{
		Items:      result,
		TotalItems: int64(len(result)),
		Page:       1,
		PageSize:   10,
	}, nil
}

func (m *mockRepository) UpdateStatus(id string, status businessphone.Status) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	phone, ok := m.phoneNumbers[id]
	if !ok {
		return businessphone.ErrPhoneNumberNotFound
	}
	phone.Status = status
	return nil
}

func (m *mockRepository) UpdateCallsEnabled(id string, enabled bool) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	phone, ok := m.phoneNumbers[id]
	if !ok {
		return businessphone.ErrPhoneNumberNotFound
	}
	phone.CallsEnabled = enabled
	return nil
}

func (m *mockRepository) UpdateBusinessProfile(id string, profile businessphone.BusinessProfile) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	phone, ok := m.phoneNumbers[id]
	if !ok {
		return businessphone.ErrPhoneNumberNotFound
	}
	phone.BusinessProfile = profile
	return nil
}

func (m *mockRepository) SyncFromMeta(phoneNumber *businessphone.WhatsAppBusinessPhoneNumber) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.phoneNumbers[phoneNumber.ID] = phoneNumber
	return nil
}

func (m *mockRepository) BatchUpdate(phones []*businessphone.WhatsAppBusinessPhoneNumber) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	for _, p := range phones {
		m.phoneNumbers[p.ID] = p
	}
	return nil
}

func (m *mockRepository) ListAll() ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	items := make([]*businessphone.WhatsAppBusinessPhoneNumber, 0, len(m.phoneNumbers))
	for _, phone := range m.phoneNumbers {
		items = append(items, phone)
	}
	return items, nil
}

func (m *mockRepository) FindByWABAId(wabaID string) ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	result := make([]*businessphone.WhatsAppBusinessPhoneNumber, 0)
	for _, p := range m.phoneNumbers {
		if p.WABAId == wabaID {
			result = append(result, p)
		}
	}
	return result, nil
}

func (m *mockRepository) ClearOwner(id string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	phone, ok := m.phoneNumbers[id]
	if !ok {
		return businessphone.ErrPhoneNumberNotFound
	}
	phone.OwnerWorkspaceID = ""
	phone.OwnerAssignedBy = ""
	phone.OwnerAssignedAt = nil
	return nil
}

func (m *mockRepository) ClearAccessToken(id string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	phone, ok := m.phoneNumbers[id]
	if !ok {
		return businessphone.ErrPhoneNumberNotFound
	}
	phone.AccessToken = ""
	return nil
}

func (m *mockRepository) FindByMetaPhoneNumberIDUnscoped(metaPhoneNumberID string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	for _, p := range m.deletedPhoneNumbers {
		if p.MetaPhoneNumberID == metaPhoneNumberID {
			return p, nil
		}
	}
	for _, p := range m.phoneNumbers {
		if p.MetaPhoneNumberID == metaPhoneNumberID {
			return p, nil
		}
	}
	return nil, businessphone.ErrPhoneNumberNotFound
}

func (m *mockRepository) Restore(id string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	phone, ok := m.deletedPhoneNumbers[id]
	if !ok {
		return businessphone.ErrPhoneNumberNotFound
	}
	m.phoneNumbers[id] = phone
	delete(m.deletedPhoneNumbers, id)
	return nil
}

type mockMetaAPIService struct {
	phoneNumbers           []businessphone.MetaPhoneNumberInfo
	phoneNumber            *businessphone.MetaPhoneNumberInfo
	businessProfile        *businessphone.MetaBusinessProfile
	listErr                error
	getErr                 error
	requestVerificationErr error
	verifyErr              error
	registerErr            error
	deregisterErr          error
	unsubscribeErr         error
	getProfileErr          error
	updateProfileErr       error
	verifyResult           bool
}

func newMockMetaAPI() *mockMetaAPIService {
	return &mockMetaAPIService{
		phoneNumbers: make([]businessphone.MetaPhoneNumberInfo, 0),
		verifyResult: true,
	}
}

func (m *mockMetaAPIService) GetWABA(wabaID string, accessToken string) (*businessphone.MetaWABAInfo, error) {
	return nil, nil
}

func (m *mockMetaAPIService) ListPhoneNumbers(wabaID string, accessToken string) ([]businessphone.MetaPhoneNumberInfo, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.phoneNumbers, nil
}

func (m *mockMetaAPIService) GetPhoneNumber(phoneNumberID string, accessToken string) (*businessphone.MetaPhoneNumberInfo, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.phoneNumber, nil
}

func (m *mockMetaAPIService) RequestVerificationCode(phoneNumberID string, method string, language string, accessToken string) error {
	return m.requestVerificationErr
}

func (m *mockMetaAPIService) VerifyCode(phoneNumberID string, code string, accessToken string) (bool, error) {
	if m.verifyErr != nil {
		return false, m.verifyErr
	}
	return m.verifyResult, nil
}

func (m *mockMetaAPIService) GetBusinessProfile(phoneNumberID string, accessToken string) (*businessphone.MetaBusinessProfile, error) {
	if m.getProfileErr != nil {
		return nil, m.getProfileErr
	}
	return m.businessProfile, nil
}

func (m *mockMetaAPIService) UpdateBusinessProfile(phoneNumberID string, profile businessphone.MetaBusinessProfile, accessToken string) error {
	return m.updateProfileErr
}

func (m *mockMetaAPIService) UploadProfilePicture(data []byte, fileName string, accessToken string) (string, error) {
	return "https://example.com/picture.jpg", nil
}

func (m *mockMetaAPIService) RegisterPhone(phoneNumberID string, pin string, accessToken string) error {
	return m.registerErr
}

func (m *mockMetaAPIService) DeregisterPhone(phoneNumberID string, accessToken string) error {
	return m.deregisterErr
}

func (m *mockMetaAPIService) GetCallingStatus(phoneNumberID string, accessToken string) (bool, error) {
	return false, nil
}

func (m *mockMetaAPIService) SetCallingStatus(phoneNumberID string, enabled bool, accessToken string) error {
	return nil
}

func (m *mockMetaAPIService) SubscribeWebhooks(wabaID string, accessToken string) error {
	return nil
}

func (m *mockMetaAPIService) BlockUser(phoneNumberID string, userNumber string, accessToken string) error {
	return nil
}

func (m *mockMetaAPIService) UnblockUser(phoneNumberID string, userNumber string, accessToken string) error {
	return nil
}

func (m *mockMetaAPIService) UnsubscribeApp(wabaID string, accessToken string) error {
	return m.unsubscribeErr
}

type mockWABARepository struct {
	accounts  map[string]*waba.WhatsAppBusinessAccount
	updateErr error
}

func newMockWABARepo() *mockWABARepository {
	return &mockWABARepository{
		accounts: make(map[string]*waba.WhatsAppBusinessAccount),
	}
}

func (m *mockWABARepository) Create(account *waba.WhatsAppBusinessAccount) error {
	m.accounts[account.ID] = account
	return nil
}

func (m *mockWABARepository) Update(id string, account *waba.WhatsAppBusinessAccount) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.accounts[id] = account
	return nil
}

func (m *mockWABARepository) Delete(id string) error {
	delete(m.accounts, id)
	return nil
}

func (m *mockWABARepository) FindByID(id string) (*waba.WhatsAppBusinessAccount, error) {
	a, ok := m.accounts[id]
	if !ok {
		return nil, waba.ErrWABANotFound
	}
	return a, nil
}

func (m *mockWABARepository) FindByMetaWABAId(metaWABAId string) (*waba.WhatsAppBusinessAccount, error) {
	for _, a := range m.accounts {
		if a.MetaWABAId == metaWABAId {
			return a, nil
		}
	}
	return nil, waba.ErrWABANotFound
}

func (m *mockWABARepository) List(_ waba.ListInput) (*shared.PaginatedResult[*waba.WhatsAppBusinessAccount], error) {
	return nil, nil
}

func (m *mockWABARepository) ClearAccessToken(id string) error {
	a, ok := m.accounts[id]
	if !ok {
		return waba.ErrWABANotFound
	}
	a.AccessToken = ""
	return nil
}

func seedWABA(repo *mockWABARepository, id, metaWABAId string) *waba.WhatsAppBusinessAccount {
	account := &waba.WhatsAppBusinessAccount{
		ID:                 id,
		MetaWABAId:         metaWABAId,
		Name:               "Test WABA",
		MessagingLimitTier: "TIER_1K",
	}
	repo.accounts[id] = account
	return account
}
