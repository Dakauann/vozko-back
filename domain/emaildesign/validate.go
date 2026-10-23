package emaildesign

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	MaxSubjectRunes     = 150
	maxPreheaderRunes   = 200
	maxHeadingRunes     = 200
	maxTextBlockRunes   = 5000
	maxButtonLabelRunes = 80
	maxAltRunes         = 300
	maxHrefRunes        = 2048
	maxSocialLabelRunes = 50
	maxCompanyRunes     = 200
	maxAddressRunes     = 300
	maxFooterTextRunes  = 1000
	maxBlockIDRunes     = 64
	MinImageWidth       = 10
	MaxImageWidth       = 100
	MinDividerThickness = 1
	MaxDividerThickness = 8
	MinSpacerHeight     = 4
	MaxSpacerHeight     = 120
)

const (
	issueFooterMissing    = "footer_missing"
	issueTextEmpty        = "text_empty"
	issueImageSrcMissing  = "image_src_missing"
	issueSocialLinksEmpty = "social_links_empty"
	issueSocialURLMissing = "social_url_missing"
	issueCompanyMissing   = "footer_company_missing"
	issueAddressMissing   = "footer_address_missing"
	issueSubjectEmpty     = "subject_empty"
)

var draftGaps = map[string]bool{
	issueTextEmpty:         true,
	linkMissing:            true,
	issueImageSrcMissing:   true,
	issueSocialLinksEmpty:  true,
	issueSocialURLMissing:  true,
	issueCompanyMissing:    true,
	issueAddressMissing:    true,
	issueFooterMissing:     true,
	issueSubjectEmpty:      true,
	issueUnknownMergeTag:   true,
	issueMergeTagMalformed: true,
}

var (
	ErrInvalidDocument = errors.New("emaildesign: invalid document")
	ErrInvalidSubject  = errors.New("emaildesign: invalid subject")
)

type Issue struct {
	Code    string `json:"code"`
	BlockID string `json:"block_id,omitempty"`
	Field   string `json:"field,omitempty"`
}

func (i Issue) DraftGap() bool {
	return draftGaps[i.Code]
}

func StructuralIssues(issues []Issue) []Issue {
	var out []Issue
	for _, issue := range issues {
		if !issue.DraftGap() {
			out = append(out, issue)
		}
	}
	return out
}

type ValidationError struct {
	Kind   error
	Issues []Issue
}

func (e *ValidationError) Error() string {
	codes := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		codes = append(codes, issue.Code)
	}
	return e.Kind.Error() + ": " + strings.Join(codes, ", ")
}

func (e *ValidationError) Unwrap() error {
	return e.Kind
}

var blockIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type validator struct {
	issues []Issue
	seen   map[string]bool
	count  int
}

func (v *validator) add(code, blockID, field string) {
	v.issues = append(v.issues, Issue{Code: code, BlockID: blockID, Field: field})
}

func Validate(doc Document) []Issue {
	v := &validator{seen: map[string]bool{}}
	if doc.Version != CurrentVersion {
		v.add("version_unsupported", "", "version")
	}
	v.settings(doc.Settings)
	for i, block := range doc.Blocks {
		v.block(block, false)
		if block.Kind == KindFooter && i != len(doc.Blocks)-1 && v.isFirstFooter(doc.Blocks, i) {
			v.add("footer_not_last", block.ID, "kind")
		}
	}
	v.footers(doc.Blocks)
	if v.count > MaxBlocks {
		v.add("too_many_blocks", "", "blocks")
	}
	return v.issues
}

func (v *validator) isFirstFooter(blocks []Block, index int) bool {
	for i := 0; i < index; i++ {
		if blocks[i].Kind == KindFooter {
			return false
		}
	}
	return true
}

func (v *validator) footers(blocks []Block) {
	found := false
	for _, block := range blocks {
		if block.Kind != KindFooter {
			continue
		}
		if found {
			v.add("footer_duplicate", block.ID, "kind")
		}
		found = true
	}
	if !found {
		v.add(issueFooterMissing, "", "blocks")
	}
}

func (v *validator) settings(s Settings) {
	if s.Width != 0 && (s.Width < MinWidth || s.Width > MaxWidth) {
		v.add("width_invalid", "", "settings.width")
	}
	colors := []struct {
		field string
		value string
	}{
		{field: "settings.background_color", value: s.BackgroundColor},
		{field: "settings.content_background_color", value: s.ContentBackgroundColor},
		{field: "settings.text_color", value: s.TextColor},
		{field: "settings.link_color", value: s.LinkColor},
		{field: "settings.button_color", value: s.ButtonColor},
		{field: "settings.button_text_color", value: s.ButtonTextColor},
	}
	for _, c := range colors {
		v.optionalColor(c.value, "", c.field)
	}
	if s.FontFamily != "" {
		if _, ok := lookupFontStack(s.FontFamily); !ok {
			v.add("font_family_invalid", "", "settings.font_family")
		}
	}
	v.mergeText(s.Preheader, "", "settings.preheader", maxPreheaderRunes)
}

