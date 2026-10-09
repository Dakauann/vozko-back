package lead

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrRelationKindInvalid    = errors.New("lead: the relation kind is not a known one")
	ErrRelationSelf           = errors.New("lead: a lead cannot be related to itself")
	ErrRelationOtherWorkspace = errors.New("lead: a relative must be in the same workspace")
	ErrRelationExists         = errors.New("lead: these two leads are already related this way")
	ErrRelationNotFound       = errors.New("lead: relation not found")
	ErrRelativeNotFound       = errors.New("lead: the relative was not found in this workspace")
)

type RelationDimension string

const (
	DimensionFamily   RelationDimension = "family"
	DimensionReferral RelationDimension = "referral"
)

type RelationKind string

const (
	KindSpouse      RelationKind = "spouse"
	KindPartner     RelationKind = "partner"
	KindParent      RelationKind = "parent"
	KindChild       RelationKind = "child"
	KindSibling     RelationKind = "sibling"
	KindGrandparent RelationKind = "grandparent"
	KindGrandchild  RelationKind = "grandchild"
	KindUncleAunt   RelationKind = "uncle_aunt"
	KindNephewNiece RelationKind = "nephew_niece"
	KindCousin      RelationKind = "cousin"
	KindInLaw       RelationKind = "in_law"
	KindRelative    RelationKind = "relative"
	KindReferred    RelationKind = "referred"
	KindReferredBy  RelationKind = "referred_by"
)

type kindRule struct {
	dimension RelationDimension
	inverse   RelationKind
	canonical bool
}

var kindRules = map[RelationKind]kindRule{
	KindSpouse:      {DimensionFamily, KindSpouse, true},
	KindPartner:     {DimensionFamily, KindPartner, true},
	KindParent:      {DimensionFamily, KindChild, false},
	KindChild:       {DimensionFamily, KindParent, true},
	KindSibling:     {DimensionFamily, KindSibling, true},
	KindGrandparent: {DimensionFamily, KindGrandchild, false},
	KindGrandchild:  {DimensionFamily, KindGrandparent, true},
	KindUncleAunt:   {DimensionFamily, KindNephewNiece, false},
	KindNephewNiece: {DimensionFamily, KindUncleAunt, true},
	KindCousin:      {DimensionFamily, KindCousin, true},
	KindInLaw:       {DimensionFamily, KindInLaw, true},
	KindRelative:    {DimensionFamily, KindRelative, true},
	KindReferred:    {DimensionReferral, KindReferredBy, true},
	KindReferredBy:  {DimensionReferral, KindReferred, false},
}

func (k RelationKind) Valid() bool {
	_, ok := kindRules[k]
	return ok
}

func (k RelationKind) Dimension() RelationDimension {
	return kindRules[k].dimension
}

func (k RelationKind) Inverse() RelationKind {
	return kindRules[k].inverse
}

func (k RelationKind) Canonical() bool {
	return kindRules[k].canonical
}

type Relation struct {
	ID          string       `json:"id,omitempty"`
	LeadID      string       `json:"leadId"`
	OtherLeadID string       `json:"otherLeadId"`
	Kind        RelationKind `json:"kind"`
	CreatedBy   string       `json:"createdBy,omitempty"`
	CreatedAt   time.Time    `json:"createdAt"`
}

func NewRelation(leadID, otherLeadID string, kind RelationKind, createdBy string) (Relation, error) {
	leadID, otherLeadID = strings.TrimSpace(leadID), strings.TrimSpace(otherLeadID)
	if leadID == "" || otherLeadID == "" {
		return Relation{}, ErrLeadRequired
	}
	if !kind.Valid() {
		return Relation{}, ErrRelationKindInvalid
	}
	if leadID == otherLeadID {
		return Relation{}, ErrRelationSelf
	}
	if kind.Canonical() {
		return Relation{LeadID: leadID, OtherLeadID: otherLeadID, Kind: kind, CreatedBy: createdBy}, nil
	}
	return Relation{LeadID: otherLeadID, OtherLeadID: leadID, Kind: kind.Inverse(), CreatedBy: createdBy}, nil
}

func (r Relation) Dimension() RelationDimension {
	return r.Kind.Dimension()
}

func (r Relation) Involves(leadID string) bool {
	return r.LeadID == leadID || r.OtherLeadID == leadID
}

func (r Relation) Other(of string) string {
	if r.LeadID == of {
		return r.OtherLeadID
	}
	return r.LeadID
}

func (r Relation) KindFor(viewer string) RelationKind {
	if r.LeadID == viewer {
		return r.Kind
	}
	return r.Kind.Inverse()
}

func (r Relation) samePairAndDimension(o Relation) bool {
	return r.Dimension() == o.Dimension() &&
		((r.LeadID == o.LeadID && r.OtherLeadID == o.OtherLeadID) || (r.LeadID == o.OtherLeadID && r.OtherLeadID == o.LeadID))
}

type RelationCounts struct {
	Relatives int
	Referred  int
}

func (c RelationCounts) Plus(o RelationCounts) RelationCounts {
	return RelationCounts{Relatives: c.Relatives + o.Relatives, Referred: c.Referred + o.Referred}
}

func (c RelationCounts) Minus(o RelationCounts) RelationCounts {
	return RelationCounts{Relatives: c.Relatives - o.Relatives, Referred: c.Referred - o.Referred}
}

func (r Relation) CountsFor(leadID string) RelationCounts {
	switch {
	case !r.Involves(leadID):
		return RelationCounts{}
	case r.Dimension() == DimensionFamily:
		return RelationCounts{Relatives: 1}
	case r.Dimension() == DimensionReferral && r.LeadID == leadID:
		return RelationCounts{Referred: 1}
	}
	return RelationCounts{}
}

func CountRelations(leadID string, relations []Relation) RelationCounts {
	var counts RelationCounts
	for _, r := range relations {
		counts = counts.Plus(r.CountsFor(leadID))
	}
	return counts
}

func RelationBetween(subject, other *Lead, kind RelationKind, actorID string) (Relation, error) {
	if subject == nil || other == nil {
		return Relation{}, ErrLeadRequired
	}
	if other.WorkspaceID != subject.WorkspaceID {
		return Relation{}, ErrRelationOtherWorkspace
	}
	return NewRelation(subject.ID, other.ID, kind, actorID)
}

func (l *Lead) Relate(other *Lead, kind RelationKind, actorID string) (Relation, error) {
	r, err := RelationBetween(l, other, kind, actorID)
	if err != nil {
		return Relation{}, err
	}
	for _, existing := range l.Relations {
		if existing.samePairAndDimension(r) {
			return Relation{}, ErrRelationExists
		}
	}
	relations := make([]Relation, 0, len(l.Relations)+1)
	l.Relations = append(append(relations, l.Relations...), r)
	l.recount()
	return r, nil
}

func (l *Lead) recount() {
	counts := CountRelations(l.ID, l.Relations)
	l.RelativesCount, l.ReferredCount = counts.Relatives, counts.Referred
}

func (l *Lead) RelationWith(otherID string, dimension RelationDimension) (Relation, bool) {
	for _, r := range l.Relations {
		if r.Dimension() == dimension && r.Involves(otherID) && r.Other(otherID) == l.ID {
			return r, true
		}
	}
	return Relation{}, false
}
