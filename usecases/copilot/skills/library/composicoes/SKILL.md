---
name: composicoes
description: Treze arquétipos de layout para posts, cartazes e banners (texto atrás da pessoa, divisão diagonal, janela, cartaz tipográfico, grade suíça, selo, corte sangrando, colagem, número grande, Z, foto dentro do texto, faixa de horizonte, repetição com quebra), com coordenadas para 1:1, 4:5 e 9:16; carregue ao montar o layout de uma arte.
---

# Composições

Coordenadas do editor: x e y são o centro da camada, w e h o tamanho, tudo em fração da arte. font_size (f) é fração da altura da arte. Os números são pontos de partida: confira com studio_look e ajuste ao conteúdo.

## Conversões úteis
- Mesmo tamanho visual entre formatos: f(4:5) = 0.8 × f(1:1); f(9:16) = 0.5625 × f(1:1).
- Largura de uma palavra ≈ letras × a × f × (altura ÷ largura da arte). Valores aproximados de a para caixa alta (confira na tela, são estimativas): bebas-neue 0.40, oswald 700 0.48, roboto ou lato 900 0.64, inter, open-sans, nunito ou playfair-display 900 0.66, work-sans 800 0.68, raleway 800 0.70, poppins 800 0.72, merriweather 900 0.74, montserrat 900 0.78. Minúsculas ficam perto de 0.8 disso. Exemplo: "DRIFT" em bebas-neue ocupando 0.84 da largura de uma arte quadrada pede f ≈ 0.42; em 9:16, f ≈ 0.24.
- Zonas seguras: no 9:16, texto entre x 0.06 e 0.94 e y 0.14 e 0.65 (o palco de texto, quase quadrado). Componha o texto ali como numa peça 1:1 e deixe a imagem preencher o topo e a zona de interface embaixo. Um 1:1 mostrado na grade 3:4 do perfil perde 12.5% de cada lado: o essencial fica entre x 0.125 e 0.875. Para o resto, carregue formatos-e-zonas-seguras.

## 1. Texto atrás da pessoa
Para silhuetas fortes: pessoas, carros, produtos; eventos, esporte, moda, lançamentos. A pessoa cobre de 15% a 35% da palavra, nunca a primeira letra e nunca mais de uma letra inteira. Montagem em profundidade-e-camadas.
- 1:1: foto sangrando; palavra em y 0.30 a 0.36 ocupando 0.84 a 0.92 da largura; a cabeça entra no terço de baixo das letras; informação alinhada à esquerda em x 0.07, y 0.86 a 0.93.
- 4:5: palavra em y 0.26 a 0.30; informação em y 0.88 a 0.94.
- 9:16: palavra em y 0.22 a 0.30; informação em y 0.56 a 0.64; a pessoa pode descer pela zona de interface.

## 2. Divisão diagonal
Para velocidade, esporte, contraste, antes e depois. Costura de 8 a 20 graus para energia, de 25 a 40 para agressividade.
- Campo de cor com add_shape shape path, caixa da arte inteira (x 0.5, y 0.5, w 1, h 1) e path "M0 0.58 L1 0.40 L1 1 L0 1 Z"; ou path_preset diagonalSplit.
- Foto em cima, campo de cor embaixo, título girado no mesmo ângulo da costura (rotation entre -8 e -15).
- 4:5: costura entre y 0.55 e 0.62. 9:16: inverta, campo de texto em cima ("M0 0 L1 0 L1 0.40 L0 0.52 Z") e foto embaixo, sobre a zona de interface.

## 3. Janela
Para marcas calmas, imóveis, saúde, cafés, fotos fracas ou poluídas. Foto recortada numa forma sobre um campo sólido ou com padrão; o título atravessa a borda da janela.
- Janela: a forma embaixo e a foto logo acima com clip true. Formas: rect com radius, ellipse, path_preset arch, ou um path próprio.
- 1:1: janela em (0.60, 0.50, 0.62, 0.72); título em x 0.07 entrando de 0.06 a 0.12 na janela.
- 4:5: janela em (0.5, 0.40, 0.76, 0.56); título na borda de baixo, y 0.68 a 0.72.
- 9:16: janela em (0.5, 0.36, 0.84, 0.38); título em y 0.55 a 0.60.
- Para quebrar a moldura, ponha o recorte da pessoa acima da janela, saindo de 0.03 a 0.08 pelo topo.

## 4. Cartaz tipográfico
Sem imagem: manifestos, line-ups, promoções. O texto é a imagem.
- 1:1: de 3 a 5 linhas, uma camada de texto por linha, cada uma com o tamanho que a faz ocupar a mesma largura (0.86 a 0.88); entrelinha visual de 0.85 a 0.92 de f; o bloco enche de 70% a 85% da altura; alterne peso ou linha cheia e linha só de contorno; dados nos cantos com f 0.022 a 0.028, caixa alta espaçada.
- 9:16: de 4 a 7 linhas com f 0.08 a 0.12 dentro do palco de texto.

