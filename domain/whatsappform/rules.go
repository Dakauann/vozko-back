package whatsappform

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

const isoDate = "2006-01-02"

func ValidKey(key string) bool {
	return keyPattern.MatchString(key)
}

type checker struct {
	doc      Document
	problems Problems
}

func ValidateStructure(doc Document) Problems {
	c := &checker{doc: doc}
	c.document()
	return c.problems
}

func (c *checker) add(screen, block, property, code string, limit int) {
	c.problems = append(c.problems, Problem{Screen: screen, Block: block, Property: property, Code: code, Limit: limit})
}

func runes(s string) int {
	return utf8.RuneCountInString(s)
}

func blank(s string) bool {
	return strings.TrimSpace(s) == ""
}

func (c *checker) document() {
	if c.doc.Schema != DocumentSchema {
		c.add("", "", "schema", ProblemInvalid, DocumentSchema)
	}
	if len(c.doc.Categories) == 0 {
		c.add("", "", "categories", ProblemRequired, 0)
	}
	for _, cat := range c.doc.Categories {
		if !cat.Valid() {
			c.add("", "", "categories", ProblemInvalid, 0)
			break
		}
	}
	if len(c.doc.Screens) == 0 {
		c.add("", "", "screens", ProblemRequired, 0)
		return
	}
	c.keys()
	if len(c.doc.Fields()) == 0 {
		c.add("", "", "blocks", ProblemNoInput, 0)
	}
	for i, s := range c.doc.Screens {
		c.screen(i, s)
	}
	c.reachability()
}

func (c *checker) keys() {
	screens, blocks, fields := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, s := range c.doc.Screens {
		switch {
		case !ValidKey(s.Key):
			c.add(s.Key, "", "key", ProblemInvalid, MaxKeyLength)
		case screens[s.Key]:
			c.add(s.Key, "", "key", ProblemDuplicate, 0)
		}
		screens[s.Key] = true
		for _, b := range s.Blocks {
			switch {
			case !ValidKey(b.Key):
				c.add(s.Key, b.Key, "key", ProblemInvalid, MaxKeyLength)
			case blocks[b.Key]:
				c.add(s.Key, b.Key, "key", ProblemDuplicate, 0)
			}
			blocks[b.Key] = true
			if b.Field == nil {
				continue
			}
			switch {
			case !ValidKey(b.Field.Key):
				c.add(s.Key, b.Key, "key", ProblemInvalid, MaxKeyLength)
			case fields[b.Field.Key]:
				c.add(s.Key, b.Key, "key", ProblemDuplicate, 0)
			}
			fields[b.Field.Key] = true
		}
	}
}

type screenCounts struct {
	components, images, links, optIns, pickers int
}

func (c *checker) screen(index int, s Screen) {
	if blank(s.Title) {
		c.add(s.Key, "", "title", ProblemRequired, 0)
	}
	switch {
	case blank(s.SubmitLabel):
		c.add(s.Key, "", "submitLabel", ProblemRequired, 0)
	case runes(s.SubmitLabel) > MaxFooterLabel:
		c.add(s.Key, "", "submitLabel", ProblemTooLong, MaxFooterLabel)
	}
	counts := screenCounts{components: 1}
	for bi, b := range s.Blocks {
		c.block(index, bi, s, b, &counts)
	}
	if counts.components > MaxComponentsPerScreen {
		c.add(s.Key, "", "blocks", ProblemTooMany, MaxComponentsPerScreen)
	}
	c.route(index, s)
}

func (c *checker) block(screenIndex, blockIndex int, s Screen, b Block, counts *screenCounts) {
	if (b.Display == nil) == (b.Field == nil) {
		c.add(s.Key, b.Key, "", ProblemInvalid, 0)
		return
	}
	if b.Display != nil {
		c.display(s.Key, b, counts)
	} else {
		c.field(s.Key, b, counts)
	}
	if b.VisibleIf != nil {
		c.condition(screenIndex, blockIndex, s.Key, b)
	}
}

func (c *checker) countOver(screen, block string, count *int, limit int) {
	*count++
	if *count > limit {
		c.add(screen, block, "kind", ProblemTooMany, limit)
	}
}

func (c *checker) display(screen string, b Block, counts *screenCounts) {
	d := b.Display
	if !slices.Contains(DisplayKinds, d.Kind) {
		c.add(screen, b.Key, "kind", ProblemInvalid, 0)
		return
	}
	counts.components++
	switch d.Kind {
	case DisplayImage:
		if blank(d.MediaID) {
			c.add(screen, b.Key, "mediaId", ProblemRequired, 0)
		}
		c.countOver(screen, b.Key, &counts.images, MaxImagesPerScreen)
		return
	case DisplayLink:
		if !httpsURL(d.URL) {
			c.add(screen, b.Key, "url", ProblemInvalid, 0)
		}
		c.countOver(screen, b.Key, &counts.links, MaxLinksPerScreen)
	}
	c.text(screen, b.Key, "text", d.Text, DisplayTextLimit(d.Kind), true)
}

