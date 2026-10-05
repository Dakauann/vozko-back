---
name: base-de-conhecimento
description: Como organizar o conteúdo de uma base de conhecimento para o agente achar a resposta certa (um assunto por seção, perguntas frequentes, atualização); carregue antes de criar ou preencher uma base.
---

# Base de conhecimento que funciona

## Como o agente lê a base
O conteúdo é dividido em pedaços, e o agente busca os pedaços mais parecidos com a pergunta do cliente. Os títulos marcam onde cada pedaço começa. Por isso a estrutura do documento decide se a resposta certa é encontrada.

## Regras de escrita
- Um assunto por seção, com um título claro: "Política de troca", "Formas de pagamento", "Horário de atendimento". Nunca misture troca, entrega e pagamento na mesma seção.
- Perguntas frequentes no formato pergunta e resposta, com a pergunta escrita como o cliente fala ("Vocês parcelam?").
- Cada seção deve fazer sentido sozinha: repita o nome do produto ou serviço em vez de "ele" ou "conforme acima".
- Números exatos e atuais: preços, prazos, endereço, horários, condições. Diga a data da última atualização quando o assunto muda com frequência.
- Tabelas pequenas viram listas; documentos escaneados viram texto.
- Nada de informação interna que o cliente não pode ver (margens, nomes de funcionários, senhas).

## O que colocar
- Produtos ou serviços com o que inclui, para quem é, preço ou como é calculado.
- Políticas: troca, cancelamento, garantia, entrega, pagamento.
- Atendimento: horários, endereço, como agendar, o que levar.
- Objeções comuns e a resposta certa.
- O que o agente não deve responder e para quem passar.

## No Vozko
- create_knowledge_base cria a base; add_knowledge_document adiciona o conteúdo.
- Depois de montar, teste com search_knowledge usando as perguntas reais dos clientes; se a resposta certa não aparece, divida ou renomeie a seção.
- Mantenha atualizado: base velha faz o agente afirmar coisa errada com confiança.
