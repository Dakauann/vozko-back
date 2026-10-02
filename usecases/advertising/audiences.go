package advertising

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"slices"
	"strings"

	ads "vozko/domain/advertising"
	"vozko/domain/crmfilter"
)

const maxCustomerRows = 500_000

var ErrCustomerFileUnreadable = errors.New("ads: the customer file is not a readable csv")

type audienceGateway interface {
	CustomAudienceTermsAccepted(ctx context.Context, token, metaAccountID string) (bool, error)
	ListAudiences(ctx context.Context, token, metaAccountID string) ([]ads.Audience, error)
	CreateCustomerList(ctx context.Context, token, metaAccountID, name, description string) (string, error)
	AddCustomers(ctx context.Context, token, audienceID string, batch ads.HashedCustomers, session ads.CustomerSession) error
	CreateLookalike(ctx context.Context, token, metaAccountID string, draft ads.LookalikeDraft) (string, error)
	DeleteAudience(ctx context.Context, token, audienceID string) error
}

type CustomerDirectory interface {
	Customers(ctx context.Context, workspaceID string, filter crmfilter.Filter, limit int) ([]ads.Customer, error)
}

type RawFiles interface {
	Bytes(ctx context.Context, workspaceID, mediaID string) ([]byte, error)
}

type AudienceList struct {
	TermsAccepted bool
	TermsURL      string
	Audiences     []ads.Audience
}

type CustomerListResult struct {
	Audience ads.Audience
	Matched  int
	Skipped  int
}

type AudienceUseCase struct {
	access    accountAccess
	gateway   audienceGateway
	customers CustomerDirectory
	files     RawFiles
	saved     ads.SavedAudienceRepository
	sessionID func() int64
}

func NewAudienceUseCase(sync *SyncUseCase, gateway audienceGateway, customers CustomerDirectory, files RawFiles, saved ads.SavedAudienceRepository) *AudienceUseCase {
	return &AudienceUseCase{
		access: sync.access, gateway: gateway, customers: customers, files: files, saved: saved,
		sessionID: func() int64 { return rand.Int64N(1<<62) + 1 },
	}
}

func TermsURL(metaAccountID string) string {
	return "https://business.facebook.com/ads/manage/customaudiences/tos/?act=" + ads.NormalizeAccountID(metaAccountID)
}

func (uc *AudienceUseCase) List(ctx context.Context, workspaceID, accountID string) (*AudienceList, error) {
	account, token, err := uc.access.open(ctx, workspaceID, accountID, ads.ScopeAdsRead)
	if err != nil {
		return nil, err
	}
	accepted, err := uc.gateway.CustomAudienceTermsAccepted(ctx, token, account.MetaAccountID)
	if err != nil {
		return nil, uc.access.failed(ctx, account, err)
	}
	audiences, err := uc.gateway.ListAudiences(ctx, token, account.MetaAccountID)
	if err != nil {
		return nil, uc.access.failed(ctx, account, err)
	}
	return &AudienceList{TermsAccepted: accepted, TermsURL: TermsURL(account.MetaAccountID), Audiences: audiences}, nil
}

func (uc *AudienceUseCase) openForAudiences(ctx context.Context, workspaceID, accountID string) (*ads.AdAccount, string, error) {
	account, token, err := uc.access.open(ctx, workspaceID, accountID, ads.ScopeAdsManagement)
	if err != nil {
		return nil, "", err
	}
	accepted, err := uc.gateway.CustomAudienceTermsAccepted(ctx, token, account.MetaAccountID)
	if err != nil {
		return nil, "", uc.access.failed(ctx, account, err)
	}
	if !accepted {
		return nil, "", ads.ErrAudienceTermsNotAccepted
	}
	return account, token, nil
}

func (uc *AudienceUseCase) CreateCustomerList(ctx context.Context, workspaceID string, draft ads.CustomerListDraft) (*CustomerListResult, error) {
	draft.Name = strings.TrimSpace(draft.Name)
	if err := draft.Validate(); err != nil {
		return nil, err
	}
	account, token, err := uc.openForAudiences(ctx, workspaceID, draft.AdAccountID)
	if err != nil {
		return nil, err
	}
	hashed, err := uc.hashedCustomers(ctx, workspaceID, draft)
	if err != nil {
		return nil, err
	}
	if len(hashed.Rows) == 0 {
		return nil, ads.ErrNoCustomersMatched
	}
	id, err := uc.gateway.CreateCustomerList(ctx, token, account.MetaAccountID, draft.Name, draft.Description)
	if err != nil {
		return nil, uc.access.failed(ctx, account, err)
	}
	if err := uc.upload(ctx, token, id, hashed); err != nil {
		if delErr := uc.gateway.DeleteAudience(ctx, token, id); delErr != nil {
			log.Printf("[ads] partial customer list %s could not be removed: %v", id, delErr)
		}
		return nil, uc.access.failed(ctx, account, err)
	}
	return &CustomerListResult{
		Audience: ads.Audience{MetaID: id, Name: draft.Name, Description: draft.Description, Kind: ads.AudienceCustomerList, ApproxLower: -1, ApproxUpper: -1},
		Matched:  len(hashed.Rows),
		Skipped:  hashed.Skipped,
	}, nil
}

func (uc *AudienceUseCase) upload(ctx context.Context, token, audienceID string, hashed ads.HashedCustomers) error {
	session := ads.CustomerSession{ID: uc.sessionID(), TotalRows: len(hashed.Rows)}
	batches := ads.Batches(hashed.Rows, ads.CustomerBatchSize())
	for i, rows := range batches {
		session.BatchSeq = i + 1
		session.LastBatch = i == len(batches)-1
		if err := uc.gateway.AddCustomers(ctx, token, audienceID, ads.HashedCustomers{Keys: hashed.Keys, Rows: rows}, session); err != nil {
			return err
		}
	}
	return nil
}

