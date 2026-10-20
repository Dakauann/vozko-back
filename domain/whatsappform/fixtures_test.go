package whatsappform

func textField(key, label string) *Field {
	return &Field{Key: key, Kind: FieldShortText, Label: label, Required: true}
}

func choiceField(key string, kind FieldKind, ids ...string) *Field {
	f := &Field{Key: key, Kind: kind, Label: "Escolha", Required: true}
	for _, id := range ids {
		f.Options = append(f.Options, Option{ID: id, Title: "Opção " + id})
	}
	return f
}

func fieldBlock(f *Field) Block {
	return Block{Key: "b_" + f.Key, Field: f}
}

func displayBlock(key string, kind DisplayKind, text string) Block {
	return Block{Key: key, Display: &Display{Kind: kind, Text: text}}
}

func oneScreen(blocks ...Block) Document {
	return Document{
		Schema:     DocumentSchema,
		Categories: []Category{CategoryLeadGeneration},
		Screens:    []Screen{{Key: "s_one", Title: "Seus dados", Blocks: blocks, SubmitLabel: "Enviar"}},
	}
}

func twoScreens() Document {
	return Document{
		Schema:     DocumentSchema,
		Categories: []Category{CategoryLeadGeneration},
		Screens: []Screen{
			{Key: "s_one", Title: "Contato", Blocks: []Block{fieldBlock(textField("f_name", "Nome"))}, Route: Route{Next: "s_two"}, SubmitLabel: "Continuar"},
			{Key: "s_two", Title: "Interesse", Blocks: []Block{fieldBlock(choiceField("f_topic", FieldRadio, "o_a", "o_b"))}, SubmitLabel: "Enviar"},
		},
	}
}

func hasProblem(problems Problems, block, property, code string) bool {
	for _, p := range problems {
		if p.Block == block && p.Property == property && p.Code == code {
			return true
		}
	}
	return false
}
