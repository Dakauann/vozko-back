---
name: formularios-de-cadastro
description: Anúncios com formulário instantâneo da Meta (cadastro sem sair do Facebook ou do Instagram): quando usar, como montar o formulário, qualidade dos cadastros e retorno rápido; carregue antes de criar ou avaliar um anúncio de cadastro.
---

# Formulários de cadastro (formulário instantâneo)

## Quando usar
- Quando o negócio quer volume de contatos para ligar ou mandar mensagem depois: seguros, cursos, imóveis, consórcio, serviços com venda consultiva.
- Se a venda acontece conversando, compare com o anúncio de WhatsApp (anuncios-para-whatsapp): conversa traz menos contatos, porém mais quentes.
- Objetivo OUTCOME_LEADS com destino ON_AD e o lead_form_id do formulário.

## Volume ou intenção
A Meta oferece dois tipos de formulário: mais volume (rápido de enviar no celular) e maior intenção (com uma tela para a pessoa revisar os dados). Mais volume traz mais cadastros e mais curiosos; maior intenção traz menos e melhores. O tipo, as perguntas personalizadas e as respostas condicionais são definidos ao criar o formulário na Meta.

## Perguntas
- Peça o mínimo que o negócio precisa para o próximo passo. Cada pergunta a mais reduz os envios.
- Campos que a Meta já preenche com o perfil (nome, e-mail, telefone, cidade) quase não reduzem envios.
- Para qualificar, perguntas de múltipla escolha são melhores que respostas digitadas: menos perguntas de escolha dão mais cadastros, mais perguntas dão cadastros mais qualificados.
- Respostas condicionais servem quando há vários produtos ou opções que dependem da resposta anterior.
- Pelo create_lead_form você monta formulários com os campos padrão (nome, e-mail, telefone, cidade, estado, empresa, cargo). Para perguntas de escolha, respostas condicionais ou o tipo maior intenção, o usuário cria o formulário na Meta e você usa o lead_form_id de list_lead_forms.

## Telas do formulário
- Abertura: diga em uma frase o que a pessoa recebe ao se cadastrar (orçamento, contato de um especialista, material).
- Política de privacidade da empresa é obrigatória: peça o link ao usuário, nunca invente.
- Tela final: agradeça, diga quando e por onde o contato vai acontecer e ofereça um próximo passo (site ou WhatsApp).

## Depois do cadastro
- Cadastro esfria em minutos: contate o quanto antes, idealmente na primeira hora. Veja funil-e-qualificacao.
- No Vozko os cadastros chegam em Formulários e leads (a busca na Meta roda a cada 15 minutos). Combine com o usuário quem liga ou manda mensagem e em quanto tempo.
- Avalie pelo custo por cadastro qualificado, não só pelo custo por cadastro. Com conversões do CRM ligadas (get_ad_conversion_settings), a Meta aprende quem vira oportunidade e venda.

## Erros comuns
- Formulário longo demais ou com muitas respostas digitadas.
- Promessa no anúncio que o contato depois não cumpre.
- Página sem os termos de cadastro aceitos: o formulário não publica (leve com open_screen ads_forms).
