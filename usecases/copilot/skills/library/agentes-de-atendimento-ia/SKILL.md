---
name: agentes-de-atendimento-ia
description: Como escrever as instruções de um agente de IA de atendimento (papel, limites, fonte de verdade, passagem para humano) e testar antes de ligar; carregue antes de criar ou ajustar um agente.
---

# Agentes de atendimento com IA

## As quatro partes de boas instruções
Um agente bom recebe quatro coisas claras nas instruções do agente:
1. Papel e missão: quem ele é, para qual negócio, qual o objetivo da conversa (tirar dúvidas, qualificar, agendar, vender) e o tom (formal, próximo, curto).
2. Limites rígidos: o que nunca fazer (prometer prazo, preço ou resultado que não estão na base, dar diagnóstico médico ou jurídico, falar de concorrentes, inventar).
3. Fonte de verdade: responder só com o que está na base de conhecimento e nos dados das ferramentas; quando não souber, dizer que vai confirmar e passar para a equipe.
4. Passagem para humano: quando passar e como.

## Quando passar para uma pessoa
- O cliente pede para falar com alguém.
- Irritação, reclamação séria, ameaça de cancelamento ou de processo.
- Assuntos de saúde, jurídicos, financeiros ou contratuais específicos do cliente.
- A dúvida não foi resolvida depois de duas ou três tentativas.
- Pedido fora do que o agente pode fazer (negociar desconto, exceção de política).
Ao passar, o agente avisa o cliente com honestidade e deixa um resumo para a equipe.

## Estilo no WhatsApp
- Mensagens curtas, uma ideia por mensagem, sem paredes de texto.
- Uma pergunta por vez para qualificar.
- Sem listas enormes nem formatação pesada.
- Se apresentar como assistente virtual quando perguntarem; nunca fingir ser pessoa.

## Comece estreito
Comece com um escopo pequeno (por exemplo, dúvidas frequentes e agendamento) e amplie só depois de ver o agente funcionando. Limites frouxos demais fazem o agente inventar; apertados demais fazem ele recusar o que poderia resolver.

## Montar no Vozko
- list_models para escolher o modelo; list_agent_tools para as ferramentas que fazem sentido (agenda, memória do contato, base de conhecimento).
- Ligue o agente a uma base de conhecimento com o conteúdo do negócio (veja a habilidade base-de-conhecimento).
- create_agent ou update_agent mostram tudo antes, com a aprovação do usuário.
- Para o agente atender um canal, configure o atendimento do canal (no WhatsApp oficial, configure_number_automation).

## Testar antes de ligar
Antes de ligar para clientes, sugira testar no simulador do agente com as perguntas mais comuns, uma pergunta fora do escopo e um cliente irritado. Ajuste as instruções pelo que der errado.
