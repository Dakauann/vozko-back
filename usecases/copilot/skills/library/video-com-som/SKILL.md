---
name: video-com-som
description: Como montar um vídeo de anúncio com música e locução (generate_music, generate_voiceover, render_video) que funcione mudo e com som; carregue antes de criar qualquer vídeo ou som de anúncio.
---

# Vídeo de anúncio com som

## Antes de começar
- Som só existe em vídeo. Um anúncio de imagem nunca toca música.
- A maioria das pessoas vê o feed sem som: o vídeo precisa contar a história só com as imagens. O som reforça, não carrega.
- Use imagens e vídeos reais do cliente ou gerados com as referências dele (veja criativo-com-a-marca). Nada inventado sobre produto, equipe ou resultados.

## Estrutura que funciona
- Os 3 primeiros segundos decidem: comece pela dor, pelo resultado ou pelo produto em uso, nunca por logo ou abertura.
- Curto: 6 a 15 segundos para Stories e Reels, até 30 para o feed. Cada cena de 2 a 4 segundos; cenas longas só quando algo acontece nelas.
- Formato do lugar: story (9:16) para Stories e Reels, portrait (4:5) para o feed. Monte uma versão por formato.
- Termine com o pedido claro (o mesmo do botão do anúncio) e a marca visível na última cena.

## Locução
- Roteiro curto e falado, como se fosse para um amigo: cerca de 2 a 3 palavras por segundo. 15 segundos aceitam umas 35 palavras.
- Uma ideia por frase, a oferta ou o benefício logo no começo, o pedido no fim.
- Escreva no idioma do público e confira nomes e números com o usuário antes de gerar; a voz lê exatamente o que estiver escrito.
- Escolha a voz pelo tom da marca: mais calorosa para serviços próximos, mais firme para B2B.

## Música
- Descreva o clima e o uso: "instrumental leve e acústico para fundo de anúncio de cafeteria, sem voz". Sem pedir músicas ou artistas conhecidos.
- Com locução, a música fica baixa por baixo da voz (o render_video já abaixa); sem locução, ela pode conduzir o ritmo das cenas.
- Cada clipe tem cerca de 30 segundos e o vídeo usa só o trecho necessário.

## Fluxo
1. Combine com o usuário a mensagem, as cenas (media_id e segundos) e se terá música, locução ou as duas.
2. Gere a música e ou a locução e mostre para ele ouvir.
3. render_video com as cenas na ordem, o formato e os media_id do som. Mostre o vídeo.
4. Ajustes de duração ou ordem só remontam o vídeo (sem custo); trocar música ou voz gera de novo e é cobrado.
5. Use o media_id do vídeo como criativo VIDEO em create_ad, save_ad_draft ou swap_ad_creative.
