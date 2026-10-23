package emaildesign

import (
	"strconv"
	"strings"
	"testing"
)

func TestValidateAcceptsFullDocument(t *testing.T) {
	issues := Validate(fullDocument())
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %+v", issues)
	}
}

func TestValidateAcceptsDefaultsForEmptySettings(t *testing.T) {
	issues := Validate(docWith(headingBlock("h", "Oi")))
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %+v", issues)
	}
}

func TestValidateReportsIssues(t *testing.T) {
	manySpacers := make([]Block, 0, MaxBlocks)
	for i := 0; i < MaxBlocks; i++ {
		manySpacers = append(manySpacers, spacerBlock("s"+strconv.Itoa(i)))
	}

	cases := []struct {
		name    string
		doc     Document
		code    string
		blockID string
		field   string
	}{
		{name: "version", doc: Document{Version: 2, Blocks: []Block{footerBlock()}}, code: "version_unsupported", field: "version"},
		{name: "width low", doc: func() Document { d := docWith(); d.Settings.Width = 400; return d }(), code: "width_invalid", field: "settings.width"},
		{name: "width high", doc: func() Document { d := docWith(); d.Settings.Width = 800; return d }(), code: "width_invalid", field: "settings.width"},
		{name: "settings color", doc: func() Document { d := docWith(); d.Settings.TextColor = "red"; return d }(), code: "color_invalid", field: "settings.text_color"},
		{name: "settings button color", doc: func() Document { d := docWith(); d.Settings.ButtonColor = "#12345"; return d }(), code: "color_invalid", field: "settings.button_color"},
		{name: "font", doc: func() Document { d := docWith(); d.Settings.FontFamily = "comic"; return d }(), code: "font_family_invalid", field: "settings.font_family"},
		{name: "preheader long", doc: func() Document { d := docWith(); d.Settings.Preheader = strings.Repeat("a", 201); return d }(), code: "text_too_long", field: "settings.preheader"},
		{name: "preheader tag", doc: func() Document { d := docWith(); d.Settings.Preheader = "{{last_name}}"; return d }(), code: "unknown_merge_tag", field: "settings.preheader"},
		{name: "too many blocks", doc: docWith(manySpacers...), code: "too_many_blocks", field: "blocks"},
		{name: "id missing", doc: docWith(headingBlock("", "Oi")), code: "block_id_missing", field: "id"},
		{name: "id invalid", doc: docWith(headingBlock("a b", "Oi")), code: "block_id_invalid", blockID: "a b", field: "id"},
		{name: "id duplicate", doc: docWith(headingBlock("x", "Oi"), headingBlock("x", "Ola")), code: "block_id_duplicate", blockID: "x", field: "id"},
		{name: "kind unknown", doc: docWith(Block{ID: "v", Kind: "video"}), code: "block_kind_unknown", blockID: "v", field: "kind"},
		{name: "props mismatch", doc: docWith(Block{ID: "m", Kind: KindHeading, Text: &Text{}}), code: "block_props_mismatch", blockID: "m", field: "kind"},
		{name: "props extra", doc: docWith(Block{ID: "m", Kind: KindHeading, Heading: &Heading{Text: "a", Level: 1}, Spacer: &Spacer{Height: 8}}), code: "block_props_mismatch", blockID: "m", field: "kind"},
		{name: "padding", doc: docWith(Block{ID: "p", Kind: KindHeading, Padding: "xl", Heading: &Heading{Text: "a", Level: 1}}), code: "padding_invalid", blockID: "p", field: "padding"},
		{name: "padding spacer", doc: docWith(Block{ID: "p", Kind: KindSpacer, Padding: PaddingSmall, Spacer: &Spacer{Height: 8}}), code: "padding_not_supported", blockID: "p", field: "padding"},
		{name: "block background", doc: docWith(Block{ID: "bg", Kind: KindHeading, BackgroundColor: "blue", Heading: &Heading{Text: "a", Level: 1}}), code: "color_invalid", blockID: "bg", field: "background_color"},
		{name: "align", doc: docWith(Block{ID: "al", Kind: KindHeading, Heading: &Heading{Text: "a", Level: 1, Align: "justify"}}), code: "align_invalid", blockID: "al", field: "align"},
		{name: "heading empty", doc: docWith(headingBlock("h", "  ")), code: "text_empty", blockID: "h", field: "text"},
		{name: "heading long", doc: docWith(headingBlock("h", strings.Repeat("a", 201))), code: "text_too_long", blockID: "h", field: "text"},
		{name: "heading level", doc: docWith(Block{ID: "h", Kind: KindHeading, Heading: &Heading{Text: "a", Level: 4}}), code: "heading_level_invalid", blockID: "h", field: "level"},
		{name: "heading level zero", doc: docWith(Block{ID: "h", Kind: KindHeading, Heading: &Heading{Text: "a"}}), code: "heading_level_invalid", blockID: "h", field: "level"},
		{name: "heading color", doc: docWith(Block{ID: "h", Kind: KindHeading, Heading: &Heading{Text: "a", Level: 2, Color: "#xyz"}}), code: "color_invalid", blockID: "h", field: "color"},
		{name: "heading tag", doc: docWith(headingBlock("h", "Oi {{nome}}")), code: "unknown_merge_tag", blockID: "h", field: "text"},
		{name: "heading tag malformed", doc: docWith(headingBlock("h", "Oi {{first_name")), code: "merge_tag_malformed", blockID: "h", field: "text"},
		{name: "heading tag empty", doc: docWith(headingBlock("h", "Oi {{}}")), code: "merge_tag_malformed", blockID: "h", field: "text"},
		{name: "field key invalid", doc: docWith(headingBlock("h", "Oi {{field.Plano}}")), code: "unknown_merge_tag", blockID: "h", field: "text"},
		{name: "text no paragraphs", doc: docWith(Block{ID: "t", Kind: KindText, Text: &Text{}}), code: "text_empty", blockID: "t", field: "paragraphs"},
		{name: "run empty", doc: docWith(textBlock("t", Run{Text: ""})), code: "run_text_empty", blockID: "t", field: "paragraphs.0.runs.0.text"},
		{name: "text long", doc: docWith(textBlock("t", Run{Text: strings.Repeat("a", 5001)})), code: "text_too_long", blockID: "t", field: "paragraphs"},
		{name: "run javascript", doc: docWith(textBlock("t", Run{Text: "x", Link: &Link{Href: "javascript:alert(1)"}})), code: "link_scheme_invalid", blockID: "t", field: "paragraphs.0.runs.0.link.href"},
		{name: "run link empty", doc: docWith(textBlock("t", Run{Text: "x", Link: &Link{}})), code: "href_missing", blockID: "t", field: "paragraphs.0.runs.0.link.href"},
		{name: "run link host", doc: docWith(textBlock("t", Run{Text: "x", Link: &Link{Href: "https://"}})), code: "link_host_missing", blockID: "t", field: "paragraphs.0.runs.0.link.href"},
		{name: "run tag", doc: docWith(textBlock("t", Run{Text: "{{idade}}"})), code: "unknown_merge_tag", blockID: "t", field: "paragraphs.0.runs.0.text"},
		{name: "text align", doc: docWith(Block{ID: "t", Kind: KindText, Text: &Text{Align: "middle", Paragraphs: []Paragraph{{Runs: []Run{{Text: "a"}}}}}}), code: "align_invalid", blockID: "t", field: "align"},
		{name: "button label", doc: docWith(buttonBlock("b", "", "https://example.com")), code: "text_empty", blockID: "b", field: "label"},
		{name: "button label long", doc: docWith(buttonBlock("b", strings.Repeat("a", 81), "https://example.com")), code: "text_too_long", blockID: "b", field: "label"},
		{name: "button href missing", doc: docWith(buttonBlock("b", "Ir", "")), code: "href_missing", blockID: "b", field: "href"},
		{name: "button ftp", doc: docWith(buttonBlock("b", "Ir", "ftp://example.com")), code: "link_scheme_invalid", blockID: "b", field: "href"},
		{name: "button href long", doc: docWith(buttonBlock("b", "Ir", "https://example.com/"+strings.Repeat("a", 2048))), code: "text_too_long", blockID: "b", field: "href"},
		{name: "button href tag", doc: docWith(buttonBlock("b", "Ir", "https://example.com/?e={{mail}}")), code: "unknown_merge_tag", blockID: "b", field: "href"},
		{name: "button href tag scheme", doc: docWith(buttonBlock("b", "Ir", "{{field.url}}")), code: "link_scheme_invalid", blockID: "b", field: "href"},
		{name: "button color", doc: docWith(Block{ID: "b", Kind: KindButton, Button: &Button{Label: "Ir", Href: "https://example.com", Color: "green"}}), code: "color_invalid", blockID: "b", field: "color"},
		{name: "button text color", doc: docWith(Block{ID: "b", Kind: KindButton, Button: &Button{Label: "Ir", Href: "https://example.com", TextColor: "#fffffff"}}), code: "color_invalid", blockID: "b", field: "text_color"},
		{name: "button align", doc: docWith(Block{ID: "b", Kind: KindButton, Button: &Button{Label: "Ir", Href: "https://example.com", Align: "top"}}), code: "align_invalid", blockID: "b", field: "align"},
		{name: "image src missing", doc: docWith(imageBlock("i", "", "x")), code: "image_src_missing", blockID: "i", field: "src"},
		{name: "image src http", doc: docWith(imageBlock("i", "http://example.com/a.png", "x")), code: "image_src_not_https", blockID: "i", field: "src"},
		{name: "image src data", doc: docWith(imageBlock("i", "data:image/png;base64,AAAA", "x")), code: "image_src_not_https", blockID: "i", field: "src"},
		{name: "image src no host", doc: docWith(imageBlock("i", "https:///a.png", "x")), code: "image_src_not_https", blockID: "i", field: "src"},
		{name: "image width low", doc: docWith(Block{ID: "i", Kind: KindImage, Image: &Image{Src: "https://example.com/a.png", Width: 5}}), code: "image_width_invalid", blockID: "i", field: "width"},
		{name: "image width high", doc: docWith(Block{ID: "i", Kind: KindImage, Image: &Image{Src: "https://example.com/a.png", Width: 101}}), code: "image_width_invalid", blockID: "i", field: "width"},
		{name: "image href", doc: docWith(Block{ID: "i", Kind: KindImage, Image: &Image{Src: "https://example.com/a.png", Width: 50, Href: "data:text/html,x"}}), code: "link_scheme_invalid", blockID: "i", field: "href"},
		{name: "image alt long", doc: docWith(imageBlock("i", "https://example.com/a.png", strings.Repeat("a", 301))), code: "text_too_long", blockID: "i", field: "alt"},
		{name: "divider thickness low", doc: docWith(Block{ID: "d", Kind: KindDivider, Divider: &Divider{Thickness: 0}}), code: "divider_thickness_invalid", blockID: "d", field: "thickness"},
		{name: "divider thickness high", doc: docWith(Block{ID: "d", Kind: KindDivider, Divider: &Divider{Thickness: 9}}), code: "divider_thickness_invalid", blockID: "d", field: "thickness"},
		{name: "divider color", doc: docWith(Block{ID: "d", Kind: KindDivider, Divider: &Divider{Thickness: 1, Color: "#1"}}), code: "color_invalid", blockID: "d", field: "color"},
		{name: "spacer low", doc: docWith(Block{ID: "s", Kind: KindSpacer, Spacer: &Spacer{Height: 3}}), code: "spacer_height_invalid", blockID: "s", field: "height"},
		{name: "spacer high", doc: docWith(Block{ID: "s", Kind: KindSpacer, Spacer: &Spacer{Height: 121}}), code: "spacer_height_invalid", blockID: "s", field: "height"},
		{name: "columns ratio", doc: docWith(columnsBlock("c", "40-60", []Block{spacerBlock("a")}, []Block{spacerBlock("b")})), code: "columns_ratio_invalid", blockID: "c", field: "ratio"},
		{name: "columns count", doc: docWith(columnsBlock("c", Ratio5050, []Block{spacerBlock("a")}, []Block{spacerBlock("b")}, []Block{spacerBlock("d")})), code: "columns_count_mismatch", blockID: "c", field: "columns"},
		{name: "columns count three", doc: docWith(columnsBlock("c", Ratio333333, []Block{spacerBlock("a")}, []Block{spacerBlock("b")})), code: "columns_count_mismatch", blockID: "c", field: "columns"},
		{name: "nested columns", doc: docWith(columnsBlock("c", Ratio3367, []Block{columnsBlock("inner", Ratio5050, nil, nil)}, []Block{spacerBlock("b")})), code: "nested_columns", blockID: "inner", field: "kind"},
		{name: "footer in columns", doc: docWith(columnsBlock("c", Ratio6733, []Block{{ID: "f2", Kind: KindFooter, Footer: &Footer{CompanyName: "a", Address: "b"}}}, []Block{spacerBlock("b")})), code: "footer_in_columns", blockID: "f2", field: "kind"},
		{name: "column child invalid", doc: docWith(columnsBlock("c", Ratio5050, []Block{headingBlock("ch", "")}, []Block{spacerBlock("b")})), code: "text_empty", blockID: "ch", field: "text"},
		{name: "column child id duplicate", doc: docWith(spacerBlock("dup"), columnsBlock("c", Ratio5050, []Block{spacerBlock("dup")}, []Block{spacerBlock("b")})), code: "block_id_duplicate", blockID: "dup", field: "id"},
		{name: "social empty", doc: docWith(Block{ID: "so", Kind: KindSocial, Social: &Social{}}), code: "social_links_empty", blockID: "so", field: "links"},
		{name: "social network", doc: docWith(Block{ID: "so", Kind: KindSocial, Social: &Social{Links: []SocialLink{{Network: "myspace", URL: "https://myspace.com/a"}}}}), code: "social_network_invalid", blockID: "so", field: "links.0.network"},
		{name: "social url", doc: docWith(Block{ID: "so", Kind: KindSocial, Social: &Social{Links: []SocialLink{{Network: NetworkFacebook, URL: "http://facebook.com/a"}}}}), code: "social_url_not_https", blockID: "so", field: "links.0.url"},
		{name: "social label long", doc: docWith(Block{ID: "so", Kind: KindSocial, Social: &Social{Links: []SocialLink{{Network: NetworkX, URL: "https://x.com/a", Label: strings.Repeat("a", 51)}}}}), code: "text_too_long", blockID: "so", field: "links.0.label"},
		{name: "social align", doc: docWith(Block{ID: "so", Kind: KindSocial, Social: &Social{Align: "end", Links: []SocialLink{{Network: NetworkX, URL: "https://x.com/a"}}}}), code: "align_invalid", blockID: "so", field: "align"},
		{name: "footer missing", doc: Document{Version: CurrentVersion, Blocks: []Block{headingBlock("h", "a")}}, code: "footer_missing", field: "blocks"},
		{name: "footer missing empty", doc: Document{Version: CurrentVersion}, code: "footer_missing", field: "blocks"},
		{name: "footer duplicate", doc: Document{Version: CurrentVersion, Blocks: []Block{{ID: "f1", Kind: KindFooter, Footer: &Footer{CompanyName: "a", Address: "b"}}, footerBlock()}}, code: "footer_duplicate", blockID: "footer", field: "kind"},
		{name: "footer not last", doc: Document{Version: CurrentVersion, Blocks: []Block{footerBlock(), headingBlock("h", "a")}}, code: "footer_not_last", blockID: "footer", field: "kind"},
		{name: "footer company", doc: Document{Version: CurrentVersion, Blocks: []Block{{ID: "f", Kind: KindFooter, Footer: &Footer{Address: "b"}}}}, code: "footer_company_missing", blockID: "f", field: "company_name"},
		{name: "footer address", doc: Document{Version: CurrentVersion, Blocks: []Block{{ID: "f", Kind: KindFooter, Footer: &Footer{CompanyName: "a", Address: " "}}}}, code: "footer_address_missing", blockID: "f", field: "address"},
		{name: "footer text long", doc: Document{Version: CurrentVersion, Blocks: []Block{{ID: "f", Kind: KindFooter, Footer: &Footer{CompanyName: "a", Address: "b", Text: strings.Repeat("a", 1001)}}}}, code: "text_too_long", blockID: "f", field: "text"},
		{name: "footer text tag", doc: Document{Version: CurrentVersion, Blocks: []Block{{ID: "f", Kind: KindFooter, Footer: &Footer{CompanyName: "a", Address: "b", Text: "{{x}}"}}}}, code: "unknown_merge_tag", blockID: "f", field: "text"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := Validate(tc.doc)
			for _, issue := range issues {
				if issue.Code == tc.code && issue.BlockID == tc.blockID && issue.Field == tc.field {
					return
				}
			}
			t.Fatalf("expected issue %s on %q field %q, got %+v", tc.code, tc.blockID, tc.field, issues)
		})
	}
}

