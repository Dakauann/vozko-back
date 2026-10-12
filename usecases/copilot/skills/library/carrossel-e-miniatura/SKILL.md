---
name: carrossel-e-miniatura
description: Como criar carrosséis que fazem a pessoa deslizar até o fim e miniaturas que ganham o clique (narrativa entre cartões, continuidade visual, teste do tamanho pequeno, rosto e poucas palavras); carregue antes de montar um carrossel ou uma capa de vídeo.
---

# Carrossel e miniatura

## Carrossel
- O primeiro cartão é o único com visualização garantida: gancho forte e a promessa do que vem depois ("5 erros que...", "Antes e depois").
- Em anúncios, a Meta pode reordenar os cartões para cada pessoa: cada cartão também precisa fazer sentido sozinho.
- Uma ideia por cartão. A chamada fica no último cartão.
- Continuidade: a mesma grade, as mesmas margens, a mesma paleta e a mesma tipografia em todos. Um contador (1 de 6) ou uma imagem que continua de um cartão para o outro convida a deslizar.
- Entre 5 e 10 cartões é o ponto doce. Numeração em sequência é bem-vinda quando a ordem informa algo.
- Formato: 4:5 só com imagens, 1:1 se houver vídeo; o essencial dentro do centro 3:4.

## Montar no editor de imagem
1. Cada cartão é uma prancheta do mesmo projeto. Monte o primeiro completo e confira com studio_look.
2. Para o próximo cartão, duplicate_artboard do primeiro e troque só o conteúdo.
3. Mantenha grade, cores, fontes e os mesmos x, y, w e h dos títulos em todos os cartões.
4. Antes de dizer que está pronto, veja o conjunto lado a lado com studio_look e artboard_id all.

## Fotos da mesma série
- Fotos geradas uma a uma saem com fundo e luz diferentes, e cada uma vira um retângulo de outro tom sobre o cartão. Desde a primeira: o mesmo fundo e a mesma luz descritos com as mesmas palavras em todos os prompts, e a primeira imagem aprovada em reference_media_ids das próximas, com o papel dito no prompt ("fundo e luz da referência 1").
- Ou gere os produtos e recorte cada um (studio_start_job kind cutout) sobre um fundo único: a cor da marca, um degradê ou um fundo gerado uma vez para todos, com a mesma sombra de contato, a mesma escala e a mesma linha de base em cada cartão.
- Se a série já saiu desencontrada, ofereça os caminhos com o custo de cada um: recortar e pôr num fundo comum é grátis; gerar de novo custa; um duotom igual em todos esconde a diferença, mas tira a cor do produto.

## Miniatura do YouTube
- Um rosto com emoção clara ou um objeto só, e de 3 a 4 palavras em fonte forte sem serifa.
- Teste do tamanho pequeno: a miniatura precisa funcionar em cerca de 10% do tamanho. Veja com studio_look e imagine-a minúscula.
- Contraste alto entre o assunto e o fundo; fundo simples ou desfocado (filters com blur na foto de fundo).
- Nada no canto inferior direito, onde aparece a duração.
- A miniatura promete o que os primeiros 30 segundos do vídeo entregam; promessa falsa derruba a retenção.
- O YouTube testa até 3 versões e escolhe a que dá mais tempo assistido: ofereça variações com studio_ask quando fizer sentido.

## Erros comuns
- Primeiro cartão sem gancho. Cartões com estilos diferentes. Texto demais por cartão.
- Miniatura com muitas palavras, rosto pequeno demais ou detalhe que some no tamanho pequeno.
