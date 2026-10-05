package workspace_config

import (
	"errors"
	"strings"
)

type MetaPayer string

const (
	MetaPayerVozko  MetaPayer = "vozko"
	MetaPayerClient MetaPayer = "client"
)

var ErrInvalidMetaPayer = errors.New("who pays Meta must be vozko or client")

func ParseMetaPayer(raw string) (MetaPayer, error) {
	switch payer := MetaPayer(strings.ToLower(strings.TrimSpace(raw))); payer {
	case MetaPayerVozko, MetaPayerClient:
		return payer, nil
	}
	return "", ErrInvalidMetaPayer
}

func (c *WorkspaceConfig) EffectiveMetaPayer() MetaPayer {
	if c.MetaPayer == MetaPayerClient {
		return MetaPayerClient
	}
	return MetaPayerVozko
}
