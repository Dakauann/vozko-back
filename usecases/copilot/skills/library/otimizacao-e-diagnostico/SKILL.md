---
name: otimizacao-e-diagnostico
description: Como ler os resultados, diagnosticar problemas e otimizar anúncios publicados sem atrapalhar o aprendizado; carregue antes de avaliar desempenho ou sugerir mudanças.
---

# Otimização e diagnóstico

## Ler antes de mexer
- Traga os números com ads_results ou run_ad_report, sempre com o período e a moeda.
- Olhe nesta ordem: entrega (está veiculando?), custo por resultado, volume de resultados, taxa de cliques, frequência, custo por mil impressões.
- Período mínimo para julgar: 3 a 7 dias e algumas dezenas de resultados. Com 2 dias ou poucos resultados, diga que ainda não dá para concluir.

## Não atrapalhe o aprendizado
- Durante a fase de aprendizado, evite mudar orçamento, público, criativo ou lance. Cada mudança grande recomeça o aprendizado.
- Mudanças de orçamento: aos poucos e com dias de intervalo. A Meta aceita mudar o orçamento de um conjunto no máximo 4 vezes por hora.
- "Aprendizado limitado" quase sempre se resolve consolidando (menos conjuntos, mais orçamento por conjunto) ou otimizando para um resultado mais frequente.

## Diagnóstico por sintoma
- Não entrega: anúncio em análise ou reprovado, conta sem fundos ou com pagamento recusado (veja funds em list_ad_accounts), orçamento baixo demais para o público, público pequeno demais.
- Muita impressão e pouco clique (taxa de cliques baixa): o criativo não para a rolagem. Teste novos ganchos e ângulos.
- Clique sem resultado: a promessa do anúncio não bate com o destino (mensagem pronta, formulário, página). Ajuste a oferta e a mensagem pronta.
- Custo subindo com frequência alta: fadiga. Frequência acima de 3 por semana em público novo, queda de 20 a 30% nos cliques ou custo por resultado dobrando pedem criativos novos.
- Resultado barato e venda nenhuma: está otimizando para o evento errado ou atraindo curiosos. Ligue as conversões do CRM e qualifique na conversa.

## O que fazer
- Pausar só o que está claramente pior depois de tempo e volume suficientes; o resto, deixe rodar.
- Para escalar o que funciona: aumente o orçamento aos poucos, ou duplique o anúncio vencedor e acrescente criativos novos no mesmo conjunto.
- Para comparar com rigor: create_ad_test com 2 a 5 campanhas ou conjuntos por pelo menos 7 dias.
- Para vigiar sozinho: create_ad_rule, explicando em palavras simples o que a regra fará.

## Como explicar
Comece pela conclusão, depois os números que a sustentam e uma única próxima ação recomendada. Sem jargão; quando usar um termo (frequência, CPM), explique em meia frase.