## 5. Grade suíça
Para cultura, arquitetura, tecnologia, corporativo, peças com muitos dados. Layout assimétrico numa grade modular, foto objetiva, sem serifa, alinhado à esquerda.
- 1:1: grade 6 × 6, margem 0.07, calha 0.02; título pendurado na margem de cima com f 0.08 a 0.12 e letter_spacing -0.02 a -0.03; uma foto ou forma ocupa um bloco de 3 × 4 módulos e pode sangrar; dados em duas colunas embaixo com f 0.022 a 0.03; um acento só.
- 4:5: grade 6 × 8. 9:16: grade 4 × 8 dentro do palco de texto.

## 6. Selo central
Para aniversários, selos, produtos artesanais, clubes, churrasco. O centro é merecido porque o objeto é simétrico.
- 1:1: círculo ou estrela serrilhada (shape star, points 20 a 28, inner 0.85 a 0.92) com diâmetro 0.56 a 0.66 em (0.5, 0.46); texto em arco com curve, f 0.035 a 0.05 e letter_spacing 0.12; título central com f 0.10 a 0.16; fita com path_preset ribbon, w 0.78 a 0.86 e h 0.09 a 0.12 em y 0.50 a 0.55; fundo com textura, não foto de papel de parede.
- 9:16: diâmetro de 0.80 da largura em y 0.36 a 0.40.

## 7. Corte sangrando
Para rostos, produtos, frentes de carro, comida. O assunto em 1.1 a 1.5 vezes a arte, com 15% a 40% fora dela (centro perto de (0.70, 0.60)); título no quadrante livre (x 0.07, topo em y 0.08 a 0.10). Corte pessoas no meio da coxa, do antebraço ou na testa, nunca nas articulações ou no queixo; mantenha o detalhe que identifica o produto.

## 8. Colagem
Para zine, público jovem, retrospectivas, cardápios, viagem. De 3 a 5 cartões com w 0.34 a 0.50, rotações entre -7 e 7 graus com sinais alternados, sobreposição de 10% a 25%, borda de foto impressa (stroke branco com stroke_width de 1.2% a 2% da largura da arte em pixels, 13 a 22 numa arte de 1080) ou nenhuma, sombra suave, fitas adesivas (rect 0.12 × 0.035, opacity 0.5) nos cantos, título como etiqueta ou carimbo, grão por cima de tudo. 9:16: ziguezague vertical no palco de texto.

## 9. Número grande
Quando o número é a mensagem: desconto, preço, data, contagem, anos.
- 1:1: numeral com f 0.50 a 0.75; % ou unidade com 0.4 a 0.5 do tamanho, alinhado pelo topo; rótulo pequeno espaçado acima; foto dentro do numeral (clip) ou pessoa na frente.
- Preço: moeda com 40% a 50% do tamanho, alinhada pelo topo; centavos com 50%, elevados; preço antigo com 25% a 35% e um traço por cima.
- 9:16: numeral com f 0.30 a 0.40.

## 10. Z
Para anúncios com marca, promessa, produto e chamada. Logo no canto superior esquerdo (w 0.10 a 0.14 perto de (0.15, 0.08)); título de y 0.16 a 0.30; produto na diagonal perto de (0.60, 0.55); chamada ou preço embaixo à direita (x 0.70 a 0.92, y 0.86 a 0.92). 9:16: chamada em y 0.60 a 0.64.

## 11. Foto dentro do texto
Para lugares e nomes curtos (3 a 6 letras). Caixa alta pesada e condensada ocupando 0.86 a 0.92 da largura, altura da maiúscula de pelo menos 0.30 da arte (0.18 em 9:16); o texto embaixo e a foto logo acima com clip true; fundo sólido; opcional uma cópia só de contorno deslocada 0.01.

## 12. Faixa de horizonte
Para imóveis, viagem, corporativo. Foto nos 55% a 65% de cima, faixa sólida embaixo com uma linha de dados em 2 ou 3 colunas; a base do título apoiada na costura. Uma costura em onda (path_preset wave) ou diagonal muda o tom sem perder a calma.

## 13. Repetição com quebra
Para varejo e lançamentos. Uma grade 3 × 3 ou 4 × 4 do mesmo objeto ou palavra; uma célula muda de cor, rotação ou conteúdo. Título pequeno; o ritmo faz o trabalho. Crie as células em lote, alinhe com align_layers e distribute_layers e agrupe com group_layers.
