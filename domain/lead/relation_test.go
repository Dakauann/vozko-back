package lead

import (
	"errors"
	"testing"
)

func TestRelationKindInverse(t *testing.T) {
	cases := []struct {
		kind, inverse RelationKind
		dimension     RelationDimension
	}{
		{KindSpouse, KindSpouse, DimensionFamily},
		{KindPartner, KindPartner, DimensionFamily},
		{KindParent, KindChild, DimensionFamily},
		{KindChild, KindParent, DimensionFamily},
		{KindSibling, KindSibling, DimensionFamily},
		{KindGrandparent, KindGrandchild, DimensionFamily},
		{KindGrandchild, KindGrandparent, DimensionFamily},
		{KindUncleAunt, KindNephewNiece, DimensionFamily},
		{KindNephewNiece, KindUncleAunt, DimensionFamily},
		{KindCousin, KindCousin, DimensionFamily},
		{KindInLaw, KindInLaw, DimensionFamily},
		{KindRelative, KindRelative, DimensionFamily},
		{KindReferred, KindReferredBy, DimensionReferral},
		{KindReferredBy, KindReferred, DimensionReferral},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			if !tc.kind.Valid() {
				t.Fatalf("%s must be valid", tc.kind)
			}
			if got := tc.kind.Inverse(); got != tc.inverse {
				t.Fatalf("Inverse(%s) = %s, want %s", tc.kind, got, tc.inverse)
			}
			if got := tc.kind.Inverse().Inverse(); got != tc.kind {
				t.Fatalf("the inverse of the inverse of %s is %s", tc.kind, got)
			}
			if got := tc.kind.Dimension(); got != tc.dimension {
				t.Fatalf("Dimension(%s) = %s, want %s", tc.kind, got, tc.dimension)
			}
		})
	}
	for _, unknown := range []RelationKind{"", "friend", "PARENT"} {
		if unknown.Valid() || unknown.Dimension() != "" || unknown.Inverse() != "" {
			t.Fatalf("%q must not be a relation kind", unknown)
		}
	}
}

func TestNewRelationIsStoredInOneCanonicalDirection(t *testing.T) {
	cases := []struct {
		name               string
		lead, other        string
		kind               RelationKind
		holder, held       string
		stored             RelationKind
		seenByLead, byHeld RelationKind
	}{
		{"maria's child is held by maria", "maria", "joao", KindChild, "maria", "joao", KindChild, KindChild, KindParent},
		{"joao's parent is held by the parent", "joao", "maria", KindParent, "maria", "joao", KindChild, KindParent, KindParent},
		{"a grandparent holds the grandchild", "neto", "avo", KindGrandparent, "avo", "neto", KindGrandchild, KindGrandparent, KindGrandparent},
		{"an uncle holds the nephew", "tio", "sobrinho", KindNephewNiece, "tio", "sobrinho", KindNephewNiece, KindNephewNiece, KindUncleAunt},
		{"the referrer holds the referral", "lider", "contato", KindReferred, "lider", "contato", KindReferred, KindReferred, KindReferredBy},
		{"a lead referred by someone is held by the referrer", "contato", "lider", KindReferredBy, "lider", "contato", KindReferred, KindReferredBy, KindReferredBy},
		{"a symmetric kind keeps the direction it was given", "ana", "bia", KindSibling, "ana", "bia", KindSibling, KindSibling, KindSibling},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewRelation(tc.lead, tc.other, tc.kind, "u-1")
			if err != nil {
				t.Fatalf("NewRelation: %v", err)
			}
			if r.LeadID != tc.holder || r.OtherLeadID != tc.held || r.Kind != tc.stored {
				t.Fatalf("stored as %s -%s-> %s, want %s -%s-> %s", r.LeadID, r.Kind, r.OtherLeadID, tc.holder, tc.stored, tc.held)
			}
			if !r.Kind.Canonical() {
				t.Fatalf("%s is not a stored kind", r.Kind)
			}
			if got := r.KindFor(tc.lead); got != tc.seenByLead {
				t.Fatalf("seen by %s = %s, want %s", tc.lead, got, tc.seenByLead)
			}
			if r.Other(tc.lead) != tc.other || r.Other(tc.other) != tc.lead {
				t.Fatalf("Other = %s / %s", r.Other(tc.lead), r.Other(tc.other))
			}
			if got := r.KindFor(tc.held); tc.held != tc.lead && got != tc.byHeld {
				t.Fatalf("seen by %s = %s, want %s", tc.held, got, tc.byHeld)
			}
		})
	}
}

