package emaildesign

type Starter struct {
	Key      string   `json:"key"`
	Document Document `json:"document"`
}

func StarterDocuments() []Starter {
	return []Starter{
		{Key: "blank", Document: blankStarter()},
		{Key: "newsletter", Document: newsletterStarter()},
		{Key: "promotion", Document: promotionStarter()},
		{Key: "announcement", Document: announcementStarter()},
		{Key: "event", Document: eventStarter()},
	}
}

func starterSettings(accent, preheader string) Settings {
	return Settings{
		Width:                  DefaultWidth,
		BackgroundColor:        "#f4f4f5",
		ContentBackgroundColor: "#ffffff",
		TextColor:              "#1f2937",
		LinkColor:              accent,
		ButtonColor:            accent,
		ButtonTextColor:        "#ffffff",
		FontFamily:             "inter",
		Preheader:              preheader,
	}
}

func starterFooter() Block {
	return Block{ID: "footer", Kind: KindFooter, Footer: &Footer{
		CompanyName: "Sua empresa",
		Address:     "Endereço da sua empresa",
		Text:        "Você recebe este email porque se cadastrou para receber nossas novidades.",
	}}
}

func starterHeading(id, text string, level int, align Align) Block {
	return Block{ID: id, Kind: KindHeading, Heading: &Heading{Text: text, Level: level, Align: align}}
}

func starterText(id string, align Align, paragraphs ...string) Block {
	ps := make([]Paragraph, 0, len(paragraphs))
	for _, p := range paragraphs {
		ps = append(ps, Paragraph{Runs: []Run{{Text: p}}})
	}
	return Block{ID: id, Kind: KindText, Text: &Text{Paragraphs: ps, Align: align}}
}

func starterButton(id, label string, fullWidth bool) Block {
	return Block{ID: id, Kind: KindButton, Button: &Button{Label: label, Href: "https://example.com", Align: AlignCenter, FullWidth: fullWidth}}
}

func starterDivider(id string) Block {
	return Block{ID: id, Kind: KindDivider, Divider: &Divider{Color: "#e5e7eb", Thickness: 1}}
}

func starterSpacer(id string, height int) Block {
	return Block{ID: id, Kind: KindSpacer, Spacer: &Spacer{Height: height}}
}

func blankStarter() Document {
	return Document{
		Version:  CurrentVersion,
		Settings: starterSettings("#2563eb", ""),
		Blocks: []Block{
			starterText("text", AlignLeft, "Escreva sua mensagem aqui."),
			starterFooter(),
		},
	}
}

func newsletterStarter() Document {
	return Document{
		Version:  CurrentVersion,
		Settings: starterSettings("#2563eb", "As novidades que separamos para você este mês"),
		Blocks: []Block{
			starterSpacer("top", 16),
			starterHeading("title", "Novidades do mês", 1, AlignLeft),
			starterText("intro", AlignLeft, "Olá, {{first_name|cliente}}! Separamos o que aconteceu de mais importante por aqui."),
			starterDivider("divider-1"),
			starterHeading("highlight-title", "Destaque", 2, AlignLeft),
			starterText("highlight-text", AlignLeft, "Conte em poucas linhas a principal notícia desta edição e por que ela importa para quem lê."),
			starterButton("highlight-button", "Leia mais", false),
			starterDivider("divider-2"),
			{ID: "stories", Kind: KindColumns, Columns: &Columns{Ratio: Ratio5050, Columns: []Column{
				{Blocks: []Block{
					starterHeading("story-1-title", "Segunda notícia", 3, AlignLeft),
					starterText("story-1-text", AlignLeft, "Um resumo curto com o essencial."),
				}},
				{Blocks: []Block{
					starterHeading("story-2-title", "Terceira notícia", 3, AlignLeft),
					starterText("story-2-text", AlignLeft, "Outro resumo curto com o essencial."),
				}},
			}}},
			starterSpacer("bottom", 16),
			{ID: "social", Kind: KindSocial, Social: &Social{Align: AlignCenter, Links: []SocialLink{
				{Network: NetworkInstagram, URL: "https://instagram.com"},
				{Network: NetworkFacebook, URL: "https://facebook.com"},
				{Network: NetworkLinkedIn, URL: "https://linkedin.com"},
			}}},
			starterFooter(),
		},
	}
}

func promotionStarter() Document {
	return Document{
		Version:  CurrentVersion,
		Settings: starterSettings("#dc2626", "Uma oferta por tempo limitado"),
		Blocks: []Block{
			starterSpacer("top", 24),
			starterHeading("eyebrow", "Oferta especial para você", 3, AlignCenter),
			{ID: "offer", Kind: KindHeading, Padding: PaddingLarge, BackgroundColor: "#fef2f2", Heading: &Heading{Text: "20% de desconto", Level: 1, Align: AlignCenter, Color: "#dc2626"}},
			starterText("details", AlignCenter, "{{first_name|Cliente}}, use o cupom OFERTA20 na finalização da compra.", "Válido enquanto durarem os estoques."),
			starterButton("cta", "Aproveitar agora", true),
			starterSpacer("gap", 8),
			starterText("terms", AlignCenter, "Desconto não cumulativo com outras promoções."),
			starterFooter(),
		},
	}
}

func announcementStarter() Document {
	return Document{
		Version:  CurrentVersion,
		Settings: starterSettings("#0f766e", "Temos uma novidade para contar"),
		Blocks: []Block{
			starterSpacer("top", 24),
			starterHeading("title", "Temos uma novidade", 1, AlignLeft),
			starterText("body", AlignLeft,
				"Olá, {{first_name|cliente}}.",
				"Escreva aqui o que mudou, quando começa a valer e o que a pessoa precisa fazer, se precisar fazer algo.",
			),
			starterButton("cta", "Saiba mais", false),
			starterDivider("divider"),
			starterText("help", AlignLeft, "Ficou com alguma dúvida? Responda este email que a nossa equipe ajuda."),
			starterFooter(),
		},
	}
}

func eventStarter() Document {
	return Document{
		Version:  CurrentVersion,
		Settings: starterSettings("#7c3aed", "Reserve a data: você está convidado"),
		Blocks: []Block{
			starterSpacer("top", 24),
			starterHeading("title", "Você está convidado", 1, AlignCenter),
			starterText("intro", AlignCenter, "{{first_name|Olá}}, preparamos um encontro especial e queremos você com a gente."),
			{ID: "details", Kind: KindColumns, BackgroundColor: "#f5f3ff", Columns: &Columns{Ratio: Ratio5050, Columns: []Column{
				{Blocks: []Block{
					starterHeading("date-title", "Quando", 3, AlignCenter),
					starterText("date-text", AlignCenter, "Dia e horário do evento"),
				}},
				{Blocks: []Block{
					starterHeading("place-title", "Onde", 3, AlignCenter),
					starterText("place-text", AlignCenter, "Endereço ou link da transmissão"),
				}},
			}}},
			starterSpacer("gap", 16),
			starterButton("cta", "Confirmar presença", false),
			starterText("note", AlignCenter, "As vagas são limitadas."),
			starterFooter(),
		},
	}
}
