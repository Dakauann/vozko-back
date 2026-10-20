package whatsappform

import "encoding/json"

type Starter struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Document    Document `json:"document"`
}

var brazilianStates = []string{
	"AC", "AL", "AP", "AM", "BA", "CE", "DF", "ES", "GO", "MA", "MT", "MS", "MG", "PA",
	"PB", "PR", "PE", "PI", "RJ", "RN", "RS", "RO", "RR", "SC", "SP", "SE", "TO",
}

func options(titles ...string) []Option {
	out := make([]Option, 0, len(titles))
	for i, title := range titles {
		out = append(out, Option{ID: "o_" + string(rune('a'+i/26)) + string(rune('a'+i%26)), Title: title})
	}
	return out
}

func input(key string, kind FieldKind, label string, required bool, binding BindingTarget) Block {
	f := &Field{Key: key, Kind: kind, Label: label, Required: required}
	if binding != "" {
		f.Binding = &Binding{Target: binding}
	}
	return Block{Key: "b_" + key, Field: f}
}

func choice(key string, kind FieldKind, label string, opts []Option, binding BindingTarget) Block {
	b := input(key, kind, label, true, binding)
	b.Field.Options = opts
	return b
}

func asked(b Block, question string) Block {
	b.Field.Question = question
	return b
}

func heading(key, text string) Block {
	return Block{Key: key, Display: &Display{Kind: DisplayHeading, Text: text}}
}

func paragraph(key, text string) Block {
	return Block{Key: key, Display: &Display{Kind: DisplayParagraph, Text: text}}
}

func consent(key string) Block {
	return input(key, FieldOptIn, "Aceito receber mensagens desta empresa pelo WhatsApp.", false, BindWhatsAppConsent)
}

func screen(key, title, submit string, next string, blocks ...Block) Screen {
	return Screen{Key: key, Title: title, Blocks: blocks, Route: Route{Next: next}, SubmitLabel: submit}
}

func doc(category Category, screens ...Screen) Document {
	return Document{Schema: DocumentSchema, Categories: []Category{category}, Screens: screens}
}

func BlankDocument() Document {
	return doc(CategoryOther, screen("s_inicio", "Formulário", "Enviar", "",
		input("f_nome", FieldShortText, "Nome", true, BindName)))
}