func TestNewRelationRefusals(t *testing.T) {
	cases := []struct {
		name        string
		lead, other string
		kind        RelationKind
		want        error
	}{
		{"no self relation", "maria", "maria", KindSibling, ErrRelationSelf},
		{"an unknown kind", "maria", "joao", "friend", ErrRelationKindInvalid},
		{"a missing lead", "", "joao", KindSibling, ErrLeadRequired},
		{"a missing relative", "maria", " ", KindSibling, ErrLeadRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewRelation(tc.lead, tc.other, tc.kind, "u-1"); !errors.Is(err, tc.want) {
				t.Fatalf("NewRelation = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRelateKeepsOneRelationPerPairPerDimension(t *testing.T) {
	maria := &Lead{ID: "maria", WorkspaceID: "ws-1", Name: "Maria"}
	joao := &Lead{ID: "joao", WorkspaceID: "ws-1", Name: "João"}

	sibling, err := maria.Relate(joao, KindSibling, "u-1")
	if err != nil {
		t.Fatalf("Relate sibling: %v", err)
	}
	if _, err := maria.Relate(joao, KindReferredBy, "u-1"); err != nil {
		t.Fatalf("a sibling who also referred the lead is a second relation: %v", err)
	}
	if _, err := maria.Relate(joao, KindCousin, "u-1"); !errors.Is(err, ErrRelationExists) {
		t.Fatalf("a second family relation for the same pair = %v, want %v", err, ErrRelationExists)
	}
	if _, err := maria.Relate(joao, KindReferred, "u-1"); !errors.Is(err, ErrRelationExists) {
		t.Fatalf("a second referral for the same pair, in the other direction = %v, want %v", err, ErrRelationExists)
	}
	if len(maria.Relations) != 2 || maria.RelativesCount != 1 || maria.ReferredCount != 0 {
		t.Fatalf("relations = %+v relatives = %d referred = %d", maria.Relations, maria.RelativesCount, maria.ReferredCount)
	}
	if sibling.ID != "" || sibling.LeadID != "maria" || sibling.CreatedBy != "u-1" {
		t.Fatalf("relation = %+v", sibling)
	}
}

func TestRelationBetweenStampsWhoLinkedThePair(t *testing.T) {
	joao := &Lead{ID: "joao", WorkspaceID: "ws-1", Name: "João"}
	maria := &Lead{ID: "maria", WorkspaceID: "ws-1", Name: "Maria"}
	r, err := RelationBetween(joao, maria, KindParent, "u-7")
	if err != nil {
		t.Fatalf("RelationBetween: %v", err)
	}
	if r.LeadID != "maria" || r.OtherLeadID != "joao" || r.Kind != KindChild || r.CreatedBy != "u-7" {
		t.Fatalf("relation = %+v", r)
	}
	if len(joao.Relations) != 0 || len(maria.Relations) != 0 {
		t.Fatal("RelationBetween builds the relation without touching either lead")
	}
	cases := []struct {
		name         string
		subject, rel *Lead
		kind         RelationKind
		want         error
	}{
		{"no relative", joao, nil, KindSibling, ErrLeadRequired},
		{"no subject", nil, maria, KindSibling, ErrLeadRequired},
		{"another workspace", joao, &Lead{ID: "x", WorkspaceID: "ws-2"}, KindSibling, ErrRelationOtherWorkspace},
		{"self", joao, joao, KindSibling, ErrRelationSelf},
		{"unknown kind", joao, maria, "friend", ErrRelationKindInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := RelationBetween(tc.subject, tc.rel, tc.kind, "u-7"); !errors.Is(err, tc.want) {
				t.Fatalf("RelationBetween = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRelateRefusesAnotherWorkspaceAndSelf(t *testing.T) {
	maria := &Lead{ID: "maria", WorkspaceID: "ws-1", Name: "Maria"}
	if _, err := maria.Relate(&Lead{ID: "x", WorkspaceID: "ws-2"}, KindSibling, "u-1"); !errors.Is(err, ErrRelationOtherWorkspace) {
		t.Fatalf("another workspace = %v", err)
	}
	if _, err := maria.Relate(maria, KindSibling, "u-1"); !errors.Is(err, ErrRelationSelf) {
		t.Fatalf("self = %v", err)
	}
	if _, err := maria.Relate(nil, KindSibling, "u-1"); !errors.Is(err, ErrLeadRequired) {
		t.Fatalf("nil relative = %v", err)
	}
	if len(maria.Relations) != 0 {
		t.Fatalf("a refused relation must not be added, got %+v", maria.Relations)
	}
}

func TestCountRelations(t *testing.T) {
	relations := []Relation{
		{LeadID: "x", OtherLeadID: "a", Kind: KindReferred},
		{LeadID: "x", OtherLeadID: "b", Kind: KindReferred},
		{LeadID: "c", OtherLeadID: "x", Kind: KindReferred},
		{LeadID: "x", OtherLeadID: "d", Kind: KindChild},
		{LeadID: "e", OtherLeadID: "x", Kind: KindChild},
		{LeadID: "f", OtherLeadID: "g", Kind: KindSibling},
	}
	got := CountRelations("x", relations)
	if got != (RelationCounts{Relatives: 2, Referred: 2}) {
		t.Fatalf("CountRelations = %+v, want 2 relatives and 2 referred", got)
	}
	if c := relations[2].CountsFor("x"); c != (RelationCounts{}) {
		t.Fatalf("being referred does not count as referring, got %+v", c)
	}
	if c := relations[2].CountsFor("c"); c != (RelationCounts{Referred: 1}) {
		t.Fatalf("the referrer counts the referral, got %+v", c)
	}
	if c := relations[4].CountsFor("e"); c != (RelationCounts{Relatives: 1}) {
		t.Fatalf("both sides of a family relation count it, got %+v", c)
	}
}

func TestRelationWith(t *testing.T) {
	l := &Lead{ID: "maria", Relations: []Relation{
		{ID: "r-1", LeadID: "joao", OtherLeadID: "maria", Kind: KindReferred},
		{ID: "r-2", LeadID: "maria", OtherLeadID: "joao", Kind: KindSibling},
	}}
	if r, ok := l.RelationWith("joao", DimensionFamily); !ok || r.ID != "r-2" {
		t.Fatalf("family relation = %+v, %v", r, ok)
	}
	if r, ok := l.RelationWith("joao", DimensionReferral); !ok || r.ID != "r-1" {
		t.Fatalf("referral = %+v, %v", r, ok)
	}
	if _, ok := l.RelationWith("ana", DimensionFamily); ok {
		t.Fatal("no relation with a stranger")
	}
}