func (v *validator) optionalColor(value, blockID, field string) {
	if value != "" && !validColor(value) {
		v.add("color_invalid", blockID, field)
	}
}

func (v *validator) mergeText(value, blockID, field string, maxRunes int) {
	if utf8.RuneCountInString(value) > maxRunes {
		v.add("text_too_long", blockID, field)
	}
	for _, problem := range mergeProblems(value) {
		v.add(problem, blockID, field)
	}
}

func (v *validator) requiredMergeText(value, blockID, field string, maxRunes int) {
	if strings.TrimSpace(value) == "" {
		v.add(issueTextEmpty, blockID, field)
		return
	}
	v.mergeText(value, blockID, field, maxRunes)
}

func (v *validator) link(href, blockID, field string) {
	if utf8.RuneCountInString(href) > maxHrefRunes {
		v.add("text_too_long", blockID, field)
		return
	}
	for _, problem := range mergeProblems(href) {
		v.add(problem, blockID, field)
	}
	if code := checkLink(linkProbe(href)); code != linkOK {
		v.add(code, blockID, field)
	}
}

func linkProbe(href string) string {
	segments, _ := parseTemplate(href)
	var b strings.Builder
	for _, seg := range segments {
		if seg.tag != nil {
			b.WriteString("x")
			continue
		}
		b.WriteString(seg.literal)
	}
	return b.String()
}

func (v *validator) align(a Align, blockID string) {
	if !validAlign(a) {
		v.add("align_invalid", blockID, "align")
	}
}

func (v *validator) identity(b Block) {
	v.count++
	switch {
	case b.ID == "":
		v.add("block_id_missing", "", "id")
	case utf8.RuneCountInString(b.ID) > maxBlockIDRunes || !blockIDPattern.MatchString(b.ID):
		v.add("block_id_invalid", b.ID, "id")
	case v.seen[b.ID]:
		v.add("block_id_duplicate", b.ID, "id")
	default:
		v.seen[b.ID] = true
	}
}

func (v *validator) block(b Block, insideColumns bool) {
	v.identity(b)
	if !b.Kind.Known() {
		v.add("block_kind_unknown", b.ID, "kind")
		return
	}
	if !propsMatchKind(b) {
		v.add("block_props_mismatch", b.ID, "kind")
		return
	}
	if insideColumns && b.Kind == KindColumns {
		v.add("nested_columns", b.ID, "kind")
		return
	}
	if insideColumns && b.Kind == KindFooter {
		v.add("footer_in_columns", b.ID, "kind")
		return
	}
	if !validPadding(b.Padding) {
		v.add("padding_invalid", b.ID, "padding")
	}
	if b.Kind == KindSpacer && b.Padding != "" {
		v.add("padding_not_supported", b.ID, "padding")
	}
	v.optionalColor(b.BackgroundColor, b.ID, "background_color")
	switch b.Kind {
	case KindHeading:
		v.heading(b.ID, *b.Heading)
	case KindText:
		v.text(b.ID, *b.Text)
	case KindButton:
		v.button(b.ID, *b.Button)
	case KindImage:
		v.image(b.ID, *b.Image)
	case KindDivider:
		v.divider(b.ID, *b.Divider)
	case KindSpacer:
		v.spacer(b.ID, *b.Spacer)
	case KindColumns:
		v.columns(b.ID, *b.Columns)
	case KindSocial:
		v.social(b.ID, *b.Social)
	case KindFooter:
		v.footer(b.ID, *b.Footer)
	}
}

func propsMatchKind(b Block) bool {
	set := map[BlockKind]bool{
		KindHeading: b.Heading != nil,
		KindText:    b.Text != nil,
		KindButton:  b.Button != nil,
		KindImage:   b.Image != nil,
		KindDivider: b.Divider != nil,
		KindSpacer:  b.Spacer != nil,
		KindColumns: b.Columns != nil,
		KindSocial:  b.Social != nil,
		KindFooter:  b.Footer != nil,
	}
	for kind, present := range set {
		if present != (kind == b.Kind) {
			return false
		}
	}
	return true
}

func (v *validator) heading(id string, h Heading) {
	v.requiredMergeText(h.Text, id, "text", maxHeadingRunes)
	if h.Level < 1 || h.Level > 3 {
		v.add("heading_level_invalid", id, "level")
	}
	v.align(h.Align, id)
	v.optionalColor(h.Color, id, "color")
}

