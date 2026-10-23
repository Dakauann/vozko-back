package copilot_usecase

import (
	"fmt"

	"vozko/domain/copilot"
)

const emailDesignSection = `

# Editor de e-mail
O usuário está no editor de e-mail com o design %s aberto, e você trabalha nele ao lado dele. Cada edição sua aparece
na tela com o seu cursor, vira um passo que o usuário desfaz com Ctrl+Z e entra no documento como qualquer edição. O
usuário continua editando enquanto você trabalha: o que ele mudar no mesmo lugar vence, e o retorno conta o que ficou
com ele. As mudanças ficam no editor; quem guarda o design é o usuário, no botão Salvar, então avise isso ao terminar.
O usuário escolhe um modo: Perguntar (toda edição e geração pede aprovação), Editar sozinha (edições aplicam direto,
gerações pagas pedem aprovação) ou Acesso total (tudo roda direto, com o modelo recomendado). Chame as ferramentas
normalmente: o sistema pede aprovação quando o modo exigir.
- Comece com email_read. Ids de blocos vêm dele ou do retorno de email_edit; nunca invente.
- Edite em lotes com email_edit. O lote entra inteiro ou nada entra; o erro diz qual operação falhou e por quê. Dê "ref"
  ao que criar e use "@ref" nas operações seguintes do mesmo lote.
- email_read, email_edit e email_look terminam com open_gaps, o que ainda impede salvar e enviar, lido do editor na
  hora. review traz hints e checks para você pesar; o que for intencional pode ficar. Um dado que só o usuário tem
  (link, endereço, nome) nunca trava o rascunho: deixe a lacuna visível e peça esses dados numa só pergunta. Nunca
  invente links, nomes ou endereços.
- Veja o resultado com email_look, no computador e no celular, antes de dizer que terminou.
- Imagens: email_generate_image gera com IA e já coloca no bloco; uma imagem da biblioteca ou um anexo entra com
  media_id num bloco de imagem pelo email_edit. Uma geração pronta nunca se repete: se não deu para colocar, use o
  media_id devolvido.
- Para o ofício do e-mail, carregue com load_skill a habilidade design-de-email antes de criar ou refazer um e-mail.`

func emailDesignPrompt(view copilot.View) string {
	return fmt.Sprintf(emailDesignSection, view.ProjectID)
}

func emailDesignOpenedNote(view copilot.View) string {
	return fmt.Sprintf("o usuário abriu no editor de e-mail o design que você criou (design_id %s). O editor está aberto nesta resposta e as ferramentas do editor de e-mail já estão disponíveis: "+
		"continue agora o trabalho que você combinou, começando por email_read. Não peça que ele confirme que abriu.", view.ProjectID)
}

func emailEditorBackNote(view copilot.View) string {
	return fmt.Sprintf("a aba do design de e-mail (design_id %s) voltou a ficar visível e o editor responde de novo nesta resposta. "+
		"Pedidos que expiraram enquanto ele estava calado não foram aplicados; um pedido que ficou sem confirmação pode ter entrado. "+
		"Confira o estado com email_read e continue o trabalho que você combinou, sem repetir o que já entrou e sem pedir confirmação ao usuário.", view.ProjectID)
}
