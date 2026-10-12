package studiotools

import (
	"slices"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/shared"
)

var studioFamilyAliases = map[copilot.StudioKind]map[string][]string{
	copilot.StudioVideo: {
		"criar":            {"create", "add", "new", "adicionar", "inserir", "icon", "icone", "forma"},
		"linha-do-tempo":   {"timeline", "clip", "clipe", "edit", "edicao", "corte"},
		"animacao":         {"animation", "motion", "movimento"},
		"trajeto":          {"motion path", "anchor", "ancora", "trajetoria"},
		"chaves":           {"keyframe", "keyframes"},
		"velocidade":       {"rampa", "speed ramp"},
		"cor":              {"color", "colour", "grading", "correcao de cor"},
		"recortes":         {"mask", "mascara", "chroma", "keying"},
		"efeitos":          {"vfx", "visual effects", "efeitos visuais"},
		"traco":            {"line", "linha", "trim path"},
		"texto":            {"typography", "tipografia"},
		"faixas-e-projeto": {"track", "faixa", "project", "projeto", "audio"},
		"transicoes":       {"transition"},
		"layout":           {"alinhamento", "alinhar", "distribuir"},
		"pais":             {"parent", "parenting", "pai"},
		"comportamentos":   {"behavior", "behaviour"},
		"estilo-de-camada": {"layer style", "layer styles"},
		"rastreamento":     {"tracking", "tracker", "estabilizar"},
		"organizacao":      {"marker", "marcador", "beat", "batida"},
		"predefinicoes":    {"animation preset", "predefinicao"},
		"grupos":           {"group", "agrupar"},
		"caminhos":         {"path op", "boolean", "booleana"},
		"repeticao":        {"repeat", "repeater", "repetir"},
		"elementos":        {"element", "library", "biblioteca"},
		"graficos":         {"chart", "graph"},
		"tabelas":          {"table"},
		"qr":               {"qr code", "qrcode"},
	},
	copilot.StudioImage: {
		"criar":       {"create", "add", "new", "adicionar", "inserir", "icon", "icone", "line", "linha"},
		"camadas":     {"layer", "group", "agrupar", "mask", "mascara"},
		"cor":         {"color", "colour", "grading", "correcao de cor", "chroma"},
		"formas":      {"boolean", "booleana", "combinar formas"},
		"texto":       {"typography", "tipografia"},
		"repeticao":   {"repeat", "repeater", "repetir"},
		"componentes": {"component", "instance", "instancia"},
		"estilos":     {"styles", "shared styles", "estilos compartilhados"},
		"layout":      {"auto layout", "layout automatico"},
		"elementos":   {"element", "library", "biblioteca"},
		"graficos":    {"chart", "graph"},
		"tabelas":     {"table"},
		"qr":          {"qr code", "qrcode"},
		"pixels":      {"paint", "pintura", "pintar"},
		"efeitos":     {"vfx", "visual effects", "efeitos visuais"},
		"pranchetas":  {"artboard", "board"},
	},
}

var studioNameStopwords = map[string]bool{"de": true, "do": true, "da": true, "dos": true, "das": true, "e": true, "a": true, "o": true, "com": true, "em": true, "para": true, "of": true, "the": true, "and": true}

type studioNameCandidate struct {
	key    string
	family string
}

type studioOpNames struct {
	exact      map[string]string
	near       map[string]string
	order      []studioNameCandidate
	suggestive []studioNameCandidate
}

func studioNameKey(raw string) string {
	var b strings.Builder
	gap := false
	for _, r := range shared.FoldForMatch(raw) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if gap && b.Len() > 0 {
				b.WriteByte('-')
			}
			gap = false
			b.WriteRune(r)
			continue
		}
		gap = true
	}
	return b.String()
}

