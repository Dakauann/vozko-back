---
name: formatos-e-zonas-seguras
description: Tamanhos, proporções, cortes automáticos e zonas seguras de cada rede e peça (Reels, Stories, feed, carrossel, Marketplace, coluna da direita, Shorts, TikTok, miniatura do YouTube, banners, apresentações, Status do WhatsApp), como aplicar no Estúdio e como conferir o encaixe por posicionamento antes de anunciar; carregue antes de escolher o formato, posicionar texto e logo ou criar um anúncio com imagem.
---

# Formatos e zonas seguras

## Como aplicar no Estúdio
- Vídeo: set_canvas com aspect story (9:16), portrait (4:5), square (1:1) ou landscape (16:9).
- Imagem: update_artboard com width e height em pixels. Para entregar a mesma peça em vários formatos, faça uma prancheta por formato com duplicate_artboard (width e height de cada rede) e confira as zonas seguras em cada uma.
- Posição vai de 0 a 1 do quadro: uma zona segura de 14% no topo vira y mínimo de 0,14 para a borda de cima do texto.
- A checagem automática avisa quando um texto entra na faixa da interface em artes 9:16.

## Posicionamentos da Meta
| Grupo | Proporção | Tamanho recomendado | Mínimo | O que a Meta faz com outra proporção |
|---|---|---|---|---|
| Feeds (Facebook, Instagram, Explorar, pesquisa do Instagram, Threads) | 4:5 | 1440x1800 | 600 px de largura | mostra inteira de 1,91:1 a 4:5; mais alta que 4:5 é cortada no centro para 4:5 |
| Stories e Reels (Facebook e Instagram) | 9:16 | 1440x2560 | 500 px de largura | imagem de outra proporção ocupa a largura e o resto do quadro vira um fundo preenchido |
| Marketplace | 1:1 | 1080x1080 | | corte central em 1:1 |
| Coluna da direita (só computador) | 1:1 | 1080x1080 | 254x133 | corte central em 1:1, imagem pequena: texto na imagem quase não se lê |
| Pesquisa do Facebook | 1:1 a 1,91:1 | 1080x1080 | 600x600 | corte central para o limite mais perto |
| Mensagens patrocinadas do Messenger e Audience Network | 1:1 a 1,91:1 | 1200x628 | Audience Network 398x208 | corte central para o limite mais perto |
| Notificações e sobreposição no Reels | 1:1 | 1080x1080 | | miniatura pequena |

- Sem recorte definido, o corte da Meta é centralizado: o que está nas bordas some primeiro.
- Com melhorias de criativo ligadas (enhancements true), a Meta ainda pode expandir ou recortar por conta própria; arte com texto vai com enhancements false.
- No editor de anúncios, a pessoa troca a mídia de cada posicionamento à mão; pela API a plataforma não separa imagens por posicionamento dentro do mesmo anúncio.

## Reels, Stories e anúncios verticais da Meta (9:16)
- Tamanho recomendado 1440x2560 (vídeo do editor: 1080x1920).
- Deixe livres de texto, logo e chamada: 14% no topo, 35% na base e 6% em cada lateral. Em 1080x1920: cerca de 269 px no topo, 672 px na base e 65 px nas laterais.
- No topo ficam o perfil e os controles; na base, a legenda (que cresce com o texto), o botão e, no Reels, as ações. Stories do Messenger pedem 14% no topo e 20% na base.
- Zona segura não é área vazia. Ela só proíbe texto essencial, logo e chamada debaixo da interface; o fundo, a foto, o produto, a textura e a decoração continuam por baixo dela até a borda, como num pôster cheio. Uma base vazia parece peça inacabada: estenda a imagem ou o cenário até embaixo, pouse o produto perto da linha dos 65%, deixe uma forma, um degradê ou um padrão atravessar a faixa. O alerta de faixa vazia avisa quando uma faixa larga do quadro fica só com o fundo.

## Conferir antes de anunciar
- Antes de create_ad ou save_ad_draft, ou de trocar a imagem de um rascunho, com uma imagem gerada, editada no Estúdio ou enviada pelo usuário, chame ad_placement_look com o media_id e os mesmos posicionamentos do anúncio.
- Olhe cada prévia: o quadro é o que a pessoa vê, o vermelho é a interface por cima e o essencial fica dentro do contorno amarelo. Os números contam o que o olho erra: quanto o corte tira, quanto do detalhe (texto, logo, rosto) fica cortado ou coberto, a resolução mostrada e a altura mínima de texto legível naquele tamanho.
- Sem visão no modelo, a resposta vem só com números: decida por eles e diga ao usuário que a conferência foi numérica.
- A conferência informa, não proíbe. Quando algo não couber, escolha pelo caso:
  - recompor no Estúdio: uma prancheta por grupo de posicionamentos (4:5 para feeds, 9:16 para Stories e Reels), o essencial dentro da área segura de cada uma, e exportar de novo;
  - uma variante por grupo: imagens 4:5 e 9:16 em FLEXIBLE deixam a Meta escolher, sem garantia; para garantir, um anúncio por grupo com placements restritos;
  - restringir placements, por exemplo tirar a coluna da direita ou o Audience Network quando a arte depende de texto pequeno;
  - avisar o usuário com o que vai sair cortado ou coberto e seguir se ele preferir assim.

## YouTube Shorts e anúncios verticais do Google (9:16)
- Mais apertado à direita, onde ficam os botões: 15% no topo, 35% na base, 4,4% à esquerda e 17,8% à direita.
- Caixa vertical que serve para Meta e Google: em 1080x1920, de x 65 a 888 e de y 288 a 1248. O que é essencial fica dentro dela.

## TikTok (9:16)
- Mínimo de 540x960. A legenda do post ocupa a base; mantenha o essencial longe dos últimos 25% e da faixa direita dos botões.

## Feed
- Imagem do feed da Meta em 4:5, 1440x1800. O perfil do Instagram mostra as fotos cortadas em 3:4: o essencial fica no centro.
- Texto principal do anúncio com até 125 caracteres.

## Carrossel
- Anúncio da Meta: de 2 a 10 cartões, 4:5 só com imagens ou 1:1 se tiver vídeo. Carrossel orgânico do Instagram vai até 20.
- Carregue carrossel-e-miniatura para a narrativa entre os cartões.

## Miniatura do YouTube (16:9)
- 1280x720 ou mais, até 2 MB. Nada importante no canto inferior direito, onde aparece a duração.

## Display do Google
- 1200x628 (1,91:1) e 1200x1200 (1:1); logos em 4:1 e 1:1. Pode ser cortado em até 5% de cada lado: margem interna maior.

## Apresentação (16:9)
- Uma ideia por slide, entendida em 3 segundos. Texto com pelo menos 30 pt.

## Status do WhatsApp (9:16)
- Até 60 s por vídeo. Não há botões de anúncio, mas mantenha 14% no topo e 20% na base livres de texto essencial; a imagem continua até a borda.

## Regras gerais
- Formato errado é o erro mais caro: confirme a rede antes de compor; na dúvida pergunte com studio_ask.
- Vertical cheio (9:16) para Reels, Stories, TikTok e Shorts; 4:5 para o feed.
- Logo e chamada sempre dentro da zona segura da rede de destino.
- Zona segura restringe texto, logo e chamada, nunca o desenho: a arte ocupa o quadro inteiro.
