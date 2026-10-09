package cep_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/cep"
	"vozko/domain/georef"
)

const refreshTimeout = 2 * time.Second

type searchCEPUseCase struct {
	cache     cep.CEPRepository
	lookup    cep.Lookup
	reference georef.CEPStreets
	now       func() time.Time
}

func NewSearchCEPUseCase(cache cep.CEPRepository, lookup cep.Lookup) cep.CEPSearchUseCase {
	return &searchCEPUseCase{cache: cache, lookup: lookup, now: time.Now}
}

func NewSearchCEPUseCaseWithReference(cache cep.CEPRepository, lookup cep.Lookup, reference georef.CEPStreets) cep.CEPSearchUseCase {
	return &searchCEPUseCase{cache: cache, lookup: lookup, reference: reference, now: time.Now}
}

func (uc *searchCEPUseCase) Execute(ctx context.Context, raw string) (*cep.CEPInfo, error) {
	code, err := cep.Parse(raw)
	if err != nil {
		return nil, err
	}
	if uc.cache == nil {
		return nil, cep.ErrCacheNotConfigured
	}
	cached, err := uc.cache.GetByCode(code)
	if err != nil {
		return nil, fmt.Errorf("read cached CEP: %w", err)
	}
	if cached == nil {
		return uc.lookupAndSave(ctx, code)
	}
	if !cached.NeedsCityCode(uc.now()) {
		return cached, nil
	}
	return uc.refresh(ctx, cached)
}

func (uc *searchCEPUseCase) lookupAndSave(ctx context.Context, code string) (*cep.CEPInfo, error) {
	fresh, err := uc.fetch(ctx, code)
	if errors.Is(err, cep.ErrUnavailable) {
		known, refErr := uc.fromReference(ctx, code)
		if refErr != nil {
			return nil, fmt.Errorf("%w (reference fallback: %v)", err, refErr)
		}
		if known != nil {
			return known, nil
		}
	}
	if err != nil {
		return nil, err
	}
	uc.save(fresh)
	return fresh, nil
}

func (uc *searchCEPUseCase) fromReference(ctx context.Context, code string) (*cep.CEPInfo, error) {
	if uc.reference == nil {
		return nil, nil
	}
	rows, err := uc.reference.CEPStreets(ctx, code)
	if err != nil {
		return nil, err
	}
	info, ok := georef.CEPInfoFromStreets(code, rows)
	if !ok {
		return nil, nil
	}
	return &info, nil
}

func (uc *searchCEPUseCase) refresh(ctx context.Context, cached *cep.CEPInfo) (*cep.CEPInfo, error) {
	bounded, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()
	fresh, err := uc.fetch(bounded, cached.Cep)
	if err != nil {
		if markErr := uc.cache.MarkChecked(cached.Cep); markErr != nil {
			log.Printf("[cep] recording the refresh of %s failed: %v", cached.Cep, markErr)
		}
		return cached, nil
	}
	uc.save(fresh)
	return fresh, nil
}

func (uc *searchCEPUseCase) save(info *cep.CEPInfo) {
	if err := uc.cache.Save(info); err != nil {
		log.Printf("[cep] caching %s failed: %v", info.Cep, err)
	}
}

func (uc *searchCEPUseCase) fetch(ctx context.Context, code string) (*cep.CEPInfo, error) {
	if uc.lookup == nil {
		return nil, cep.ErrLookupNotConfigured
	}
	info, err := uc.lookup.Lookup(ctx, code)
	if err != nil {
		return nil, err
	}
	info.Cep = code
	return info, nil
}
