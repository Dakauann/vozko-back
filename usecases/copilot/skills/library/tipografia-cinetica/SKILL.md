---
name: tipografia-cinetica
surface: studio
description: Como animar texto no vídeo como em motion design profissional (palavra por palavra, linha por linha, escalonamento, máquina de escrever, destaque de palavra, troca de palavra, contador, ritmo de leitura e áreas seguras dos vídeos curtos); carregue antes de criar títulos animados, frases de impacto, vinhetas com texto ou vídeos só de texto.
---

# Tipografia cinética

Texto animado serve à leitura: o movimento leva o olho para a próxima ideia e para assim que ela precisa ser lida. A 30 quadros por segundo, 1 quadro dura 33 ms. Tempos, curvas e o resto do motion estão em motion-design; o estilo das legendas da fala está em legendas-e-texto.

## Ritmo de leitura
- Cerca de 3 palavras por segundo. Cada texto fica parado na tela por (palavras ÷ 3) + 0,5 a 1 segundo depois de terminar de entrar.
- De 1 a 4 palavras por batida em vídeo curto; uma ideia por batida, no ritmo da locução ou da música.
- O texto termina de entrar antes de começar a ser lido: entradas de texto de 250 a 500 ms.

## Coreografia da peça
- Um verbo da marca guia o texto (pulsar, deslizar, montar, revelar, empilhar, carimbar); o conceito de movimento está em motion-design.
- Dentro de uma cascata a entrada é uma só; entre frases, varie a família: uma frase desliza pela diagonal, outra se revela por máscara, outra se monta letra por letra com add_text_animator, outra corre por set_text_path. Duas famílias por peça, no mínimo.
- Use o quadro inteiro: frases que entram pela borda, que ocupam cantos e alturas diferentes, que empurram a anterior para fora, em vez de cada mensagem trocar a outra no mesmo centro.
- Um momento herói com coreografia: palavras que se sobrepõem e se acomodam, a palavra principal que cresce enquanto as outras abrem espaço, uma revelação por máscara ou um contador que fecha a frase.
- Saídas com intenção (animate_text side out, exit): a frase sai na direção de onde a próxima vem, ou dá lugar a ela, em vez de sumir no corte.

## Escalonamento
- Entre letras: 1 a 2 quadros (33 a 67 ms). Entre palavras: 2 a 4 quadros (67 a 133 ms). Entre linhas: 4 a 8 quadros (130 a 270 ms).
- A cascata inteira cabe em cerca de 600 ms; frase longa pede escalonar por linha, não por palavra.
- Todas as partes usam a mesma entrada e a mesma curva; a variação vem só do atraso.
- A ordem segue a leitura: da esquerda para a direita e de cima para baixo.

## Palavra por palavra e linha por linha
- Um texto só, animado por partes: animate_text(clip_id, side in, text_style, unit, duration_ms?, stagger_ms?, easing?). unit char anima letra por letra, word palavra por palavra, line linha por linha; duration_ms é o tempo de cada parte e stagger_ms o atraso entre uma parte e a seguinte. Sem esses campos valem os tempos do estilo, que já seguem o escalonamento acima.
- Estilos: rise sobe para o lugar, fade surge, slide entra pela esquerda, pop cresce passando do tamanho, blur sai do desfoque, typewriter aparece de uma vez como digitação.
- Escolha estilo, unidade e curva pelo vocabulário de movimento da peça (motion-design): fade ou rise por linha no editorial, slide por palavra no cinético, pop por letra no brincalhão, blur por linha na revelação lenta, typewriter no técnico; animadores com centerOut, random ou trackingIn na coreografia tipográfica. A unidade muda o ritmo tanto quanto o estilo, e frases diferentes do mesmo vídeo podem entrar de jeitos diferentes dentro da mesma família de curvas.
- Saída: animate_text com side out; as partes saem na mesma ordem e a última termina junto com o fim do clipe. text_style none tira a animação daquele lado.
- O texto todo continua um clipe: entrance, exit, animate e transições valem por cima das partes. Texto em curva anima como uma peça só.
- Palavra de impacto: a palavra principal fica num add_text separado que entra por último, maior, com motion_preset pop ou animate_text pop.
- Combinações prontas: título com apoio, promoção, convite e título de adesivo da categoria textCombos entram com add_element(item_id, at_ms, duration_ms?, texts) como clipes de texto ligados, um por parte, já com fonte, cor e efeitos escolhidos. Anime cada clipe com animate_text: o título entra primeiro e o apoio de 2 a 4 quadros depois.
- Exemplo: [{"op":"add_text","ref":"t","text":"Oferta de hoje","style":"headline","at_ms":0,"duration_ms":3000},{"op":"animate_text","clip_id":"@t","side":"in","text_style":"blur","unit":"line"},{"op":"animate_text","clip_id":"@t","side":"out","text_style":"fade","unit":"line","stagger_ms":0}].

