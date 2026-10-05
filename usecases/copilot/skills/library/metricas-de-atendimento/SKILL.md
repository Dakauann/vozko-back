---
name: metricas-de-atendimento
description: Quais métricas de atendimento importam, referências de mercado para WhatsApp e como transformar os números em ações; carregue antes de analisar desempenho da equipe ou dos agentes.
---

# Métricas de atendimento

## As que importam
- Tempo de primeira resposta: entre a primeira mensagem do cliente e a primeira resposta. Referências de mercado no WhatsApp: segundos para automação, até 1 minuto para pessoas em horário de pico bem atendido. Contato de anúncio esfria em minutos.
- Separe o tempo da IA e o tempo das pessoas: uma média única esconde a espera longa de quem foi passado para a equipe.
- Tempo até resolver e taxa de resolução: quanto tempo e quantas conversas terminam resolvidas.
- Retrabalho: conversas reabertas ou que voltam com o mesmo assunto indicam resposta incompleta.
- Fila e conversas sem resposta: o que está esperando agora.
- Passagens da IA para pessoas: acima de uns 15% sugere base de conhecimento ou instruções fracas.
- Satisfação (CSAT): acima de 85% é ótimo; abaixo de 80% pede atenção.
- Conversão por etapa do funil: mostra onde os clientes travam.

## Como ler
- Compare períodos iguais (semana com semana) e olhe tendência, não um dia isolado.
- Olhe por canal, por pessoa e por horário: o problema costuma estar concentrado.
- Volume alto com tempo de resposta subindo é falta de gente ou de automação naquele horário.

## Transformar em ação
- Primeira resposta lenta em certos horários: escala, agente de IA no horário vazio ou mensagem de espera honesta.
- Muita passagem da IA para pessoas: veja as conversas passadas, complete a base de conhecimento e as instruções.
- Retrabalho alto: revise as respostas mais comuns e crie respostas prontas corretas.

## Presença e atividade de uma pessoa
- Conectado quer dizer que o painel estava aberto no navegador, não que a pessoa estava trabalhando. Não existe estado de ausente: só conectado, em ligação e offline.
- Uma sessão de 14 horas ou mais (possible_forgotten_tab) quase sempre é aba esquecida aberta; não conte como jornada e diga isso.
- late_start compara com o horário habitual da própria pessoa; no_presence só aparece em dias da semana em que ela costuma trabalhar.
- A roleta só entrega conversas para quem está conectado. Se alguém "não recebeu nada", veja primeiro se estava offline naquele horário. received_while_offline acima de zero é sinal de problema no sistema, não da pessoa.
- Fale com cuidado: mostre os fatos e o horário, sem acusar. Quem diz que trabalhou pode ter usado outro navegador ou o celular.

## No Vozko
attendance_metrics, attendance_trend, attendance_team, attendance_backlog, attendance_live, attendance_rework e attendance_stages trazem os números; member_activity mostra a presença e o trabalho de uma pessoa; conversation_insights mostra o que os clientes falam. Comece pela conclusão, depois os números e uma ação.
