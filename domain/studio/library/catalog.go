package library

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"vozko/domain/shared"
)

const (
	DefaultLocale  = "pt"
	englishLocale  = "en"
	CatalogVersion = 2
)

var Locales = []string{"pt", "en", "es", "de"}

var Editors = []string{"image", "video"}

const (
	KindLayers  = "layers"
	KindClips   = "clips"
	KindFrame   = "frame"
	KindGrid    = "grid"
	KindSticker = "sticker"
	KindMockup  = "mockup"
	KindAsset   = "asset"
)

var Kinds = []string{KindLayers, KindClips, KindFrame, KindGrid, KindSticker, KindMockup, KindAsset}

//go:embed catalog.json
var embedded []byte

type Names map[string]string

type Slot struct {
	ID      string `json:"id"`
	Samples Names  `json:"samples"`
}

type Subcategory struct {
	ID    string `json:"id"`
	Names Names  `json:"names"`
}

type Category struct {
	ID            string        `json:"id"`
	Panel         string        `json:"panel"`
	Names         Names         `json:"names"`
	Subcategories []Subcategory `json:"subcategories,omitempty"`
	Fixture       string        `json:"fixture,omitempty"`
}

type Source struct {
	Set     string `json:"set"`
	License string `json:"license"`
	Credit  string `json:"credit,omitempty"`
	URL     string `json:"url,omitempty"`
}

type Asset struct {
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ContentType string `json:"contentType"`
}

type Item struct {
	ID          string              `json:"id"`
	Category    string              `json:"category"`
	Subcategory string              `json:"subcategory,omitempty"`
	Panel       string              `json:"panel"`
	Editors     []string            `json:"editors"`
	Kind        string              `json:"kind"`
	Ratio       float64             `json:"ratio"`
	Names       Names               `json:"names"`
	Tags        map[string][]string `json:"tags,omitempty"`
	Slots       []Slot              `json:"slots,omitempty"`
	Colors      map[string][]string `json:"colors"`
	Source      Source              `json:"source"`
	Asset       *Asset              `json:"asset,omitempty"`
}

type Catalog struct {
	Version    int        `json:"version"`
	Categories []Category `json:"categories"`
	Items      []Item     `json:"items"`
	byID       map[string]int
	foldMu     sync.Mutex
	folds      map[string][]foldedTerms
}

type foldedTerms struct {
	name     string
	terms    []string
	haystack string
}

type Query struct {
	Text     string
	Category string
	Editor   string
	Locale   string
}

var (
	loadOnce sync.Once
	loaded   *Catalog
	loadErr  error
)

func Default() (*Catalog, error) {
	loadOnce.Do(func() { loaded, loadErr = Parse(embedded) })
	return loaded, loadErr
}

type catalogEntry struct {
	Item
	Base *int `json:"base,omitempty"`
}

type catalogFile struct {
	Version    int            `json:"version"`
	Categories []Category     `json:"categories"`
	Shared     []Item         `json:"shared,omitempty"`
	Items      []catalogEntry `json:"items"`
}

func (e catalogEntry) expanded(shared []Item) (Item, error) {
	if e.Base == nil {
		return e.Item, nil
	}
	own := e.Item
	own.ID, own.Subcategory, own.Names = "", "", nil
	if *e.Base < 0 || *e.Base >= len(shared) || shared[*e.Base].ID != "" || !reflect.ValueOf(own).IsZero() {
		return Item{}, fmt.Errorf("library catalog: item %q points at a shared body it cannot use", e.ID)
	}
	item := shared[*e.Base]
	item.ID, item.Subcategory = e.ID, e.Subcategory
	if e.Names != nil {
		item.Names = e.Names
	}
	return item, nil
}

func Parse(raw []byte) (*Catalog, error) {
	var file catalogFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("library catalog: %w", err)
	}
	if file.Version != CatalogVersion {
		return nil, fmt.Errorf("library catalog: version %d, want %d", file.Version, CatalogVersion)
	}
	c := Catalog{Version: file.Version, Categories: file.Categories, Items: make([]Item, len(file.Items))}
	for i, entry := range file.Items {
		item, err := entry.expanded(file.Shared)
		if err != nil {
			return nil, err
		}
		c.Items[i] = item
	}
	c.byID = make(map[string]int, len(c.Items))
	for i, item := range c.Items {
		if _, dup := c.byID[item.ID]; dup || item.ID == "" {
			return nil, fmt.Errorf("library catalog: item id %q is empty or repeated", item.ID)
		}
		c.byID[item.ID] = i
	}
	if len(c.Items) == 0 {
		return nil, errors.New("library catalog: no items")
	}
	return &c, nil
}

func (c *Catalog) Find(id string) (Item, bool) {
	i, ok := c.byID[strings.TrimSpace(id)]
	if !ok {
		return Item{}, false
	}
	return c.Items[i], true
}

func (c *Catalog) Category(id string) (Category, bool) {
	for _, category := range c.Categories {
		if category.ID == id {
			return category, true
		}
	}
	return Category{}, false
}

func (i Item) Supports(editor string) bool {
	return contains(i.Editors, editor)
}

func (i Item) Frames() bool {
	return i.Kind == KindFrame || i.Kind == KindGrid || i.Kind == KindMockup
}

