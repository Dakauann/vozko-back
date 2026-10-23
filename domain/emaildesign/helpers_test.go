package emaildesign

func footerBlock() Block {
	return Block{ID: "footer", Kind: KindFooter, Footer: &Footer{CompanyName: "Acme Ltda", Address: "Rua A, 1, Sao Paulo"}}
}

func textBlock(id string, runs ...Run) Block {
	return Block{ID: id, Kind: KindText, Text: &Text{Paragraphs: []Paragraph{{Runs: runs}}}}
}

func headingBlock(id, text string) Block {
	return Block{ID: id, Kind: KindHeading, Heading: &Heading{Text: text, Level: 1}}
}

func buttonBlock(id, label, href string) Block {
	return Block{ID: id, Kind: KindButton, Button: &Button{Label: label, Href: href}}
}

func imageBlock(id, src, alt string) Block {
	return Block{ID: id, Kind: KindImage, Image: &Image{Src: src, Alt: alt, Width: 100}}
}

func spacerBlock(id string) Block {
	return Block{ID: id, Kind: KindSpacer, Spacer: &Spacer{Height: 16}}
}

func columnsBlock(id string, ratio ColumnRatio, columns ...[]Block) Block {
	cols := make([]Column, 0, len(columns))
	for _, blocks := range columns {
		cols = append(cols, Column{Blocks: blocks})
	}
	return Block{ID: id, Kind: KindColumns, Columns: &Columns{Ratio: ratio, Columns: cols}}
}

func docWith(blocks ...Block) Document {
	return Document{Version: CurrentVersion, Blocks: append(blocks, footerBlock())}
}

func hasIssue(issues []Issue, code, blockID string) bool {
	for _, issue := range issues {
		if issue.Code == code && issue.BlockID == blockID {
			return true
		}
	}
	return false
}

func hasHint(hints []Hint, code, blockID string) bool {
	for _, hint := range hints {
		if hint.Code == code && hint.BlockID == blockID {
			return true
		}
	}
	return false
}

func fullDocument() Document {
	return Document{
		Version: CurrentVersion,
		Settings: Settings{
			Width:                  600,
			BackgroundColor:        "#f4f4f5",
			ContentBackgroundColor: "#ffffff",
			TextColor:              "#222",
			LinkColor:              "#2563eb",
			ButtonColor:            "#2563eb",
			ButtonTextColor:        "#ffffff",
			FontFamily:             "inter",
			Preheader:              "Novidades de {{first_name|cliente}}",
		},
		Blocks: []Block{
			{ID: "h1", Kind: KindHeading, Heading: &Heading{Text: "Oi {{first_name}}", Level: 1, Align: AlignCenter}},
			{ID: "t1", Kind: KindText, Padding: PaddingLarge, BackgroundColor: "#fafafa", Text: &Text{Paragraphs: []Paragraph{
				{Runs: []Run{{Text: "Bem-vindo "}, {Text: "a bordo", Bold: true}, {Text: " e ", Italic: true}, {Text: "veja", Underline: true}}},
				{Runs: []Run{{Text: "nosso site", Link: &Link{Href: "https://example.com/site"}}, {Text: " ou "}, {Text: "escreva", Link: &Link{Href: "mailto:contato@example.com"}}}},
			}}},
			{ID: "b1", Kind: KindButton, Button: &Button{Label: "Comprar agora", Href: "https://example.com/loja?ref=1", Align: AlignCenter}},
			{ID: "b2", Kind: KindButton, Button: &Button{Label: "Ligar", Href: "tel:+5511999999999", FullWidth: true, Color: "#16a34a", TextColor: "#fff"}},
			{ID: "img", Kind: KindImage, Image: &Image{Src: "https://cdn.example.com/a.png", Alt: "Produto", Href: "https://example.com/produto", Width: 80, Align: AlignCenter}},
			{ID: "d1", Kind: KindDivider, Divider: &Divider{Color: "#e5e7eb", Thickness: 1}},
			{ID: "s1", Kind: KindSpacer, Spacer: &Spacer{Height: 24}},
			columnsBlock("c1", Ratio5050,
				[]Block{headingBlock("c1h", "Esquerda"), textBlock("c1t", Run{Text: "Texto A"})},
				[]Block{buttonBlock("c1b", "Ver", "https://example.com/ver")},
			),
			columnsBlock("c2", Ratio333333,
				[]Block{textBlock("c2a", Run{Text: "Um"})},
				[]Block{textBlock("c2b", Run{Text: "Dois"})},
				[]Block{textBlock("c2c", Run{Text: "Tres"})},
			),
			{ID: "soc", Kind: KindSocial, Social: &Social{Links: []SocialLink{
				{Network: NetworkInstagram, URL: "https://instagram.com/acme"},
				{Network: NetworkWebsite, URL: "https://acme.example.com", Label: "Nosso site"},
			}}},
			{ID: "footer", Kind: KindFooter, Footer: &Footer{CompanyName: "Acme & Filhos", Address: "Rua <A>, 1", Text: "Voce recebe porque assinou, {{name|cliente}}."}},
		},
	}
}

func fullInput() RenderInput {
	return RenderInput{
		Personalization:  Personalization{FirstName: "Ana", Name: "Ana Souza", Email: "ana@example.com", Company: "Loja", Fields: map[string]string{"plano": "Ouro"}},
		Subject:          "Oferta para {{first_name}}",
		UnsubscribeURL:   "https://mail.example.com/u/abc123",
		UnsubscribeLabel: "Descadastrar",
	}
}
