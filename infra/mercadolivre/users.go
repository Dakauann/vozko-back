package mercadolivre

import (
	"context"
	"errors"

	mldomain "vozko/domain/mercadolivre"
)

type UserService struct {
	c *Client
}

var _ mldomain.UserService = (*UserService)(nil)

func NewUserService(c *Client) (*UserService, error) {
	if c == nil {
		return nil, errors.New("mercadolivre: user service requires a client")
	}
	return &UserService{c: c}, nil
}

type userWire struct {
	ID        mldomain.FlexibleID `json:"id"`
	Nickname  string              `json:"nickname"`
	FirstName string              `json:"first_name"`
	LastName  string              `json:"last_name"`
	Company   *userCompanyWire    `json:"company"`
	SiteID    string              `json:"site_id"`
	CountryID string              `json:"country_id"`
	UserType  string              `json:"user_type"`
	Permalink string              `json:"permalink"`
	Tags      []string            `json:"tags"`
}

type userCompanyWire struct {
	BrandName     string `json:"brand_name"`
	CorporateName string `json:"corporate_name"`
}

func (s *UserService) Me(ctx context.Context, creds mldomain.Credentials) (*mldomain.User, error) {
	return s.fetch(ctx, creds, "/users/me")
}

func (s *UserService) GetUser(ctx context.Context, creds mldomain.Credentials, userID string) (*mldomain.User, error) {
	if !mldomain.ValidNumericID(userID) {
		return nil, mldomain.ErrUserIDInvalid
	}
	return s.fetch(ctx, creds, "/users/"+pathEscape(userID))
}

func (s *UserService) fetch(ctx context.Context, creds mldomain.Credentials, path string) (*mldomain.User, error) {
	var w userWire
	if err := s.c.getJSON(ctx, request{path: path, family: FamilyOther, creds: creds}, &w); err != nil {
		return nil, err
	}
	if !mldomain.ValidNumericID(w.ID.String()) {
		return nil, &Error{Code: CodeDecode, Message: "user answer carries no id"}
	}
	user := &mldomain.User{
		ID:        w.ID.String(),
		Nickname:  w.Nickname,
		FirstName: w.FirstName,
		LastName:  w.LastName,
		SiteID:    w.SiteID,
		CountryID: w.CountryID,
		UserType:  w.UserType,
		Permalink: w.Permalink,
		Tags:      w.Tags,
	}
	if w.Company != nil {
		user.BrandName, user.CorporateName = w.Company.BrandName, w.Company.CorporateName
	}
	return user, nil
}
