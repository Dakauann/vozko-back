package studiotools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/studio/library"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
)

const (
	defaultLibraryResults = 20
	librarySuggestions    = 5
	studioLibraryBroken   = "a biblioteca de elementos não pôde ser lida; tente de novo mais tarde"
)

type studioLibrarySearchArgs struct {
	Query    string `json:"query,omitempty" desc:"palavras para buscar no nome e nas palavras-chave dos itens, no idioma da conversa ou em inglês, sem precisar de acento (coração, heart, seta dupla); vazio lista os itens da categoria, ou as categorias quando não há category"`
	Category string `json:"category,omitempty" desc:"limita a uma categoria da biblioteca"`
	Editor   string `json:"editor,omitempty" enum:"image,video" desc:"editor em que o item vai entrar; padrão o do projeto aberto"`
	Limit    int    `json:"limit,omitempty" desc:"quantos itens devolver, padrão 20; total diz quantos combinaram"`
}

type studioElementText struct {
	Slot string `json:"slot" req:"true" desc:"o slot de texto do item (slots em studio_library_search)"`
	Text string `json:"text" req:"true" desc:"o texto que entra no lugar do exemplo"`
}

type studioElementColor struct {
	From string `json:"from" req:"true" desc:"uma cor do item, como em colors de studio_library_search"`
	To   string `json:"to" req:"true" desc:"a cor nova #rrggbb ou #rrggbbaa"`
}

type studioElementArgs struct {
	ItemID string               `json:"item_id,omitempty" desc:"add_element: o item da biblioteca, o item_id exato de studio_library_search"`
	Texts  []studioElementText  `json:"texts,omitempty" desc:"add_element: o texto de cada slot; os slots omitidos ficam com o texto de exemplo"`
	Colors []studioElementColor `json:"colors,omitempty" desc:"add_element: troca as cores do item por outras já na criação"`
}

const studioElementGuide = "Biblioteca de elementos (a mesma dos painéis Texto e Elementos): procure com studio_library_search antes de desenhar à mão e insira com add_element(item_id, x?, y?, w?, h?, texts?, colors?, at_ms?, duration_ms?, track_id?, clip_ids?): o item entra como camadas ou clipes comuns, editáveis como qualquer outro; x e y põem o centro do item, w ou h mudam o tamanho mantendo a proporção (os dois juntos esticam um item de uma peça só), texts troca o texto de cada slot e colors troca cada cor from pela cor to; a resposta traz os ids criados em created. " +
	"Pelo kind: frame e grid na imagem viram molduras vazias (uma grade ocupa a prancheta inteira, em layout automático), e as notas da resposta trazem os ids das molduras para fill_frame; no vídeo, frame recorta os clipes de vídeo ou imagem de clip_ids no formato e grid divide a tela entre eles na ordem de clip_ids, sem criar clipes. sticker é uma animação feita de camadas nossas: no vídeo entra animada no cursor de tempo, e duration_ms estica a animação inteira; na imagem entra o último quadro dela. " +
	"A categoria audio (só no vídeo) traz sons prontos de interface e efeitos: entram como clipe de som numa faixa de áudio em at_ms, com a duração do próprio som, e x, y, w, h e colors não valem para eles; áudios do próprio usuário entram com add_media, e música, voz e efeitos novos saem de studio_generate_music, studio_generate_voiceover e studio_generate_sfx. "

type elementOperation interface {
	element() (string, studioElementArgs, []string)
}

func (o studioVideoOperation) element() (string, studioElementArgs, []string) {
	if len(o.ClipIDs) > 0 {
		return o.Op, o.studioElementArgs, o.ClipIDs
	}
	if o.ClipID != "" {
		return o.Op, o.studioElementArgs, []string{o.ClipID}
	}
	return o.Op, o.studioElementArgs, nil
}

func (o studioImageOperation) element() (string, studioElementArgs, []string) {
	return o.Op, o.studioElementArgs, nil
}

func checkElements[T elementOperation](ops []T, editor copilot.StudioKind) error {
	var catalog *library.Catalog
	for i, op := range ops {
		name, args, clips := op.element()
		if name != "add_element" {
			continue
		}
		if catalog == nil {
			loaded, err := library.Default()
			if err != nil {
				return errors.New(studioLibraryBroken)
			}
			catalog = loaded
		}
		if msg := elementIssue(catalog, args, string(editor), clips); msg != "" {
			return fmt.Errorf("%w: operação %d: %s", copilottools.ErrInvalidArgs, i+1, msg)
		}
	}
	return nil
}

func elementIssue(catalog *library.Catalog, args studioElementArgs, editor string, clips []string) string {
	id := strings.TrimSpace(args.ItemID)
	if id == "" {
		return "add_element precisa de item_id; busque o item com studio_library_search"
	}
	item, ok := catalog.Find(id)
	if !ok {
		return fmt.Sprintf("o item %q não existe na biblioteca; parecidos: %s; busque com studio_library_search", id, strings.Join(catalog.Closest(id, editor, librarySuggestions), ", "))
	}
	if !item.Supports(editor) {
		return fmt.Sprintf("o item %s não entra no editor de %s; parecidos que entram: %s", id, editorName(editor), strings.Join(catalog.Closest(id, editor, librarySuggestions), ", "))
	}
	if editor == string(copilot.StudioVideo) && item.Frames() && len(clips) == 0 {
		return fmt.Sprintf("o item %s é moldura ou grade: no vídeo ele recorta clipes que já existem; mande clip_ids com os clipes de vídeo ou imagem", id)
	}
	slots := item.SlotIDs()
	for _, text := range args.Texts {
		if !copilottools.Contains(slots, text.Slot) {
			if len(slots) == 0 {
				return fmt.Sprintf("o item %s não tem texto para trocar", id)
			}
			return fmt.Sprintf("o item %s não tem o slot %q; use um de: %s", id, text.Slot, strings.Join(slots, ", "))
		}
	}
	return ""
}