func numberVariants(key string) []string {
	head, word := "", key
	if i := strings.LastIndexByte(key, '-'); i >= 0 {
		head, word = key[:i+1], key[i+1:]
	}
	var forms []string
	switch {
	case strings.HasSuffix(word, "oes"):
		forms = append(forms, strings.TrimSuffix(word, "oes")+"ao")
	case strings.HasSuffix(word, "ao"):
		forms = append(forms, strings.TrimSuffix(word, "ao")+"oes")
	case strings.HasSuffix(word, "ies"):
		forms = append(forms, strings.TrimSuffix(word, "ies")+"y")
	case strings.HasSuffix(word, "y"):
		forms = append(forms, strings.TrimSuffix(word, "y")+"ies")
	}
	if strings.HasSuffix(word, "es") {
		forms = append(forms, strings.TrimSuffix(word, "es"))
	}
	if strings.HasSuffix(word, "s") {
		forms = append(forms, strings.TrimSuffix(word, "s"))
	} else {
		forms = append(forms, word+"s", word+"es")
	}
	variants := make([]string, 0, len(forms))
	for _, form := range forms {
		if form != "" {
			variants = append(variants, head+form)
		}
	}
	return variants
}

func newStudioOpNames(kind copilot.StudioKind, families []studioOpFamily, fields []string, core []string) studioOpNames {
	names := studioOpNames{exact: map[string]string{}, near: map[string]string{}}
	for _, family := range families {
		names.add(family.name, family.name, true)
	}
	for _, family := range families {
		for _, op := range family.ops {
			names.add(op, family.name, true)
		}
	}
	documented := fieldFamilies(families, fields)
	for _, field := range fields {
		if family := documented[field]; family != "" && !slices.Contains(core, field) {
			names.add(field, family, false)
		}
	}
	for _, family := range families {
		for _, alias := range studioFamilyAliases[kind][family.name] {
			names.add(alias, family.name, true)
		}
	}
	for _, candidate := range names.order {
		for _, variant := range numberVariants(candidate.key) {
			if _, taken := names.exact[variant]; taken {
				continue
			}
			if _, taken := names.near[variant]; !taken {
				names.near[variant] = candidate.family
			}
		}
	}
	return names
}

func fieldFamilies(families []studioOpFamily, order []string) map[string]string {
	owners := map[string]string{}
	mentions := map[string]int{}
	for _, family := range families {
		counts := map[string]int{}
		for _, word := range guideWord.FindAllString(family.guide, -1) {
			counts[word]++
		}
		for word, count := range counts {
			if count > mentions[word] {
				owners[word], mentions[word] = family.name, count
			}
		}
	}
	for _, family := range families {
		for _, field := range family.fields(order) {
			if _, taken := owners[field]; !taken {
				owners[field] = family.name
			}
		}
	}
	return owners
}

func (n *studioOpNames) add(raw, family string, suggestive bool) {
	key := studioNameKey(raw)
	if key == "" {
		return
	}
	if _, taken := n.exact[key]; taken {
		return
	}
	n.exact[key] = family
	n.order = append(n.order, studioNameCandidate{key: key, family: family})
	if suggestive {
		n.suggestive = append(n.suggestive, studioNameCandidate{key: key, family: family})
	}
}

func (n studioOpNames) lookup(key string) (string, bool) {
	if family, ok := n.exact[key]; ok {
		return family, true
	}
	family, ok := n.near[key]
	return family, ok
}

func (n studioOpNames) resolve(raw string) (string, bool) {
	if family, ok := n.lookup(studioNameKey(raw)); ok {
		return family, true
	}
	words := strings.Fields(shared.FoldForMatch(raw))
	if len(words) < 2 {
		return "", false
	}
	found := ""
	for _, word := range words {
		key := studioNameKey(word)
		if studioNameStopwords[key] {
			continue
		}
		family, ok := n.lookup(key)
		if !ok {
			continue
		}
		if found != "" && found != family {
			return "", false
		}
		found = family
	}
	return found, found != ""
}

func (n studioOpNames) closest(raw string) string {
	key := studioNameKey(raw)
	best, bestScore := "", -1
	for _, candidate := range n.suggestive {
		if score := shared.EditDistance(key, candidate.key); bestScore < 0 || score < bestScore {
			best, bestScore = candidate.family, score
		}
	}
	return best
}