func (c *checker) text(screen, block, property, value string, limit int, required bool) {
	switch {
	case required && blank(value):
		c.add(screen, block, property, ProblemRequired, 0)
	case runes(value) > limit:
		c.add(screen, block, property, ProblemTooLong, limit)
	}
}

func httpsURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && u.Scheme == "https" && u.Host != ""
}

func (c *checker) field(screen string, b Block, counts *screenCounts) {
	f := b.Field
	if !f.Kind.Valid() {
		c.add(screen, b.Key, "kind", ProblemInvalid, 0)
		return
	}
	counts.components++
	if !blank(f.Question) {
		counts.components++
		c.text(screen, b.Key, "question", f.Question, MaxQuestionLength, false)
	}
	c.text(screen, b.Key, "label", f.Label, LabelLimit(f.Kind), true)
	c.text(screen, b.Key, "help", f.Help, MaxHelpLength, false)
	switch {
	case f.Kind == FieldOptIn:
		c.countOver(screen, b.Key, &counts.optIns, MaxOptInsPerScreen)
	case f.Kind.IsMedia():
		c.countOver(screen, b.Key, &counts.pickers, MaxPickersPerScreen)
	}
	c.pattern(screen, b.Key, f)
	c.bounds(screen, b.Key, f)
	c.options(screen, b.Key, f)
	c.mimeTypes(screen, b.Key, f)
	c.dates(screen, b.Key, f)
	c.terms(screen, b.Key, f)
}

func (c *checker) pattern(screen, block string, f *Field) {
	if f.Pattern == "" {
		return
	}
	if f.Kind != FieldShortText && f.Kind != FieldNumber {
		c.add(screen, block, "pattern", ProblemNotAllowed, 0)
		return
	}
	if _, err := regexp.Compile(f.Pattern); err != nil {
		c.add(screen, block, "pattern", ProblemInvalid, 0)
	}
	if blank(f.Help) {
		c.add(screen, block, "help", ProblemRequired, 0)
	}
}

func (c *checker) bounds(screen, block string, f *Field) {
	if f.Min < 0 || f.Max < 0 {
		c.add(screen, block, "min", ProblemInvalid, 0)
		return
	}
	if f.Max > 0 && f.Min > f.Max {
		c.add(screen, block, "min", ProblemInvalid, f.Max)
	}
	switch {
	case f.Kind.IsMedia():
		if f.Max > MaxUploads {
			c.add(screen, block, "max", ProblemTooMany, MaxUploads)
		}
	case f.Kind.IsMultiChoice():
		if f.Max > len(f.Options) {
			c.add(screen, block, "max", ProblemTooMany, len(f.Options))
		}
	case f.Kind.IsTextInput(), f.Kind == FieldLongText:
	default:
		if f.Min != 0 || f.Max != 0 {
			c.add(screen, block, "min", ProblemNotAllowed, 0)
		}
	}
}

func (c *checker) options(screen, block string, f *Field) {
	if !f.Kind.HasOptions() {
		if len(f.Options) > 0 {
			c.add(screen, block, "options", ProblemNotAllowed, 0)
		}
		return
	}
	lo, hi := OptionBounds(f.Kind)
	switch {
	case len(f.Options) < lo:
		c.add(screen, block, "options", ProblemTooFew, lo)
	case len(f.Options) > hi:
		c.add(screen, block, "options", ProblemTooMany, hi)
	}
	seen := map[string]bool{}
	for _, o := range f.Options {
		switch {
		case !ValidKey(o.ID):
			c.add(screen, block, "options.id", ProblemInvalid, MaxKeyLength)
		case seen[o.ID]:
			c.add(screen, block, "options", ProblemDuplicate, 0)
		}
		seen[o.ID] = true
		c.text(screen, block, "options.title", o.Title, MaxOptionTitle, true)
		c.text(screen, block, "options.description", o.Description, MaxOptionDescription, false)
	}
}

func (c *checker) mimeTypes(screen, block string, f *Field) {
	if len(f.MimeTypes) == 0 {
		return
	}
	if f.Kind != FieldDocument {
		c.add(screen, block, "mimeTypes", ProblemNotAllowed, 0)
		return
	}
	for _, m := range f.MimeTypes {
		if !slices.Contains(AllowedDocumentMimeTypes, m) {
			c.add(screen, block, "mimeTypes", ProblemNotAllowed, 0)
			return
		}
	}
}