var starterCatalog = []Starter{
	{
		Key: "lead_capture", Name: "Captação de lead",
		Description: "Nome, e-mail e interesse de quem chegou pelo WhatsApp, com o aceite de mensagens.",
		Document: doc(CategoryLeadGeneration,
			screen("s_contato", "Seus dados", "Continuar", "s_interesse",
				heading("b_titulo", "Que bom ter você aqui"),
				asked(input("f_nome", FieldShortText, "Nome", true, BindName), "Como podemos te chamar?"),
				input("f_email", FieldEmail, "E-mail", true, BindEmail)),
			screen("s_interesse", "Seu interesse", "Enviar", "",
				choice("f_interesse", FieldRadio, "O que você procura?", options("Conhecer os produtos", "Falar com vendas", "Suporte"), ""),
				consent("f_aceite"))),
	},
	{
		Key: "appointment", Name: "Agendamento",
		Description: "Pedido de horário com data, período e observações.",
		Document: doc(CategoryAppointmentBooking,
			screen("s_agenda", "Agendar horário", "Solicitar", "",
				input("f_nome", FieldShortText, "Nome", true, BindName),
				asked(input("f_data", FieldDate, "Data", true, ""), "Qual dia é melhor para você?"),
				choice("f_periodo", FieldRadio, "Período", options("Manhã", "Tarde", "Noite"), ""),
				input("f_obs", FieldLongText, "Observações", false, ""))),
	},
	{
		Key: "nps", Name: "Pesquisa de satisfação",
		Description: "Nota de 0 a 10 e um comentário, no formato NPS.",
		Document: doc(CategorySurvey,
			screen("s_nota", "Sua opinião", "Enviar", "",
				paragraph("b_intro", "Leva menos de um minuto."),
				choice("f_nota", FieldDropdown, "Nota de 0 a 10", options("0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10"), ""),
				asked(input("f_comentario", FieldLongText, "Comentário", false, ""), "O que mais influenciou a sua nota?"))),
	},
	{
		Key: "sign_up", Name: "Cadastro",
		Description: "Dados de cadastro com nascimento e endereço por CEP.",
		Document: doc(CategorySignUp,
			screen("s_pessoais", "Dados pessoais", "Continuar", "s_endereco",
				input("f_nome", FieldShortText, "Nome completo", true, BindName),
				input("f_email", FieldEmail, "E-mail", true, BindEmail),
				input("f_nascimento", FieldDate, "Data de nascimento", false, BindBirthDate)),
			screen("s_endereco", "Endereço", "Concluir", "",
				input("f_cep", FieldShortText, "CEP", false, BindAddressZip),
				input("f_cidade", FieldShortText, "Cidade", false, BindAddressCity),
				choice("f_estado", FieldDropdown, "Estado", options(brazilianStates...), BindAddressState),
				consent("f_aceite"))),
	},
	{
		Key: "contact_us", Name: "Fale conosco",
		Description: "Assunto e mensagem para a equipe responder.",
		Document: doc(CategoryContactUs,
			screen("s_contato", "Fale conosco", "Enviar", "",
				input("f_nome", FieldShortText, "Nome", true, BindName),
				choice("f_assunto", FieldDropdown, "Assunto", options("Dúvida", "Sugestão", "Reclamação", "Outro"), ""),
				input("f_mensagem", FieldLongText, "Mensagem", true, ""))),
	},
	{
		Key: "quote", Name: "Orçamento",
		Description: "Pedido de orçamento com o que a pessoa precisa, quantidade e prazo.",
		Document: doc(CategoryLeadGeneration,
			screen("s_pedido", "Pedido de orçamento", "Continuar", "s_contato",
				asked(input("f_descricao", FieldLongText, "Descrição", true, ""), "Do que você precisa?"),
				input("f_quantidade", FieldNumber, "Quantidade", false, ""),
				input("f_prazo", FieldDate, "Prazo desejado", false, "")),
			screen("s_contato", "Seus dados", "Enviar", "",
				input("f_nome", FieldShortText, "Nome", true, BindName),
				input("f_email", FieldEmail, "E-mail", true, BindEmail),
				consent("f_aceite"))),
	},
	{
		Key: "documents", Name: "Envio de documentos",
		Description: "Documento em PDF ou imagem e uma foto, cada um na sua tela.",
		Document: doc(CategoryOther,
			screen("s_documento", "Documento", "Continuar", "s_foto",
				input("f_nome", FieldShortText, "Nome", true, BindName),
				func() Block {
					b := input("f_documento", FieldDocument, "Documento", true, "")
					b.Field.Help = "PDF ou imagem"
					b.Field.MimeTypes = []string{"application/pdf", "image/jpeg", "image/png"}
					return b
				}()),
			screen("s_foto", "Foto", "Enviar", "",
				input("f_foto", FieldPhoto, "Foto", false, ""))),
	},
}

func CloneDocument(d Document) Document {
	raw, err := json.Marshal(d)
	if err != nil {
		panic(err)
	}
	var out Document
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}

func Starters() []Starter {
	out := make([]Starter, 0, len(starterCatalog))
	for _, s := range starterCatalog {
		s.Document = CloneDocument(s.Document)
		out = append(out, s)
	}
	return out
}

func StarterByKey(key string) (Starter, bool) {
	for _, s := range starterCatalog {
		if s.Key == key {
			s.Document = CloneDocument(s.Document)
			return s, true
		}
	}
	return Starter{}, false
}