func editorName(editor string) string {
	if editor == string(copilot.StudioImage) {
		return "imagem"
	}
	return "vídeo"
}

type studioLibraryTool struct {
	studioScope
}

func NewStudioLibrarySearchTool() copilot.Tool {
	return &studioLibraryTool{}
}

func (t *studioLibraryTool) Meta() copilot.Meta { return studioReadMeta() }

func (t *studioLibraryTool) Definition() tools.Definition {
	def := copilottools.Definition("studio_library_search",
		"Busca na biblioteca de elementos do Estúdio, a mesma dos painéis Texto e Elementos (formas, linhas tracejadas, pontilhadas e com pontas, polígonos, estrelas, setas, fluxograma, balões, faixas e selos, ícones vetoriais em seis pesos, emojis vetoriais, textos prontos, animações, molduras, grades, sons prontos, controles), sem passar pelo editor. "+
			"Procura pelo nome e por palavras-chave no idioma da conversa e em inglês, sem precisar de acento, e devolve os itens que entram no editor: item_id, name, category, kind (layers vira camadas; clips vira clipes de vídeo; frame e grid são molduras e grades para fotos e clipes; sticker é uma animação que na imagem entra parada no último quadro), "+
			"ratio (largura dividida pela altura; numa grade é 1 e ela se ajusta ao quadro), slots (os textos que dá para trocar, com o exemplo) e colors (as cores do item nesse editor, para trocar em add_element). "+
			"Sem query e sem category, lista as categorias com quantos itens cada uma tem. Insira o item escolhido com add_element em studio_edit_image ou studio_edit_video.",
		studioLibrarySearchArgs{})
	if catalog, err := library.Default(); err == nil {
		param := def.Parameters["category"]
		for _, category := range catalog.Categories {
			param.Enum = append(param.Enum, category.ID)
		}
		def.Parameters["category"] = param
	}
	return def
}

type libraryResultSlot struct {
	Slot   string `json:"slot"`
	Sample string `json:"sample"`
}

type libraryResultItem struct {
	ItemID      string              `json:"item_id"`
	Name        string              `json:"name"`
	Category    string              `json:"category"`
	Subcategory string              `json:"subcategory,omitempty"`
	Kind        string              `json:"kind"`
	Ratio       float64             `json:"ratio"`
	Editors     []string            `json:"editors"`
	Slots       []libraryResultSlot `json:"slots,omitempty"`
	Colors      []string            `json:"colors,omitempty"`
	Credit      string              `json:"credit,omitempty"`
}

type libraryResultCategory struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Panel string `json:"panel"`
	Count int    `json:"count"`
}

type libraryResult struct {
	Editor     string                  `json:"editor"`
	Total      int                     `json:"total"`
	Items      []libraryResultItem     `json:"items,omitempty"`
	Categories []libraryResultCategory `json:"categories,omitempty"`
}

func (t *studioLibraryTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	if !cc.View.OnStudio() {
		return studioFailure(studioNotHere)
	}
	var a studioLibrarySearchArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return studioFailure(err.Error())
	}
	if a.Limit < 0 {
		return studioFailure("limit precisa ser maior que 0")
	}
	catalog, err := library.Default()
	if err != nil {
		return studioFailure(studioLibraryBroken)
	}
	editor := a.Editor
	if editor == "" {
		editor = string(cc.View.ProjectKind)
	}
	if editor != string(copilot.StudioImage) && editor != string(copilot.StudioVideo) {
		return studioFailure("informe editor image ou video")
	}
	if a.Category != "" {
		if _, ok := catalog.Category(a.Category); !ok {
			return studioFailure(fmt.Sprintf("a categoria %q não existe", a.Category))
		}
	}
	locale := library.Locale(cc.Locale)
	matches := catalog.Search(library.Query{Text: a.Query, Category: a.Category, Editor: editor, Locale: locale})
	result := libraryResult{Editor: editor, Total: len(matches)}
	if strings.TrimSpace(a.Query) == "" && a.Category == "" {
		result.Categories = libraryCategories(catalog, matches, locale)
		return copilot.Result{Status: copilot.StatusOK, Data: result}
	}
	limit := a.Limit
	if limit == 0 {
		limit = defaultLibraryResults
	}
	for _, item := range matches[:min(limit, len(matches))] {
		result.Items = append(result.Items, libraryItem(item, editor, locale))
	}
	return copilot.Result{Status: copilot.StatusOK, Data: result}
}

func libraryCategories(catalog *library.Catalog, items []library.Item, locale string) []libraryResultCategory {
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Category]++
	}
	var categories []libraryResultCategory
	for _, category := range catalog.Categories {
		if counts[category.ID] > 0 {
			categories = append(categories, libraryResultCategory{ID: category.ID, Name: category.Names.In(locale), Panel: category.Panel, Count: counts[category.ID]})
		}
	}
	return categories
}

func libraryItem(item library.Item, editor, locale string) libraryResultItem {
	slots := make([]libraryResultSlot, 0, len(item.Slots))
	for _, slot := range item.Slots {
		slots = append(slots, libraryResultSlot{Slot: slot.ID, Sample: slot.Samples.In(locale)})
	}
	return libraryResultItem{
		ItemID:      item.ID,
		Name:        item.Names.In(locale),
		Category:    item.Category,
		Subcategory: item.Subcategory,
		Kind:        item.Kind,
		Ratio:       item.Ratio,
		Editors:     item.Editors,
		Slots:       slots,
		Colors:      item.Colors[editor],
		Credit:      item.Source.Credit,
	}
}
