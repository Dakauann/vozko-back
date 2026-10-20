---
name: formularios-whatsapp
surface: panel
description: Como montar formulários do WhatsApp (WhatsApp Flows) que as pessoas terminam: poucas perguntas, um assunto por tela, rótulos dentro dos limites da Meta, quando salvar a resposta no lead, aceite de mensagens e dados sensíveis; carregue antes de criar, publicar ou enviar um formulário.
---

# Formulários do WhatsApp

## Quando usar
- Formulário quando a resposta tem estrutura: cadastro, agendamento, orçamento, pesquisa de satisfação, envio de documento. Uma pergunta solta ou uma conversa aberta continua em mensagem normal.
- Comece de um starter quando um deles servir (lead_capture, appointment, nps, sign_up, contact_us, quote, documents) e ajuste o que o usuário pediu. Monte do zero só quando nenhum servir.

## Curto, um assunto por tela
- O mínimo de perguntas que resolve o pedido. Cada pergunta a mais derruba as respostas; pergunte só o que alguém vai usar.
- Uma tela por assunto (seus dados, o que você procura, quando), com poucos campos. O título da tela diz o assunto em poucas palavras.
- Ordem: o fácil primeiro (nome), o que qualifica depois, contato e aceite no fim.
- Escolha em vez de texto livre quando as respostas são previsíveis: escolha única para poucas opções, lista para listas longas (estados, horários), várias escolhas quando cabe mais de uma.
- Obrigatório só o indispensável; o resto fica opcional.
- O botão de cada tela (submitLabel) tem verbo: Continuar, Enviar, Solicitar, Agendar.
- Caminhos diferentes por resposta (branchOn) só quando o que vem depois muda de verdade. As rotas só seguem adiante.

## Rótulos dentro dos limites da Meta
- Texto curto, e-mail, telefone, número e texto longo: o label vai até 20 caracteres. A pergunta maior vai em question, o texto acima do campo, e o label fica com a palavra-chave. Exemplo: question "Como podemos te chamar?", label "Nome".
- Escolha única e várias escolhas: label até 30. Lista: 20. Data: 40. Aceite: 120. Título de cada opção: 30.
- help (até 80) serve para o formato esperado, por exemplo "Só números" ou "PDF ou foto".
- Títulos (heading) até 80; parágrafos podem ser longos, mas leitura no celular pede frases curtas.
- Quando create_whatsapp_form devolver um problema de tamanho, encurte o label e leve o resto para question.

## Salvar a resposta no lead
- Ligue a resposta a um campo do lead (binding) quando ela diz quem a pessoa é e alguém vai usar depois: nome, e-mail, nascimento, CEP, cidade, estado, aceite de mensagens ou um campo personalizado do workspace.
- Não ligue respostas do momento (o pedido de hoje, a nota da pesquisa, observações). Elas ficam na resposta e na conversa.
- O tipo da pergunta precisa combinar com o campo: e-mail com e-mail, data com data, escolha com um campo de opções iguais.
- Telefone nunca troca o número do lead; só vai para um campo de texto personalizado.
- Resposta sensível não é salva no lead.

## Aceite de mensagens
- Para mandar marketing pelo WhatsApp depois, inclua no fim um campo de aceite (opt_in) separado e opcional, ligado a consent_whatsapp.
- Diga quem vai mandar e o quê, em linguagem simples: "Aceito receber mensagens da Clínica Viva pelo WhatsApp com novidades e ofertas." Use o nome real do negócio do usuário.
- O aceite nunca é obrigatório para concluir o formulário, e quem não aceita continua sendo atendido.
- Se o negócio tiver política de privacidade, ligue o link em termsUrl do aceite.
- Base legal, prova do consentimento e saída da lista: siga lgpd-no-marketing.

## Dados sensíveis
- Não peça dado sensível (CPF, RG, saúde, religião, dados financeiros, fotos de documentos) a não ser que o atendimento dependa dele. Quando depender, diga na tela por que precisa e marque o campo como sensitive.
- Fotos e documentos já são sensíveis. Quem não tem permissão vê essas respostas como ••••.
- Nunca peça senha, número completo de cartão ou código de segurança.

## Enviar e acompanhar
- Publique com publish_whatsapp_form antes de enviar.
- send_whatsapp_form só vai com a janela de 24 horas aberta. A mensagem diz o que a pessoa ganha e quanto tempo leva ("Leva 1 minuto"); o botão é curto e com verbo ("Preencher", "Agendar").
- Fora da janela, a alternativa é um modelo aprovado com botão do formulário, enviado com send_template.
- Para agir nas respostas, ofereça create_form_automation (etiqueta e oportunidade, em rascunho para o usuário revisar e ativar).
- Para avaliar, whatsapp_form_results mostra envios, respostas, tempo até responder e as escolhas mais comuns; whatsapp_form_responses traz as respostas, e export_whatsapp_form_responses gera a planilha.