## Animadores de texto (seletor por faixa e tremor)
- Quando animate_text não basta (girar cada letra, colorir uma faixa que passa, abrir o espaçamento, revelar do centro para fora, tremer cada letra), use animadores: add_text_animator(clip_id, animator_preset? ou unit, animator_properties, selector_kind, campos do seletor). Um texto aceita vários animadores; eles somam posição, giro, desfoque e espaço e multiplicam escala e opacidade, na ordem da lista; move_text_animator muda essa ordem quando um animador precisa agir antes do outro.
- animator_properties é o valor no ponto mais forte da seleção: x e y em px do quadro, scale e opacity em por cento (100 não muda nada; opacity 0 esconde, scale 0 some), rotation em graus, blur em px, tracking em px depois de cada unidade, fill e stroke em #rrggbb (só em texto com cor sólida; stroke só com contorno).
- O seletor range decide quem recebe e quanto. A faixa vai de range_start a range_end (percent de 0 a 100, ou index contando unidades) e range_offset desliza a faixa. range_shape: square enche quem está dentro, rampUp sobe ao longo da faixa e deixa cheio quem passou dela, rampDown faz o contrário, triangle, round e smooth fazem um pico no meio. ease_high e ease_low suavizam o topo e a base da curva da seleção; selector_amount dosa (negativo inverte).
- range_order muda quem vem primeiro: forward lê do começo, backward do fim, centerOut abre do meio para as pontas, edgesIn fecha das pontas para o meio, random embaralha com selector_seed (a mesma semente repete o mesmo resultado).
- Revelar em sequência: opacity 0 (e um y ou scale) com range_shape rampUp, range_end entre 15 e 30 e range_offset animado de -range_end até 100 com animate_text_animator (selector_property offset). Faixa curta dá uma cascata seca; faixa longa dá uma onda macia. Esconder em sequência: rampDown com o mesmo offset.
- Onda que percorre o texto: y negativo pequeno (um terço da altura da letra) com range_shape smooth e range_end perto de 30, offset de -30 a 100 no tempo do clipe. Faixa de cor: fill com range_shape triangle no mesmo esquema.
- Espaçamento fechando: tracking positivo com range cheio (square, 0 a 100) e selector_amount animado de 100 a 0 com easeOut; as letras se juntam e a linha continua alinhada.
- Tremor em loop: selector_kind wiggly com x, y e rotation pequenos (alguns px e até 8 graus), wiggly_rate de 2 a 4, wiggly_loop_ms igual ao tempo que o texto fica na tela ou um divisor dele (o movimento se repete exatamente), wiggly_correlation 0 para cada letra ir para um lado ou perto de 100 para tremerem juntas. Tremor só em texto curto e já lido: letra que treme cansa a leitura.
- Prontos (animator_preset), gravados como valores comuns e editáveis: cascadeIn, riseIn, centerReveal, randomFade, blurIn, trackingIn, cascadeOut, wave, colorSweep, wiggle. Ajuste depois com update_text_animator.
- Tempo: as entradas prontas duram até 1 s a partir do começo do clipe; a leitura começa quando a última unidade assenta, então o ritmo de leitura acima continua valendo.
- Animadores valem junto com animate_text, com o contador (cada algarismo é uma unidade) e com texto em caminho (a posição anda ao longo da curva e o giro soma com a curva).
- Exemplo: [{"op":"add_text_animator","clip_id":"@t","unit":"char","animator_properties":{"opacity":0,"y":40,"rotation":-12},"range_shape":"rampUp","range_order":"centerOut","range_end":25},{"op":"animate_text_animator","clip_id":"@t","selector_property":"offset","keys":[{"at_ms":0,"value":-25,"easing":"easeOut"},{"at_ms":900,"value":100}]}].

## Máquina de escrever
- animate_text com text_style typewriter e unit char revela a frase crescendo, uma letra de cada vez, sem piscar.
- Cerca de 2 quadros por letra (stagger_ms 55 a 70, o padrão é 55); para uma pausa de cerca de 1 segundo numa quebra de frase, divida em dois textos.
- Cursor opcional: um retângulo fino depois do texto com opacity indo de 1 a 0 e voltando a cada 16 quadros com easeInOut; um piscar seco parece defeito.

## Destaque de palavra
- A palavra aparece primeiro; cerca de 1 segundo depois entra o destaque.
- Destaque como caixa: highlight no texto da palavra, ou um retângulo atrás dela que entra com scale de 0,9 para 1 e opacity em cerca de 18 quadros, com curva sem exagero.
- Destaque como cor: a palavra em outra cor entra por cima da original com opacity em 6 a 10 quadros.
- Um destaque por frase.
- Destaque parado no próprio texto: style_text com match pinta ou engrossa só a palavra, sem outro clipe, e a frase continua alinhada e quebrando junto. Combina com animate_text unit word: a palavra em outra cor entra no seu tempo e se lê como a batida da frase.
- As partes estilizadas andam com as suas letras: animate_text, contador e texto em caminho levam a cor, o peso e o tamanho de cada trecho junto.
- No máximo dois ou três estilos numa linha; caps para um rótulo curto e underline com parcimônia.

