package cep

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidCEP          = errors.New("cep: must have 8 digits")
	ErrNotFound            = errors.New("cep: not found")
	ErrUnavailable         = errors.New("cep: lookup unavailable")
	ErrLookupNotConfigured = errors.New("cep: lookup not configured")
	ErrCacheNotConfigured  = errors.New("cep: cache not configured")
)

const (
	codeLength    = 8
	genericSuffix = "000"
)

const CityCodeRetryInterval = 24 * time.Hour

var maskSeparators = strings.NewReplacer(".", "", "-", "", " ", "")

type CEPInfo struct {
	Cep        string
	Logradouro string
	Complement string
	Bairro     string
	Localidade string
	Uf         string
	IBGE       string
	CheckedAt  time.Time
}

func (c CEPInfo) NeedsCityCode(now time.Time) bool {
	return c.IBGE == "" && !now.Before(c.CheckedAt.Add(CityCodeRetryInterval))
}

type Lookup interface {
	Lookup(ctx context.Context, code string) (*CEPInfo, error)
}

type CEPRepository interface {
	GetByCode(cepCode string) (*CEPInfo, error)
	Save(cep *CEPInfo) error
	MarkChecked(cepCode string) error
}

type CEPSearchUseCase interface {
	Execute(ctx context.Context, cep string) (*CEPInfo, error)
}

func Parse(raw string) (string, error) {
	code := maskSeparators.Replace(raw)
	if len(code) != codeLength || !onlyDigits(code) {
		return "", ErrInvalidCEP
	}
	return code, nil
}

func Format(code string) string {
	if len(code) != codeLength {
		return code
	}
	return code[:5] + "-" + code[5:]
}

func IsGeneric(raw string) bool {
	code, err := Parse(raw)
	return err == nil && strings.HasSuffix(code, genericSuffix)
}

func onlyDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
