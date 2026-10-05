---
name: modelos-do-whatsapp
description: Como escrever modelos do WhatsApp oficial que a Meta aprova na categoria certa (marketing, utilidade, autenticação), variáveis, motivos de reprovação e custo; carregue antes de criar um modelo.
---

# Modelos do WhatsApp oficial

## Categorias
- Marketing: promoção, oferta, novidade, convite, reengajamento, qualquer coisa que venda.
- Utilidade: algo que o cliente pediu ou está em andamento com ele: confirmação de pedido, agendamento, lembrete de consulta, atualização de entrega, cobrança.
- Autenticação: só códigos de verificação.
Desde 2025 a Meta muda sozinha para marketing todo modelo de utilidade com linguagem promocional (desconto, oferta, cupom). Não tente esconder promoção em utilidade: além de custar como marketing, insistir pode render punição na conta.

## Custo
Desde julho de 2025 a Meta cobra por mensagem entregue, com preço por categoria e pelo país do número. Marketing é a mais cara. Utilidade enviada enquanto a janela de 24 horas de atendimento está aberta não é cobrada pela Meta. Diga ao usuário que o custo aparece na aprovação.

## O que faz reprovar
- Categoria errada.
- Variáveis sem exemplo, ou variável solta no início ou no fim do texto, ou duas variáveis seguidas.
- Texto vago que só funciona com as variáveis ("Olá {{1}}, {{2}}").
- Conteúdo proibido pela Meta (apostas, conteúdo adulto, promessas financeiras, alegações de saúde).
- Links quebrados, erros de formatação.

## Como escrever
- Comece dizendo quem fala e por quê: "Olá {{1}}, aqui é a Clínica Viva. Sua consulta está marcada para {{2}}."
- Variáveis numeradas na ordem ({{1}}, {{2}}) com um exemplo real para cada uma (body_examples).
- Curto e com uma ação clara; botões de resposta rápida (até 3) para respostas comuns ("Confirmar", "Remarcar").
- Rodapé curto opcional, por exemplo como sair da lista no marketing.

## No Vozko
create_template manda para a Meta aprovar (minutos a horas); list_templates mostra a situação e quantas variáveis cada modelo pede. Só use em envios depois de aprovado.
