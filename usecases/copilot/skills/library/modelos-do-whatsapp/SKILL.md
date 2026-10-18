---
name: modelos-do-whatsapp
surface: panel
description: Como escrever modelos do WhatsApp oficial que a Meta aprova na categoria certa (marketing, utilidade, autenticação), variáveis, motivos de reprovação e custo; carregue antes de criar um modelo.
---

# Modelos do WhatsApp oficial

## Categorias (como a Meta decide)
- Utilidade só quando cumpre as duas condições: é não promocional (nenhuma oferta, desconto, cupom, novidade, recomendação, upsell ou venda cruzada) e é específica de uma ação ou pedido do cliente (pedido, agendamento, pagamento, conta) ou essencial para ele (alerta de segurança, sistema fora do ar, recall, mudança de política de privacidade).
- Deixe a transação explícita com variáveis do que é específico: número do pedido, data, valor, serviço. Mensagem genérica, só saudação ou só variável vira marketing.
- Conteúdo misto vira marketing inteiro: um aviso de entrega com "aproveite 10% na próxima compra" é marketing. Separe em dois modelos.
- Pesquisa de satisfação só é utilidade quando fala de uma compra ou atendimento específico; pesquisa genérica é marketing.
- Utilidade não serve para puxar uma conversa que o cliente não pediu.
- Marketing: todo o resto, incluindo divulgação, convite, reengajamento, parabéns e boas-vindas genéricas.
- Autenticação: só código de verificação, com botão de copiar código, sem link, mídia ou emoji. Não é criada por create_template.
- Desde abril de 2025 a Meta aprova como marketing o modelo de utilidade que ela considerar marketing, e pode reclassificar um modelo já aprovado avisando um dia antes. Quem insiste em usar utilidade para vender recebe aviso, limite nos envios de utilidade e restrição na conta inteira.
- Se o usuário pedir utilidade com algo promocional, explique isso e proponha dois modelos: o aviso como utilidade e a oferta como marketing.

## Custo
Desde julho de 2025 a Meta cobra por mensagem entregue, com preço por categoria e pelo país do número. Marketing é a mais cara. Utilidade enviada enquanto a janela de 24 horas de atendimento está aberta não é cobrada pela Meta. Diga ao usuário que o custo aparece na aprovação.

## O que faz reprovar
- Variáveis fora de ordem ou pulando número, começando ou terminando o texto, duas seguidas, ou muitas para o tamanho do texto.
- Exemplos de variável com #, $ ou %. Em valores, deixe o "R$" no texto fixo e só o número na variável: "Valor: R$ {{2}}" com exemplo "150,00".
- Exemplos irreais ou faltando. Cada variável precisa de um exemplo que pareça uma mensagem de verdade.
- Texto ou exemplos em idioma diferente do idioma do modelo; a Meta não traduz.
- Pedir dado sensível completo: número de cartão, de conta, CPF ou RG inteiros (os últimos 4 dígitos podem).
- Tom ameaçador, como ameaça de processo ou de exposição pública, comum em cobrança. Cobrança é utilidade quando é um aviso claro e neutro.
- Mesmo corpo e rodapé de outro modelo da conta (duplicado).
- Spam, golpe ou conteúdo proibido pela Política de Comércio da Meta (apostas, conteúdo adulto, drogas, armas, promessas financeiras, alegações de saúde).
- Links quebrados e erros de formatação.

## Como escrever
- Comece dizendo quem fala e por quê: "Olá {{1}}, aqui é a Clínica Viva. Sua consulta está marcada para {{2}}."
- Variáveis numeradas na ordem ({{1}}, {{2}}) com um exemplo real para cada uma (body_examples).
- Curto e com uma ação clara; botões de resposta rápida (até 3) para respostas comuns ("Confirmar", "Remarcar").
- Rodapé curto. Use o rodapé para mostrar ao lead como sair, com um comando de barra que ele possa enviar, por exemplo "Responda /sair para não receber mais"; sem a barra a mensagem não vale como saída.
- Outra saída possível é um botão: inclua o texto em quick_replies (por exemplo "Não quero receber") e repita esse texto em exit_button. Com exit_confirmation você define a resposta enviada a quem sair.
- Saída no marketing: obrigatória. Sem ela o Vozko recusa a criação.
- Saída na utilidade: inclua também, por padrão. Só deixe de fora quando o usuário pedir. Use uma frase neutra que fale do tipo de aviso, por exemplo "Para não receber mais estes avisos, responda /sair", sem nada promocional, para o modelo continuar como utilidade.

## No Vozko
create_template manda para a Meta aprovar (minutos a horas); list_templates mostra a situação e quantas variáveis cada modelo pede. Só use em envios depois de aprovado.

Para mudar um modelo que já existe, use edit_template em vez de criar outro com nome parecido: a Meta trata cópias levemente alteradas de um modelo pausado como tentativa de burlar a pausa. Mande o modelo inteiro na nova versão. Um modelo aprovado aceita uma edição por dia, então junte as mudanças pedidas numa edição só.