var crmKeys = []ads.MatchKey{ads.MatchPhone, ads.MatchFirstName, ads.MatchLastName}

func (uc *AudienceUseCase) hashedCustomers(ctx context.Context, workspaceID string, draft ads.CustomerListDraft) (ads.HashedCustomers, error) {
	if draft.Source == ads.SourceCRM {
		customers, err := uc.customers.Customers(ctx, workspaceID, draft.CRMFilter, maxCustomerRows)
		if err != nil {
			return ads.HashedCustomers{}, err
		}
		return ads.HashCustomers(crmKeys, customers, ""), nil
	}
	data, err := uc.files.Bytes(ctx, workspaceID, draft.FileMediaID)
	if err != nil {
		return ads.HashedCustomers{}, err
	}
	customers, err := readCustomerCSV(data, draft.Columns, draft.SkipHeader)
	if err != nil {
		return ads.HashedCustomers{}, err
	}
	keys := make([]ads.MatchKey, 0, len(draft.Columns))
	for _, k := range draft.Columns {
		if k != "" && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	return ads.HashCustomers(keys, customers, "55"), nil
}

func readCustomerCSV(data []byte, columns []ads.MatchKey, skipHeader bool) ([]ads.Customer, error) {
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	reader.FieldsPerRecord = -1
	reader.Comma = detectSeparator(data)
	var out []ads.Customer
	for line := 0; ; line++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%w: line %d: %v", ErrCustomerFileUnreadable, line+1, err)
		}
		if line == 0 && skipHeader {
			continue
		}
		if len(out) >= maxCustomerRows {
			return nil, fmt.Errorf("%w: more than %d rows", ErrCustomerFileUnreadable, maxCustomerRows)
		}
		customer := ads.Customer{}
		for i, key := range columns {
			if key != "" && i < len(record) {
				customer[key] = record[i]
			}
		}
		out = append(out, customer)
	}
}

func detectSeparator(data []byte) rune {
	firstLine, _, _ := bytes.Cut(data, []byte("\n"))
	if bytes.Count(firstLine, []byte(";")) > bytes.Count(firstLine, []byte(",")) {
		return ';'
	}
	return ','
}

func (uc *AudienceUseCase) owned(ctx context.Context, token, metaAccountID, audienceID string) (*ads.Audience, error) {
	audiences, err := uc.gateway.ListAudiences(ctx, token, metaAccountID)
	if err != nil {
		return nil, err
	}
	for i := range audiences {
		if audiences[i].MetaID == audienceID {
			return &audiences[i], nil
		}
	}
	return nil, ads.ErrAudienceNotFound
}

func (uc *AudienceUseCase) CreateLookalike(ctx context.Context, workspaceID string, draft ads.LookalikeDraft) (*ads.Audience, error) {
	draft.Name = strings.TrimSpace(draft.Name)
	if err := draft.Validate(); err != nil {
		return nil, err
	}
	account, token, err := uc.openForAudiences(ctx, workspaceID, draft.AdAccountID)
	if err != nil {
		return nil, err
	}
	if _, err := uc.owned(ctx, token, account.MetaAccountID, draft.OriginAudienceID); err != nil {
		if errors.Is(err, ads.ErrAudienceNotFound) {
			return nil, ads.FieldError("originAudienceId", "not_available")
		}
		return nil, uc.access.failed(ctx, account, err)
	}
	id, err := uc.gateway.CreateLookalike(ctx, token, account.MetaAccountID, draft)
	if err != nil {
		return nil, uc.access.failed(ctx, account, err)
	}
	return &ads.Audience{MetaID: id, Name: draft.Name, Kind: ads.AudienceLookalike, OriginAudienceID: draft.OriginAudienceID, LookalikeRatio: draft.Ratio(), ApproxLower: -1, ApproxUpper: -1}, nil
}

func (uc *AudienceUseCase) Delete(ctx context.Context, workspaceID, accountID, audienceID string) error {
	account, token, err := uc.access.open(ctx, workspaceID, accountID, ads.ScopeAdsManagement)
	if err != nil {
		return err
	}
	if _, err := uc.owned(ctx, token, account.MetaAccountID, audienceID); err != nil {
		return uc.access.failed(ctx, account, err)
	}
	return uc.access.failed(ctx, account, uc.gateway.DeleteAudience(ctx, token, audienceID))
}

func (uc *AudienceUseCase) SavedList(ctx context.Context, workspaceID string) ([]*ads.SavedAudience, error) {
	return uc.saved.List(ctx, workspaceID)
}

func (uc *AudienceUseCase) Save(ctx context.Context, workspaceID, userID string, s ads.SavedAudience) (*ads.SavedAudience, error) {
	s.Normalize()
	if err := s.Validate(); err != nil {
		return nil, err
	}
	s.WorkspaceID = workspaceID
	now := uc.access.now()
	if s.ID == "" {
		s.CreatedBy, s.CreatedAt, s.UpdatedAt = userID, now, now
		return &s, uc.saved.Create(ctx, &s)
	}
	existing, err := uc.saved.Find(ctx, workspaceID, s.ID)
	if err != nil {
		return nil, err
	}
	s.CreatedBy, s.CreatedAt, s.UpdatedAt = existing.CreatedBy, existing.CreatedAt, now
	return &s, uc.saved.Update(ctx, &s)
}

func (uc *AudienceUseCase) DeleteSaved(ctx context.Context, workspaceID, id string) error {
	return uc.saved.Delete(ctx, workspaceID, id)
}
