---
name: tipografia-cinetica
description: Como animar texto no vídeo como em motion design profissional (palavra por palavra, linha por linha, escalonamento, máquina de escrever, destaque de palavra, troca de palavra, contador, ritmo de leitura e áreas seguras dos vídeos curtos); carregue antes de criar títulos animados, frases de impacto, vinhetas com texto ou vídeos só de texto.
---

# Tipografia cinética

Texto animado serve à leitura: o movimento leva o olho para a próxima ideia e para assim que ela precisa ser lida. A 30 quadros por segundo, 1 quadro dura 33 ms. Tempos, curvas e o resto do motion estão em motion-design; o estilo das legendas da fala está em legendas-e-texto.

## Ritmo de leitura
- Cerca de 3 palavras por segundo. Cada texto fica parado na tela por (palavras ÷ 3) + 0,5 a 1 segundo depois de terminar de entrar.
- De 1 a 4 palavras por batida em vídeo curto; uma ideia por batida, no ritmo da locução ou da música.
- O texto termina de entrar antes de começar a ser lido: entradas de texto de 250 a 500 ms.

## Escalonamento
- Entre letras: 1 a 2 quadros (33 a 67 ms). Entre palavras: 2 a 4 quadros (67 a 133 ms). Entre linhas: 4 a 8 quadros (130 a 270 ms).
- A cascata inteira cabe em cerca de 600 ms; frase longa pede escalonar por linha, não por palavra.
- Todas as partes usam a mesma entrada e a mesma curva; a variação vem só do atraso.
- A ordem segue a leitura: da esquerda para a direita e de cima para baixo.

## Palavra por palavra e linha por linha
- Cada palavra ou linha é um add_text com at_ms escalonado, todas com a mesma fonte, tamanho e alinhamento, posicionadas como a frase montada.
- Entrada típica: entrance slideUp curta, ou animate em y subindo 2% a 4% do quadro com cubic-bezier(0.05,0.7,0.1,1) e opacity de 0 para 1.
- Saída: todas juntas com exit fade, ou escalonadas na mesma ordem e mais rápidas que a entrada.
- Palavra de impacto: a palavra principal entra por último, maior, com motion_preset pop ou scale de 0,8 para 1 com backOut.

## Máquina de escrever
- Revele o texto aumentando a frase, nunca piscando letra por letra: uma sequência de clipes de texto com a frase crescendo, cada um trocado no ponto exato, sem transição.
- Cerca de 2 quadros por letra (15 letras por segundo) e uma pausa de cerca de 1 segundo nas quebras de frase.
- Cursor opcional: um retângulo fino depois do texto com opacity indo de 1 a 0 e voltando a cada 16 quadros com easeInOut; um piscar seco parece defeito.

## Destaque de palavra
- A palavra aparece primeiro; cerca de 1 segundo depois entra o destaque.
- Destaque como caixa: highlight no texto da palavra, ou um retângulo atrás dela que entra com scale de 0,9 para 1 e opacity em cerca de 18 quadros, com curva sem exagero.
- Destaque como cor: a palavra em outra cor entra por cima da original com opacity em 6 a 10 quadros.
- Um destaque por frase.

## Troca de palavra
- Palavras que se revezam no mesmo lugar ("rápido", "fácil", "seguro"): a que sai desfoca (blur de 0 para 6 a 10) e some enquanto a nova entra desfocada e fica nítida, em 6 a 10 quadros, no mesmo lugar.
- Reserve a largura da palavra mais longa: posicione todas pelo mesmo alinhamento para a frase em volta não pular.

## Contador
- Números que sobem (seguidores, preço, resultado): clipes com o número trocando em passos, rápidos no começo e mais lentos no fim, terminando no número final parado por pelo menos 1 segundo.

## Onde o texto fica
- Em 9:16, deixe livres cerca de 14% no topo, 35% embaixo e 6% nas laterais por causa da interface das redes (detalhes em formatos-e-zonas-seguras).
- Contraste sempre suficiente; sobre vídeo, véu ou caixa atrás do texto.
- No máximo 3 flashes por segundo e nada de texto tremendo enquanto precisa ser lido.

## Conferir
Veja com studio_look no meio de cada cascata e no ponto em que a frase fica completa. Leia em voz alta: se não dá para ler no tempo em que o texto fica parado, encurte a frase ou dê mais tempo.
