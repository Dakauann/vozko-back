---
name: principios-de-animacao
description: Os princípios clássicos de animação aplicados ao motion design do Estúdio (antecipação, exagero, continuidade e sobreposição, arcos, tempo, encenação, ação secundária) e receitas de movimentos (tremer, pulsar, batimento, cair e quicar, girar, balançar, zoom, explosão de partículas, revelar); carregue junto com motion-design quando a animação parecer mecânica, rígida ou sem vida.
---

# Princípios de animação no Estúdio

motion-design dá os tempos e as curvas. Esta habilidade diz como fazer o movimento parecer vivo. A 30 quadros por segundo, 1 quadro dura 33 ms.

## Os princípios, traduzidos para animate
- **Desacelerar e acelerar.** Nada começa nem para de repente. Entrada desacelera (easeOut ou cubic-bezier(0.05,0.7,0.1,1)); saída acelera (easeIn ou cubic-bezier(0.3,0,0.8,0.15)).
- **Antecipação.** Antes de um movimento grande, um recuo curto para o lado oposto: de 2 a 4 quadros e de 5% a 10% da distância. Faça com backIn no trecho que sai, ou com uma chave a mais (recuo e depois o movimento).
- **Exagero e acomodação.** Passe de 4% a 10% do alvo e volte em 4 a 8 quadros: backOut, spring ou cubic-bezier(0.34,1.56,0.64,1). Elastic e bounce são exagero cômico.
- **Continuidade e sobreposição.** Partes de um conjunto não chegam juntas: a principal chega primeiro e as secundárias de 2 a 4 quadros depois (at_ms escalonados), cada uma com a mesma curva.
- **Arcos.** Coisas vivas andam em curva. Anime x e y com curvas diferentes: x com linear ou easeInOut e y com easeOut faz um arco que sobe e assenta; inverta para um arco que cai.
- **Tempo é peso.** Pequeno e leve se move rápido (150 a 250 ms); grande e pesado leva mais (500 a 800 ms) e acomoda com menos exagero.
- **Encenação.** Um movimento principal por vez. Enquanto o herói se move, o resto fica parado ou quase parado.
- **Ação secundária.** Um detalhe que reforça a principal sem disputar com ela: o selo gira 6 graus quando o produto chega, a sombra cresce quando o título cai.
- **Profundidade.** O que está longe é menor, mais lento e desfocado (blur de 4 a 10); o que está perto é maior, mais rápido e nítido.
- **Estilo constante.** O mesmo papel usa sempre o mesmo movimento no vídeo inteiro; o tom escolhido (sóbrio ou brincalhão) vale para todos os elementos.

## Receitas
- **Tremer (erro, alerta, impacto).** x oscilando 1% a 2% do quadro em volta da posição, chaves a cada 2 quadros por 6 a 10 quadros, linear, terminando na posição original.
- **Pulsar.** motion_preset pulse, ou scale de 1 para 1,06 a 1,1 e de volta com easeInOut, a cada 400 a 600 ms.
- **Batimento.** Dois pulsos rápidos (1 para 1,12, de volta, 1 para 1,08, de volta, em cerca de 300 ms) e uma pausa de cerca de 500 ms antes de repetir.
- **Cair e quicar.** motion_preset drop, ou y de cima até a posição com bounce em 700 a 1000 ms. Para uma queda acelerando antes do quique, um trecho com easeIn até o chão e o quique depois.
- **Girar e balançar.** motion_preset spin para giro contínuo (linear); wobble para balanço curto. Balanço vivo: rotação de 6 a 10 graus para os dois lados, diminuindo a cada ida.
- **Zoom de impacto.** scale de 0,6 para 1 com backOut ou spring em 300 a 450 ms, junto com opacity de 0 para 1 nos primeiros 150 ms (motion_preset pop).
- **Explosão de partículas.** De 8 a 20 formas pequenas (círculos ou estrelas) no mesmo ponto; cada uma vai para fora numa direção com easeOut em 400 a 700 ms, cai um pouco no fim (y com easeIn no último trecho) e some com opacity. Varie direção, distância e um atraso de 0 a 3 quadros entre elas.
- **Revelar com foco.** opacity de 0 para 1 e blur de 16 a 24 para 0 em 300 a 500 ms com easeOut; escala de 1,04 para 1 junto deixa a revelação mais rica.
- **Objeto que entra empurrando outro.** O que entra chega com backOut; o que é empurrado se move 2 a 3 quadros depois, na mesma direção, com distância menor.

## Combinar
Os movimentos somam: quicar e girar, pulsar e deslizar, cair e espalhar partículas. Combine no máximo dois por elemento e mantenha um herói por cena.

## Conferir
Veja com studio_look em times_ms no começo, no pico do exagero e no fim de cada movimento. Se tudo se move ao mesmo tempo, escalone; se parece duro, troque a curva; se parece mole, encurte o tempo.
