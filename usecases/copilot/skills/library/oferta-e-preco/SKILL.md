---
name: oferta-e-preco
description: Como criar peças de oferta e promoção com preço que convertem e cumprem a lei brasileira (hierarquia produto e preço, preço à vista, parcelamento completo, urgência verdadeira, uma oferta por peça); carregue antes de criar arte ou vídeo com preço, desconto ou promoção.
---

# Oferta e preço

## Hierarquia
- O produto é o primeiro elemento; o preço é o segundo. Depois a condição e a chamada.
- Uma oferta por peça. Duas ofertas brigam e nenhuma é lembrada.
- O preço em tamanho grande, peso forte e cor de acento, com contraste de pelo menos 4,5:1.
- Preço "de" riscado menor e mais apagado que o preço "por".

## A lei brasileira de preços (Decreto 5.903/2006)
- O preço precisa ser claro, preciso e legível.
- É infração mostrar só as parcelas, obrigando a pessoa a calcular o total: sempre mostre o preço à vista.
- No parcelado, informe o número de parcelas, o valor de cada uma, os juros e o total.
- É infração usar letras de tamanhos desiguais que escondam informação, ou texto na mesma cor ou em cor parecida com o fundo.
- Condições como "frete grátis acima de" ou "estoque limitado" ficam legíveis, não em letra miúda.

## Urgência
- Prazo e escassez funcionam ("só até domingo"), mas precisam ser verdadeiros.
- Em vídeo, a chamada aparece mais de uma vez e fica fixa no fim; diga e escreva.

## Arte de oferta no editor
- Produto com fundo removido (studio_start_job com cutout) sobre um fundo limpo ou um degradê radial que destaca o centro.
- Selo de desconto com add_shape (star ou ellipse) na cor de acento, sem cobrir o produto.
- Preço com add_text, font_weight 800, perto do produto. Parcelamento logo abaixo, menor, nunca escondido.
- Confira com studio_look e com o campo checks que nada da condição ficou ilegível.

## Erros comuns
- Só parcela, sem preço à vista. Juros escondidos. Letra miúda clara sobre fundo claro.
- Várias ofertas na mesma peça. Urgência falsa. Preço menor que o selo decorativo.