func TestValidateIsDeterministic(t *testing.T) {
	doc := docWith(headingBlock("", ""), Block{ID: "v", Kind: "video"}, imageBlock("i", "http://a", ""))
	first := Validate(doc)
	for i := 0; i < 20; i++ {
		again := Validate(doc)
		if len(again) != len(first) {
			t.Fatalf("issue count changed: %d vs %d", len(again), len(first))
		}
		for j := range first {
			if first[j] != again[j] {
				t.Fatalf("issue order changed at %d: %+v vs %+v", j, first[j], again[j])
			}
		}
	}
}

func TestValidateSubject(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		want    []string
	}{
		{name: "valid", subject: "Oferta para {{first_name|voce}}", want: nil},
		{name: "empty", subject: "", want: []string{"subject_empty"}},
		{name: "blank", subject: "   ", want: []string{"subject_empty"}},
		{name: "too long", subject: strings.Repeat("a", 151), want: []string{"subject_too_long"}},
		{name: "max length", subject: strings.Repeat("a", 150), want: nil},
		{name: "unknown tag", subject: "Oi {{apelido}}", want: []string{"unknown_merge_tag"}},
		{name: "malformed tag", subject: "Oi {{name", want: []string{"merge_tag_malformed"}},
		{name: "newline", subject: "Oi\nTchau", want: []string{"subject_invalid_characters"}},
		{name: "carriage return", subject: "Oi\rTchau", want: []string{"subject_invalid_characters"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := ValidateSubject(tc.subject)
			if len(issues) != len(tc.want) {
				t.Fatalf("expected %v, got %+v", tc.want, issues)
			}
			for i, code := range tc.want {
				if issues[i].Code != code || issues[i].Field != "subject" {
					t.Fatalf("expected %s on subject, got %+v", code, issues[i])
				}
			}
		})
	}
}

