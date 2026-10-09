package copilottools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/address"
	"vozko/domain/copilot"
	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/leadarea"
	"vozko/domain/shared"
)

const (
	ownerMe        = "me"
	ownerNone      = "none"
	areaNamesShown = 10
)

var eloFieldViewer = customfield.Viewer{}

type leadFilterArgs struct {
	Query           string   `json:"query" desc:"nome, número ou trecho das memórias do contato"`
	HasMemory       bool     `json:"has_memory" desc:"só contatos com memórias registradas"`
	Cidade          string   `json:"cidade" desc:"cidade: o city_key de lead_geo_summary (ex.: sp:campinas) ou o nome com a UF (ex.: Campinas/SP)"`
	UF              string   `json:"uf" desc:"UF com duas letras: sozinha filtra o estado inteiro; com cidade, completa o nome da cidade"`
	Bairros         []string `json:"bairros" desc:"bairros: o pair de lead_geo_summary (ex.: sp:campinas/centro) ou o nome do bairro junto com cidade"`
	Area            string   `json:"area" desc:"nome ou id de uma área desenhada no mapa; exige ver endereços completos"`
	OwnerID         string   `json:"owner_id" desc:"responsável: member_id de list_assignable_members, me para o próprio usuário ou none para leads sem responsável"`
	UseScreenFilter bool     `json:"use_screen_filter" desc:"parte do filtro da tela de Leads; só quando o usuário está nela"`
}

func leadFilterOf(ctx context.Context, cc copilot.Context, deps LeadDeps, a leadFilterArgs) (crmfilter.Filter, error) {
	var groups []crmfilter.Group
	if a.UseScreenFilter {
		screen, ok := cc.View.ScreenLeadFilter()
		if !ok {
			return crmfilter.Filter{}, fmt.Errorf("%w: use_screen_filter só funciona com o usuário na tela de Leads", errInvalidArgs)
		}
		groups = append(groups, screen.Groups...)
	}
	if q := strings.TrimSpace(a.Query); q != "" {
		groups = append(groups, predicate(crmfilter.FieldQuery, crmfilter.OpContains, q))
	}
	if a.HasMemory {
		groups = append(groups, predicate(crmfilter.FieldMemoryCategory, crmfilter.OpIsSet))
	}
	places, err := placeGroups(a)
	if err != nil {
		return crmfilter.Filter{}, err
	}
	groups = append(groups, places...)
	if area := strings.TrimSpace(a.Area); area != "" {
		id, err := areaID(ctx, cc, deps, area)
		if err != nil {
			return crmfilter.Filter{}, err
		}
		groups = append(groups, predicate(crmfilter.FieldArea, crmfilter.OpIn, id))
	}
	if owner := strings.TrimSpace(a.OwnerID); owner != "" {
		groups = append(groups, ownerGroup(cc, owner))
	}
	filter := crmfilter.Filter{Groups: groups}
	if err := lead.ValidateFilter(filter); err != nil {
		return crmfilter.Filter{}, fmt.Errorf("%w: filtro de leads inválido (%v); confira owner_id, cidade e bairros", errInvalidArgs, err)
	}
	if err := refuseSensitiveFilter(cc, deps, filter); err != nil {
		return crmfilter.Filter{}, err
	}
	return filter, nil
}

func (a leadFilterArgs) given() bool {
	return strings.TrimSpace(a.Query) != "" || a.HasMemory || strings.TrimSpace(a.Cidade) != "" || strings.TrimSpace(a.UF) != "" ||
		len(shared.DistinctTrimmed(a.Bairros)) > 0 || strings.TrimSpace(a.Area) != "" || strings.TrimSpace(a.OwnerID) != "" || a.UseScreenFilter
}

func placeGroups(a leadFilterArgs) ([]crmfilter.Group, error) {
	city, state := strings.TrimSpace(a.Cidade), strings.TrimSpace(a.UF)
	cityKey := ""
	if city != "" {
		key, ok := address.CityKeyFromText(city, state)
		if !ok {
			return nil, fmt.Errorf("%w: cidade %q não reconhecida; use o city_key de lead_geo_summary ou o nome com a UF (ex.: Campinas/SP)", errInvalidArgs, city)
		}
		cityKey = key
	}
	bairros := shared.DistinctTrimmed(a.Bairros)
	if len(bairros) > crmfilter.MaxDistrictPairs {
		return nil, fmt.Errorf("%w: no máximo %d bairros por busca", errInvalidArgs, crmfilter.MaxDistrictPairs)
	}
	var groups []crmfilter.Group
	switch {
	case len(bairros) > 0:
		pairs := make([]string, 0, len(bairros))
		for _, bairro := range bairros {
			pair, err := districtPair(bairro, cityKey)
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, pair)
		}
		groups = append(groups, predicate(crmfilter.FieldDistrict, crmfilter.OpIn, pairs...))
	case cityKey != "":
		groups = append(groups, predicate(crmfilter.FieldCity, crmfilter.OpIn, cityKey))
	}
	if state == "" || cityKey != "" {
		return groups, nil
	}
	code, ok := address.StateCode(state)
	if !ok {
		return nil, fmt.Errorf("%w: uf %q não reconhecida; use a sigla com duas letras (ex.: SP)", errInvalidArgs, state)
	}
	return append(groups, predicate(crmfilter.FieldState, crmfilter.OpIn, code)), nil
}