## Troca de palavra
- Palavras que se revezam no mesmo lugar ("rápido", "fácil", "seguro"): a que sai desfoca (blur de 0 para 6 a 10) e some enquanto a nova entra desfocada e fica nítida, em 6 a 10 quadros, no mesmo lugar.
- Reserve a largura da palavra mais longa: posicione todas pelo mesmo alinhamento para a frase em volta não pular.

## Contador
- Números que sobem (seguidores, preço, resultado) são um contador num clipe de texto, nunca vários clipes trocando: set_counter(clip_id, counter_from, counter_to, count_start_ms?, count_ms?, easing?) grava duas chaves do número e o editor desenha cada quadro com os mesmos algarismos, de largura igual, sem tremer.
- Curva: easeOut corre no começo e desacelera no fim, que é o padrão e o mais legível; spring ou backOut passam do número e voltam, bom para um resultado comemorado; linear só para relógio ou cronômetro. Contagem de 1 a 2,5 s; o número final fica parado por pelo menos 1 s.
- Formato: counter_locale pt-BR escreve 10.000 e 1.234,5; en-US escreve 10,000. decimals de 0 a 3 (preço com 2, porcentagem com 0 ou 1). grouping false tira o separador de milhar em anos e códigos. prefix e suffix com até 16 caracteres: "R$ " antes, "%", " mil" ou "+" depois.
- Prefixo e sufixo menores ou em outra cor que o número ("R$" pequeno, número grande) com style_text: as posições contam o prefixo, um caractere para o número inteiro e o sufixo, então o número muda como uma peça só.
- Mais de dois patamares (sobe, segura, sobe de novo): animate com property counter e uma chave por patamar.
- Os estilos de animate_text valem no contador (cada algarismo entra no seu tempo), e o contador também vai num caminho com set_text_path.

## Texto em caminho
- set_text_path(clip_id, path_shape arc, circle, wave ou custom, bend?, path?, path_offset?, path_align?, upright?, path_reverse?): as letras seguem a curva. arc é o mesmo arco de curve, que ele substitui; circle contorna uma elipse dentro da caixa (path_reverse true lê por dentro); wave ondula; custom usa path com coordenadas de 0 a 1 na caixa do clipe.
- Texto que corre pelo caminho: animate property pathOffset com uma chave abaixo de 0 e outra acima de 1 (por exemplo -0,5 a 1,5) faz a frase entrar por uma ponta e sair pela outra; num caminho aberto as letras somem nas pontas, num fechado dão a volta.
- Selo girando: circle com a frase dando a volta e pathOffset de 0 a 1 em 4 a 8 s com linear, num clipe que repete. upright true deixa as letras em pé em vez de virarem com a curva.
- Arco leve (bend 0,2 a 0,4) para títulos; bend acima de 0,7 só com poucas palavras, senão as letras ficam apertadas no topo.

## Onde o texto fica
- Em 9:16, deixe livres cerca de 14% no topo, 35% embaixo e 6% nas laterais por causa da interface das redes (detalhes em formatos-e-zonas-seguras).
- Contraste sempre suficiente; sobre vídeo, véu ou caixa atrás do texto.
- No máximo 3 flashes por segundo e nada de texto tremendo enquanto precisa ser lido.
- Em texto, anime scale; scaleX ou scaleY sozinhos esticam a letra, o que só cabe como efeito de impacto rápido, nunca no texto que está sendo lido.

## Anúncio de produto digital
- Texto sozinho promete; a tela mostra. Num lançamento de app, painel ou biblioteca de modelos, intercale as frases com o produto: a captura real que a pessoa mandou ou, sem ela, uma tela montada com os cartões da biblioteca (studio_library_search com category components: kpis, charts, navigation) que se constrói no ritmo das palavras.
- Os números desses cartões são de exemplo: deixe claro que é uma tela ilustrativa e nunca os use como resultado prometido.

## Conferir
- Confira cena por cena enquanto monta, não só no fim: studio_look com times_ms no momento em que cada frase fica completa e parada, e studio_look com focus_id no texto animado (ou from_ms e to_ms na troca de cena) para ver o movimento; motion_report traz valores, tempos e easing entre os quadros. É assim que você vê o vídeo tocando: nunca diga que não conseguiu conferir a reprodução.
- Leia em voz alta: se não dá para ler no tempo em que o texto fica parado, encurte a frase ou dê mais tempo.
- Defeito pequeno que você já viu (palavra cortada pela caixa, sombra escura sujando a letra no fundo escuro, marca entrando tarde) você corrige na hora, antes de responder. Na resposta só entra como pendência o que depende da pessoa: uma aprovação, um arquivo, uma escolha.
