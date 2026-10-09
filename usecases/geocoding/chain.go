package geocoding_usecase

import (
	"context"
	"errors"
	"log"
	"time"

	"vozko/domain/address"
	"vozko/domain/geo"
	"vozko/domain/geocoding"
)

var errChainIncomplete = errors.New("geocoding chain: the reference is missing")

type ChainDeps struct {
	Reference geo.Reference
	Now       func() time.Time
}

type Chain struct {
	deps ChainDeps
	now  func() time.Time
}

type BatchResult struct {
	Settlements []geocoding.Settlement
	Release     []string
}

func NewChain(deps ChainDeps) (*Chain, error) {
	if deps.Reference == nil {
		return nil, errChainIncomplete
	}
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Chain{deps: deps, now: now}, nil
}

type batch struct {
	chain  *Chain
	claims []geocoding.Claim
	down   []geo.UnavailableReason
	index  geo.ReferenceIndex
}

func (c *Chain) Resolve(ctx context.Context, claims []geocoding.Claim) BatchResult {
	at := c.now()
	b := &batch{chain: c, claims: claims, down: make([]geo.UnavailableReason, len(claims))}
	b.readReference(ctx)
	var out BatchResult
	for i, claim := range claims {
		var candidates []geo.Fix
		if b.down[i] == "" && !geocoding.StaleClaim(claim.Address, claim.Fingerprint) {
			candidates = b.index.Candidates(claim.Address.Postal, at)
		}
		resolution, err := geocoding.Resolve(geocoding.Attempt{
			Address: claim.Address, Fingerprint: claim.Fingerprint, Attempts: claim.Attempts,
			ReferenceDown: b.down[i], Candidates: candidates, Now: at,
		})
		if err != nil {
			log.Printf("[geocoding] address %s of workspace %s left for a later sweep: %v", claim.AddressID, claim.WorkspaceID, err)
			out.Release = append(out.Release, claim.AddressID)
			continue
		}
		out.Settlements = append(out.Settlements, geocoding.Settlement{Claim: claim, Resolution: resolution})
	}
	return out
}

func (b *batch) readReference(ctx context.Context) {
	coverage, err := b.chain.deps.Reference.Coverage(ctx)
	if err != nil {
		log.Printf("[geocoding] reference coverage unreadable: %v", err)
		b.markDown(geo.ReasonReferenceDown)
		return
	}
	var covered []address.Postal
	for i, claim := range b.claims {
		if coverage.Covers(claim.Address.Postal) {
			covered = append(covered, claim.Address.Postal)
			continue
		}
		b.down[i] = geo.ReasonReferenceNotLoaded
	}
	if len(covered) == 0 {
		return
	}
	index, err := b.chain.deps.Reference.Lookup(ctx, covered)
	if err != nil {
		log.Printf("[geocoding] reference lookup failed: %v", err)
		b.markDown(geo.ReasonReferenceDown)
		return
	}
	b.index = index
}

func (b *batch) markDown(reason geo.UnavailableReason) {
	for i := range b.down {
		if b.down[i] == "" {
			b.down[i] = reason
		}
	}
}