func (c *checker) dates(screen, block string, f *Field) {
	if f.MinDate == "" && f.MaxDate == "" {
		return
	}
	if f.Kind != FieldDate && f.Kind != FieldDateRange {
		c.add(screen, block, "minDate", ProblemNotAllowed, 0)
		return
	}
	parsed := map[string]time.Time{}
	for property, value := range map[string]string{"minDate": f.MinDate, "maxDate": f.MaxDate} {
		if value == "" {
			continue
		}
		at, err := time.Parse(isoDate, value)
		if err != nil {
			c.add(screen, block, property, ProblemInvalid, 0)
			continue
		}
		parsed[property] = at
	}
	lo, hasLo := parsed["minDate"]
	hi, hasHi := parsed["maxDate"]
	if hasLo && hasHi && lo.After(hi) {
		c.add(screen, block, "minDate", ProblemInvalid, 0)
	}
}

func (c *checker) terms(screen, block string, f *Field) {
	if f.TermsURL == "" {
		return
	}
	if f.Kind != FieldOptIn {
		c.add(screen, block, "termsUrl", ProblemNotAllowed, 0)
		return
	}
	if !httpsURL(f.TermsURL) {
		c.add(screen, block, "termsUrl", ProblemInvalid, 0)
	}
}

func (c *checker) condition(screenIndex, blockIndex int, screen string, b Block) {
	cond := b.VisibleIf
	ref, ok := c.doc.Field(cond.FieldKey)
	if !ok {
		c.add(screen, b.Key, "visibleIf", ProblemUnknownTarget, 0)
		return
	}
	if ref.Screen > screenIndex || (ref.Screen == screenIndex && ref.Block >= blockIndex) {
		c.add(screen, b.Key, "visibleIf", ProblemInvalid, 0)
		return
	}
	switch {
	case ref.Field.Kind.IsSingleChoice():
		if _, ok := ref.Field.Option(cond.Value); !ok {
			c.add(screen, b.Key, "visibleIf", ProblemUnknownTarget, 0)
		}
	case ref.Field.Kind == FieldOptIn:
		if cond.Value != "true" && cond.Value != "false" {
			c.add(screen, b.Key, "visibleIf", ProblemInvalid, 0)
		}
	default:
		c.add(screen, b.Key, "visibleIf", ProblemInvalid, 0)
	}
}

func (c *checker) target(index int, screen, property, key string) {
	target := c.doc.ScreenIndex(key)
	switch {
	case target < 0:
		c.add(screen, "", property, ProblemUnknownTarget, 0)
	case target <= index:
		c.add(screen, "", property, ProblemBackwardRoute, 0)
	}
}

func (c *checker) route(index int, s Screen) {
	r := s.Route
	if r.Next != "" {
		c.target(index, s.Key, "route.next", r.Next)
	}
	if len(r.Branches) == 0 && r.BranchOn == "" {
		return
	}
	if r.BranchOn == "" {
		c.add(s.Key, "", "route.branchOn", ProblemRequired, 0)
		return
	}
	if r.Next == "" {
		c.add(s.Key, "", "route.next", ProblemRequired, 0)
	}
	ref, ok := c.doc.Field(r.BranchOn)
	if !ok || ref.Screen != index || !ref.Field.Kind.IsSingleChoice() {
		c.add(s.Key, "", "route.branchOn", ProblemInvalid, 0)
		return
	}
	if !ref.Field.Required {
		c.add(s.Key, "", "route.branchOn", ProblemRequired, 0)
	}
	seen := map[string]bool{}
	for _, branch := range r.Branches {
		if _, ok := ref.Field.Option(branch.OptionID); !ok {
			c.add(s.Key, "", "route.branches", ProblemUnknownTarget, 0)
		}
		if seen[branch.OptionID] {
			c.add(s.Key, "", "route.branches", ProblemDuplicate, 0)
		}
		seen[branch.OptionID] = true
		c.target(index, s.Key, "route.branches", branch.Next)
	}
	if len(r.Targets()) > MaxRouteBranches {
		c.add(s.Key, "", "route.branches", ProblemTooMany, MaxRouteBranches)
	}
}

func (c *checker) reachability() {
	reached := map[int]bool{0: true}
	for i, s := range c.doc.Screens {
		if !reached[i] {
			continue
		}
		for _, key := range s.Route.Targets() {
			if target := c.doc.ScreenIndex(key); target > i {
				reached[target] = true
			}
		}
	}
	for i, s := range c.doc.Screens {
		if !reached[i] {
			c.add(s.Key, "", "key", ProblemUnreachable, 0)
		}
	}
}
