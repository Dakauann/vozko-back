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
	SiteID    string              `json:"site_id"`
	CountryID string              `json:"country_id"`
	UserType  string              `json:"user_type"`
	Permalink string              `json:"permalink"`
	Tags      []string            `json:"tags"`
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
	return &mldomain.User{
		ID:        w.ID.String(),
		Nickname:  w.Nickname,
		SiteID:    w.SiteID,
		CountryID: w.CountryID,
		UserType:  w.UserType,
		Permalink: w.Permalink,
		Tags:      w.Tags,
	}, nil
}