func (v *validator) text(id string, t Text) {
	v.align(t.Align, id)
	if len(t.Paragraphs) == 0 {
		v.add(issueTextEmpty, id, "paragraphs")
		return
	}
	total := 0
	for pi, p := range t.Paragraphs {
		for ri, r := range p.Runs {
			field := "paragraphs." + strconv.Itoa(pi) + ".runs." + strconv.Itoa(ri)
			total += utf8.RuneCountInString(r.Text)
			if r.Text == "" {
				v.add("run_text_empty", id, field+".text")
			}
			for _, problem := range mergeProblems(r.Text) {
				v.add(problem, id, field+".text")
			}
			if r.Link != nil {
				v.link(r.Link.Href, id, field+".link.href")
			}
		}
	}
	if total > maxTextBlockRunes {
		v.add("text_too_long", id, "paragraphs")
	}
}

func (v *validator) button(id string, b Button) {
	v.requiredMergeText(b.Label, id, "label", maxButtonLabelRunes)
	v.link(b.Href, id, "href")
	v.align(b.Align, id)
	v.optionalColor(b.Color, id, "color")
	v.optionalColor(b.TextColor, id, "text_color")
}

func (v *validator) image(id string, img Image) {
	switch {
	case strings.TrimSpace(img.Src) == "":
		v.add(issueImageSrcMissing, id, "src")
	case utf8.RuneCountInString(img.Src) > maxHrefRunes:
		v.add("text_too_long", id, "src")
	case !isHTTPSWithHost(img.Src):
		v.add("image_src_not_https", id, "src")
	}
	v.mergeText(img.Alt, id, "alt", maxAltRunes)
	if img.Href != "" {
		v.link(img.Href, id, "href")
	}
	if img.Width < MinImageWidth || img.Width > MaxImageWidth {
		v.add("image_width_invalid", id, "width")
	}
	v.align(img.Align, id)
}

func (v *validator) divider(id string, d Divider) {
	v.optionalColor(d.Color, id, "color")
	if d.Thickness < MinDividerThickness || d.Thickness > MaxDividerThickness {
		v.add("divider_thickness_invalid", id, "thickness")
	}
}

func (v *validator) spacer(id string, s Spacer) {
	if s.Height < MinSpacerHeight || s.Height > MaxSpacerHeight {
		v.add("spacer_height_invalid", id, "height")
	}
}

func (v *validator) columns(id string, c Columns) {
	shares := c.Ratio.shares()
	if shares == nil {
		v.add("columns_ratio_invalid", id, "ratio")
	} else if len(shares) != len(c.Columns) {
		v.add("columns_count_mismatch", id, "columns")
	}
	for _, column := range c.Columns {
		for _, child := range column.Blocks {
			v.block(child, true)
		}
	}
}

func (v *validator) social(id string, s Social) {
	v.align(s.Align, id)
	if len(s.Links) == 0 {
		v.add(issueSocialLinksEmpty, id, "links")
	}
	for i, link := range s.Links {
		prefix := "links." + strconv.Itoa(i)
		if !link.Network.known() {
			v.add("social_network_invalid", id, prefix+".network")
		}
		switch {
		case strings.TrimSpace(link.URL) == "":
			v.add(issueSocialURLMissing, id, prefix+".url")
		case !isHTTPSWithHost(link.URL):
			v.add("social_url_not_https", id, prefix+".url")
		}
		v.mergeText(link.Label, id, prefix+".label", maxSocialLabelRunes)
	}
}

func (v *validator) footer(id string, f Footer) {
	if strings.TrimSpace(f.CompanyName) == "" {
		v.add(issueCompanyMissing, id, "company_name")
	} else {
		v.mergeText(f.CompanyName, id, "company_name", maxCompanyRunes)
	}
	if strings.TrimSpace(f.Address) == "" {
		v.add(issueAddressMissing, id, "address")
	} else {
		v.mergeText(f.Address, id, "address", maxAddressRunes)
	}
	v.mergeText(f.Text, id, "text", maxFooterTextRunes)
}

func ValidateSubject(subject string) []Issue {
	var issues []Issue
	add := func(code string) {
		issues = append(issues, Issue{Code: code, Field: "subject"})
	}
	if strings.TrimSpace(subject) == "" {
		add(issueSubjectEmpty)
		return issues
	}
	if strings.ContainsAny(subject, "\r\n") {
		add("subject_invalid_characters")
		return issues
	}
	if utf8.RuneCountInString(subject) > MaxSubjectRunes {
		add("subject_too_long")
	}
	for _, problem := range mergeProblems(subject) {
		add(problem)
	}
	return issues
}