const CategoryAudio = "audio"

func (i Item) Sound() bool {
	return i.Category == CategoryAudio
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func (n Names) In(locale string) string {
	if text := n[Locale(locale)]; text != "" {
		return text
	}
	return n[DefaultLocale]
}

func (i Item) SlotIDs() []string {
	ids := make([]string, 0, len(i.Slots))
	for _, slot := range i.Slots {
		ids = append(ids, slot.ID)
	}
	return ids
}

func Locale(raw string) string {
	lower := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.IndexAny(lower, "-_"); i >= 0 {
		lower = lower[:i]
	}
	for _, l := range Locales {
		if l == lower {
			return l
		}
	}
	return DefaultLocale
}

func Fold(text string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(text) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return strings.TrimSpace(b.String())
}

func IDWords(id string) []string {
	var words []string
	for _, part := range strings.FieldsFunc(id, func(r rune) bool { return r == '-' || r == '_' || r == '.' || unicode.IsSpace(r) }) {
		start := 0
		runes := []rune(part)
		for i := 1; i < len(runes); i++ {
			if unicode.IsUpper(runes[i]) {
				words = append(words, strings.ToLower(string(runes[start:i])))
				start = i
			}
		}
		words = append(words, strings.ToLower(string(runes[start:])))
	}
	return words
}

func (i Item) terms(locale string) (string, []string) {
	name := i.Names.In(locale)
	terms := append([]string{name}, i.Tags[locale]...)
	if locale != englishLocale {
		terms = append(terms, i.Names[englishLocale])
		terms = append(terms, i.Tags[englishLocale]...)
	}
	return name, append(terms, IDWords(i.ID)...)
}

func foldTerms(name string, terms []string) foldedTerms {
	folded := make([]string, len(terms))
	for i, term := range terms {
		folded[i] = Fold(term)
	}
	return foldedTerms{name: Fold(name), terms: folded, haystack: strings.Join(folded, " ")}
}

func MatchRank(query, name string, terms []string) (int, bool) {
	wanted := Fold(query)
	return rankFolded(wanted, strings.Fields(wanted), foldTerms(name, terms))
}

func rankFolded(wanted string, tokens []string, f foldedTerms) (int, bool) {
	if len(tokens) == 0 {
		return 0, false
	}
	for _, token := range tokens {
		if !strings.Contains(f.haystack, token) {
			return 0, false
		}
	}
	switch {
	case f.name == wanted:
		return 0, true
	case strings.HasPrefix(f.name, wanted):
		return 1, true
	}
	for _, term := range f.terms {
		if strings.HasPrefix(term, wanted) {
			return 2, true
		}
	}
	return 3, true
}

func (c *Catalog) foldedFor(locale string) []foldedTerms {
	c.foldMu.Lock()
	defer c.foldMu.Unlock()
	if known, ok := c.folds[locale]; ok {
		return known
	}
	folded := make([]foldedTerms, len(c.Items))
	for i, item := range c.Items {
		folded[i] = foldTerms(item.terms(locale))
	}
	if c.folds == nil {
		c.folds = map[string][]foldedTerms{}
	}
	c.folds[locale] = folded
	return folded
}

func (c *Catalog) Search(q Query) []Item {
	locale := Locale(q.Locale)
	type ranked struct {
		item  Item
		index int
		rank  int
	}
	var found []ranked
	wanted := Fold(q.Text)
	tokens := strings.Fields(wanted)
	var folded []foldedTerms
	if len(tokens) > 0 {
		folded = c.foldedFor(locale)
	}
	for index, item := range c.Items {
		if q.Category != "" && item.Category != q.Category {
			continue
		}
		if q.Editor != "" && !item.Supports(q.Editor) {
			continue
		}
		if strings.TrimSpace(q.Text) == "" {
			found = append(found, ranked{item, index, 0})
			continue
		}
		if rank, ok := rankFolded(wanted, tokens, folded[index]); ok {
			found = append(found, ranked{item, index, rank})
		}
	}
	sort.SliceStable(found, func(a, b int) bool {
		if found[a].rank != found[b].rank {
			return found[a].rank < found[b].rank
		}
		return found[a].index < found[b].index
	})
	items := make([]Item, len(found))
	for i, r := range found {
		items[i] = r.item
	}
	return items
}

func (c *Catalog) Closest(id, editor string, count int) []string {
	wanted := strings.ToLower(strings.TrimSpace(id))
	type scored struct {
		id    string
		index int
		score int
	}
	var all []scored
	for index, item := range c.Items {
		if editor != "" && !item.Supports(editor) {
			continue
		}
		score := shared.EditDistance(wanted, item.ID)
		if wanted != "" && strings.Contains(item.ID, wanted) {
			score = 0
		}
		all = append(all, scored{item.ID, index, score})
	}
	sort.SliceStable(all, func(a, b int) bool {
		if all[a].score != all[b].score {
			return all[a].score < all[b].score
		}
		return all[a].index < all[b].index
	})
	ids := make([]string, 0, count)
	for _, s := range all {
		if len(ids) == count {
			break
		}
		ids = append(ids, s.id)
	}
	return ids
}