func districtPair(bairro, cityKey string) (string, error) {
	if strings.Contains(bairro, ":") {
		city, district, err := crmfilter.ParseDistrictPair(bairro)
		if err != nil {
			return "", fmt.Errorf("%w: bairro %q não reconhecido; use o pair de lead_geo_summary", errInvalidArgs, bairro)
		}
		return crmfilter.DistrictPair(city, district), nil
	}
	if cityKey == "" {
		return "", fmt.Errorf("%w: o bairro %q precisa da cidade; passe cidade com a UF ou o pair de lead_geo_summary", errInvalidArgs, bairro)
	}
	district := address.DistrictKey(bairro)
	if district == "" {
		return "", fmt.Errorf("%w: bairro %q não reconhecido", errInvalidArgs, bairro)
	}
	return crmfilter.DistrictPair(cityKey, district), nil
}

func ownerGroup(cc copilot.Context, owner string) crmfilter.Group {
	switch strings.ToLower(owner) {
	case ownerMe:
		return predicate(crmfilter.FieldOwner, crmfilter.OpIn, cc.UserID)
	case ownerNone:
		return predicate(crmfilter.FieldOwner, crmfilter.OpIsEmpty)
	}
	return predicate(crmfilter.FieldOwner, crmfilter.OpIn, owner)
}

func areaID(ctx context.Context, cc copilot.Context, deps LeadDeps, raw string) (string, error) {
	if parsed, err := uuid.Parse(raw); err == nil {
		return parsed.String(), nil
	}
	if deps.Areas == nil {
		return "", errLeadToolUnavailable
	}
	areas, err := deps.Areas.List(ctx, viewerOf(cc))
	if errors.Is(err, leadarea.ErrAddressesRequired) {
		return "", fmt.Errorf("%w: filtrar por área exige permissão para ver endereços completos", errInvalidArgs)
	}
	if err != nil {
		return "", err
	}
	wanted := shared.FoldForMatch(raw)
	var found []leadarea.Area
	names := make([]string, 0, min(len(areas), areaNamesShown))
	for _, area := range areas {
		if shared.FoldForMatch(area.Name) == wanted {
			found = append(found, area)
		}
		if len(names) < areaNamesShown {
			names = append(names, area.Name)
		}
	}
	switch len(found) {
	case 1:
		return found[0].ID, nil
	case 0:
		if len(names) == 0 {
			return "", fmt.Errorf("%w: não há áreas desenhadas que o usuário possa ver", errInvalidArgs)
		}
		return "", fmt.Errorf("%w: área %q não encontrada; áreas disponíveis: %s", errInvalidArgs, raw, strings.Join(names, ", "))
	}
	return "", fmt.Errorf("%w: há mais de uma área chamada %q; peça ao usuário para escolher no mapa", errInvalidArgs, raw)
}

func refuseSensitiveFilter(cc copilot.Context, deps LeadDeps, filter crmfilter.Filter) error {
	if !filter.UsesField(crmfilter.FieldCustom) {
		return nil
	}
	if deps.Definitions == nil {
		return errLeadToolUnavailable
	}
	defs, err := deps.Definitions.ListByObject(cc.WorkspaceID, customfield.ObjectLead)
	if err != nil {
		return err
	}
	_, err = customfield.BindFilter(filter, defs, eloFieldViewer)
	if errors.Is(err, customfield.ErrFilterSensitive) {
		return fmt.Errorf("%w: o filtro usa um campo sensível, e campos sensíveis nunca passam pela Elo; peça ao usuário para agir pela tela de Leads", errInvalidArgs)
	}
	if err != nil {
		return fmt.Errorf("%w: filtro de campo personalizado inválido (%v)", errInvalidArgs, err)
	}
	return nil
}