func TestDraftGapsAreTheOnlyIssuesADraftMayKeep(t *testing.T) {
	gaps := docWith(
		buttonBlock("cta", "Comprar", ""),
		imageBlock("hero", "", ""),
		Block{ID: "title", Kind: KindHeading, Heading: &Heading{Text: "", Level: 1}},
		Block{ID: "social", Kind: KindSocial, Social: &Social{Links: []SocialLink{{Network: NetworkInstagram}}}},
		textBlock("tag", Run{Text: "Oi {{apelido}}"}),
	)
	gaps.Blocks[len(gaps.Blocks)-1].Footer.CompanyName = ""
	issues := Validate(gaps)
	for _, code := range []string{"href_missing", "image_src_missing", "text_empty", "social_url_missing", "unknown_merge_tag", "footer_company_missing"} {
		if !hasCode(issues, code) {
			t.Fatalf("missing %s in %+v", code, issues)
		}
	}
	if broken := StructuralIssues(issues); len(broken) != 0 {
		t.Fatalf("a draft with gaps is still a valid document, got %+v", broken)
	}

	broken := docWith(
		buttonBlock("cta", "Comprar", "ftp://x.com"),
		imageBlock("hero", "http://cdn.example.com/a.jpg", "A"),
		Block{ID: "social", Kind: KindSocial, Social: &Social{Links: []SocialLink{{Network: NetworkInstagram, URL: "http://instagram.com/x"}}}},
	)
	got := StructuralIssues(Validate(broken))
	for _, code := range []string{"link_scheme_invalid", "image_src_not_https", "social_url_not_https"} {
		if !hasCode(got, code) {
			t.Fatalf("a given URL that is not https makes the document invalid: missing %s in %+v", code, got)
		}
	}
}

func hasCode(issues []Issue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
