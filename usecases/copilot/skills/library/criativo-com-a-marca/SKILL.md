---
name: criativo-com-a-marca
description: Como gerar imagens de anúncio novas a partir das referências do cliente (logo, fotos, prints, estilo) com generate_image; carregue antes de criar qualquer imagem de anúncio.
---

# Criativo com a marca do cliente

## Regra principal
O criativo é uma imagem nova, gerada pelo modelo de imagem a partir das referências do cliente. Nunca devolva a imagem do cliente com textos, selos ou botões colados por cima. A identidade (cores, logo, estilo, produto) vem das referências dele, nunca de um estilo padrão.

## Reunir as referências
1. Peça o que existir: logo, fotos reais do produto, do serviço, do espaço ou da equipe, prints do site ou do Instagram, e anúncios antigos de que o cliente gostou.
2. Se você enxerga os anexos, descreva em uma frase o que vai aproveitar de cada um (cores, clima, produto, enquadramento) e confirme com o usuário antes de gerar.
3. Prints podem ter nomes e telefones de clientes: pergunte antes de usar e prefira um print sem dados pessoais.

## Gerar com generate_image
- Passe os media_id em reference_media_ids na ordem de importância e diga no prompt o papel de cada um: "logo da referência 1 idêntico, no canto superior", "produto da referência 2 fiel, sem mudar forma nem rótulo", "paleta e clima da referência 3".
- Descreva a cena pelo ângulo do anúncio (veja criativos-que-performam): o que aparece, quem, onde, luz e enquadramento, com as cores da marca em palavras e em hexadecimal quando o cliente souber.
- Texto escrito dentro da imagem: o mínimo, ou nenhum. Modelos de imagem erram letras; a mensagem vai no texto do anúncio. Se o cliente fizer questão, no máximo 5 palavras e confira a grafia na imagem gerada.
- Proporção certa para cada lugar: portrait (4:5) para o feed, story (9:16) para Stories e Reels, square (1:1) para carrossel. Gere uma versão por proporção em vez de cortar.
- Variações de ângulo, não de detalhe: para testar, gere conceitos diferentes (dor, desejo, prova, oferta), cada um com as mesmas referências da marca.

## Nunca inventar
- O produto, o espaço, a tela do sistema e a equipe que aparecem precisam vir das referências. Sem referência do que precisa ser real, peça ao usuário em vez de imaginar.
- Nada de pessoas apresentadas como clientes reais, depoimentos, números, selos ou prêmios que o cliente não forneceu.

## Ajustes e uso
- Para ajustar uma imagem gerada ("mais escura", "troque o fundo"), passe o media_id dela como referência e descreva só a mudança.
- Mostre a imagem, ajuste o que o usuário pedir e só então use o media_id em create_ad ou swap_ad_creative.
- Imagens com texto ou telas vão com enhancements false, para a Meta não cortar; fotos e cenas sem texto podem ir com enhancements true.
