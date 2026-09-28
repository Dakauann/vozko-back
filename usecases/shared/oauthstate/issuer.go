package oauthstate

import (
	"fmt"
	"strings"
	"time"
)

type Issuer struct {
	secret            string
	nonces            NonceStore
	defaultReturnPath string
}

type IssueInput struct {
	WorkspaceID string
	UserID      string
	ReturnPath  string
	Popup       bool
}

func NewIssuer(secret string, nonces NonceStore, defaultReturnPath string) (*Issuer, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, fmt.Errorf("oauth: state issuer requires a signing secret")
	}
	if nonces == nil {
		return nil, fmt.Errorf("oauth: state issuer requires a nonce store")
	}
	return &Issuer{secret: secret, nonces: nonces, defaultReturnPath: defaultReturnPath}, nil
}

func (i *Issuer) Issue(in IssueInput) (string, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return "", fmt.Errorf("oauth: workspace is required")
	}
	nonce, err := NewNonce()
	if err != nil {
		return "", fmt.Errorf("oauth: mint nonce: %w", err)
	}
	raw, err := EncodeState(OAuthState{
		WorkspaceID: in.WorkspaceID,
		UserID:      in.UserID,
		Nonce:       nonce,
		ExpiresAt:   time.Now().UTC().Add(TTL),
		ReturnPath:  SafeReturnPath(in.ReturnPath, i.defaultReturnPath),
		Popup:       in.Popup,
	}, i.secret)
	if err != nil {
		return "", err
	}
	if err := i.nonces.Issue(nonce, in.WorkspaceID); err != nil {
		return "", err
	}
	return raw, nil
}

func (i *Issuer) Redeem(raw string) (*OAuthState, error) {
	state, err := DecodeState(raw, i.secret)
	if err != nil {
		return nil, err
	}
	if err := i.nonces.Consume(state.Nonce); err != nil {
		return nil, err
	}
	return state, nil
}

func (i *Issuer) ReturnPath(state *OAuthState) string {
	if state == nil {
		return i.defaultReturnPath
	}
	return SafeReturnPath(state.ReturnPath, i.defaultReturnPath)
}

func (i *Issuer) DefaultReturnPath() string { return i.defaultReturnPath }
