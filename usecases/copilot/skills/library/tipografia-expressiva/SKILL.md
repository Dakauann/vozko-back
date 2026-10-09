---
name: tipografia-expressiva
description: Tipografia de cartaz e post no Estúdio (papel de cada fonte do editor, pares que funcionam, escala entre níveis, tratamentos de título como caixa alta empilhada, contorno, eco, vertical, itálico de velocidade, arco e cromado, e texto que vira imagem); carregue ao escolher fontes e montar títulos.
---

# Tipografia expressiva

A higiene (tamanho mínimo, contraste, linhas longas) está em design-de-imagem. Aqui fica a voz.

## O papel de cada fonte do editor
- Condensadas de impacto: bebas-neue (só caixa alta, um peso só), oswald (200 a 700, com minúsculas).
- Geométricas: montserrat (o 900 é forte em cartaz, mas é a fonte mais usada de todas), poppins (simpática, muito usada em SaaS), raleway (pesos finos bonitos e caixa alta espaçada; os números são de estilo antigo, então preços e datas ficam desnivelados).
- De trabalho: inter, roboto, work-sans (a de mais personalidade em tamanho grande), open-sans, lato (quente), dm-sans, nunito (arredondada: infantil, saúde, educação).
- Serifadas: playfair-display (alto contraste; o itálico é o mais expressivo do editor), merriweather (robusta, olho grande, itálico forte).

## Pares
Contraste um eixo (serifa contra sem serifa, condensada contra larga, 900 contra 200) e compartilhe outro (altura do x, clima). No máximo duas famílias; uma família só, com contraste de peso, caixa e largura, muitas vezes é mais forte. playfair-display, dm-sans e inter como título são escolhas padrão de IA: use com um motivo.

| Título + apoio | Uso |
|---|---|
| bebas-neue + work-sans | eventos, esporte, noite |
| bebas-neue + merriweather itálico | automobilismo clássico, documentário |
| oswald 700 + lato | notícia, promoção, academia |
| oswald 600 + merriweather | revista, institucional com energia |
| playfair-display itálico + work-sans em caixa alta espaçada | moda, alta gastronomia, beleza, hotel |
| montserrat 900 + montserrat 300 | varejo, fitness |
| raleway 200 em caixa alta espaçada + raleway 800 | moda minimalista, arquitetura, imóvel de alto padrão |
| merriweather 900 + open-sans | educação, saúde, órgão público |
| nunito 900 + nunito 400 | infantil, clínica acolhedora |
| inter 800 apertada + inter 400 | tecnologia, suíço, corporativo |
| poppins 700 + merriweather | marca simpática que precisa de credibilidade |

## Escala
- Do herói ao segundo nível: de 3:1 a 8:1 em cartaz. Do segundo ao terceiro: de 1.5:1 a 2:1.
- Razões de escala: 1.25 para peças densas, 1.333 a 1.618 para anúncios, 2.618 quando o herói precisa dominar.
- Três níveis, não cinco.

## Tratamentos de título
Tamanhos relativos ao tamanho da letra. Em pixels, o tamanho da letra é font_size × altura da arte.
- **Gigante:** altura da maiúscula de 25% a 45% da arte; pode cortar 10% a 15% de uma letra na borda se a palavra continuar legível.
- **Caixa alta empilhada:** uma camada por linha, line_height 0.82 a 0.92, letter_spacing 0 a 0.01, cada linha com o tamanho que a faz ocupar a mesma largura. Acentos do português (Ã, Ç, É) batem abaixo de 0.95: aumente ou troque a palavra.
- **Espaçamento:** título em caixa mista de -0.02 a -0.04; rótulos em caixa alta de 0.08 a 0.20.
- **Só contorno:** fill "#00000000", stroke na cor e stroke_width de 1.5% a 3% do tamanho da letra em pixels. **Eco:** uma palavra cheia e de 2 a 4 cópias só de contorno (duplicate_layers), deslocadas, com opacity de 1 a 0.3.
- **Vertical:** rótulo de borda (x 0.04 ou 0.96, rotation -90, f 0.02 a 0.03, letter_spacing 0.15) ou uma palavra condensada gigante ocupando a altura toda.
- **Peso misturado:** 900 ao lado de 200 da mesma família.
- **Itálico de velocidade:** italic true nas fontes que têm itálico (todas menos bebas-neue e oswald, que ignoram italic). Com bebas-neue ou oswald, a velocidade vem do ângulo: gire a palavra (rotation de -6 a -12) sobre uma faixa diagonal. Linhas de velocidade saem do lado oposto ao movimento (formas-e-decoracao).
- **Caixa de destaque:** highlight com radius baixo e rotation entre -3 e 3; no máximo 2 por peça.
- **Arco:** curve, f 0.035 a 0.06, letter_spacing 0.10 a 0.20; bom em selos e em volta de uma roda ou de um prato.
- **Degradê no texto:** não use como ênfase de interface, mas cromado é um recurso de gênero (Y2K, metal, automobilismo): gradient com angle 90, from claro, via escuro e to claro, numa palavra só.

## Texto que vira imagem
1. Planos intercalados: a palavra atrás da pessoa, os rótulos na frente (profundidade-e-camadas).
2. Texto como recipiente: o texto embaixo e a foto logo acima com clip true.
3. Seguir a geometria da imagem: base paralela ao carro ou à estrada (rotation), texto em arco em volta de uma roda, título no horizonte.
4. Pegar a cor da imagem: fill tirado da foto, ou blend_mode multiply para a textura aparecer através.
5. Texto como textura: uma palavra repetida com opacity de 0.05 a 0.12 ao fundo.
6. Letra como objeto: um O que vira roda ou prato, quando a imagem real é clara.
7. Profundidade de montagem: a mesma palavra duas vezes, grande e cinza atrás, branca e menor na frente, deslocadas.

Confira larguras e quebras com studio_look: as estimativas de largura de composicoes são aproximadas.
