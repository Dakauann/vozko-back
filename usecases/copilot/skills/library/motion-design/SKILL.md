---
name: motion-design
description: Como criar motion design profissional no Estúdio (princípios de animação, tempos, curvas, escalonamento, texto cinético, cenas, transições, revelação de logo e acessibilidade); carregue antes de criar qualquer animação, vinheta ou vídeo de apresentação.
---

# Motion design no Estúdio

## Como a animação funciona aqui
- animate grava keyframes de x, y (centro, de 0 a 1), scale (1 é o tamanho atual), rotation (graus) e opacity. O at_ms conta do início do clipe.
- A easing de uma chave vale para o trecho que sai dela. entrance e exit dão entradas e saídas prontas; motion_preset aplica animações completas.
- Crie e anime no mesmo lote com ref e @ref. Limites: 32 chaves por propriedade e 400 no vídeo.
- A 30 quadros por segundo, 1 quadro dura 33 ms: 100 ms são 3 quadros, 300 ms são 9, 500 ms são 15.

## Princípios (Disney, Material, Apple)
- Uma ação por vez e um elemento que manda nos outros. O que está perto do gatilho reage primeiro.
- Entrar desacelerando, sair acelerando, mover acelerando e desacelerando. Movimento linear parece mecânico.
- Antecipação discreta antes de um movimento grande (5% a 10% para o lado oposto) e acomodação depois.
- Movimento conta algo: entrada, ênfase, troca de assunto. Movimento sem motivo só cansa.

## Tempos
- Pelo tamanho do que se move: detalhe pequeno de 150 a 250 ms; elemento médio de 300 a 500 ms; tela cheia ou destaque de 500 a 800 ms.
- Entradas de 400 a 800 ms; saídas de 20% a 30% mais curtas, de 250 a 500 ms. Saída nunca mais lenta que a entrada.
- Transições de cena de 400 a 700 ms.
- Texto fica parado o tempo de leitura (cerca de 3 palavras por segundo) mais 0,5 a 1 s.
- Elemento pequeno se move rápido; elemento grande ou que atravessa a tela leva mais tempo.

## Curvas (easing)
- Entrada e chegada desaceleram; saída acelera; mudança de lugar dentro do quadro acelera e desacelera. Movimento linear parece mecânico.
- linear só para movimento constante (giro contínuo, Ken Burns lento, barra de progresso) e para trocas de opacidade.
- hold para trocas de uma vez: contador, texto que muda, efeito de máquina de escrever.
- Curvas com nome: easeOut, easeIn, easeInOut (suaves); backOut passa uns 10% do alvo e volta; backIn recua uns 10% antes de sair (antecipação); backInOut faz os dois; spring chega com impulso de mola (passa uns 16% e se acomoda); elastic vibra em volta do alvo (passa até 37%); bounce quica no alvo.
- Curvas exatas da indústria com cubic-bezier(x1,y1,x2,y2), escritas como no CSS:
  - entrada enfática, nítida e moderna: cubic-bezier(0.05,0.7,0.1,1); entrada calma: cubic-bezier(0,0,0.3,1);
  - saída enfática: cubic-bezier(0.3,0,0.8,0.15); mudança de lugar padrão: cubic-bezier(0.2,0,0,1);
  - editorial, simétrica e elegante: cubic-bezier(0.45,0,0.55,1); exagero brincalhão: cubic-bezier(0.34,1.56,0.64,1).
  - y abaixo de 0 ou acima de 1 faz a curva passar do ponto; x fica entre 0 e 1.
- Escolha pelo tom: produtivo e corporativo pede curvas sem exagero e tempos curtos; expressivo e jovem aceita backOut e spring nos momentos de destaque; elastic e bounce são cômicos, use em um elemento só e só quando o tom pedir.
- Exagero nunca em opacity (ela é limitada a 0 e 1) e nunca em todos os elementos.
- Animações prontas com essas curvas: motion_preset pop (cresce passando do tamanho), drop (cai e quica) e springIn (entra com mola).
- Escala: entrada sutil de 0,92 a 0,96 para 1; entrada marcante de 0,6 a 0,8 para 1. Nunca de 0 em elementos grandes. Com backOut, spring ou elastic o pico passa do valor da chave; o clipe animado precisa caber em 4 vezes o quadro contando esse pico.
- Deslocamentos de 2% a 8% do quadro. Ken Burns de 1 para 1,08 a 1,15 durante a cena.

## Escalonamento e texto cinético
- Elementos que entram juntos: de 67 a 133 ms entre palavras (2 a 4 quadros) e de 130 a 270 ms entre linhas, somando no máximo cerca de 600 ms.
- Uma ideia por batida, no ritmo da locução ou da música. O texto fica parado enquanto é lido.
- Direção acompanha a leitura: da esquerda para a direita e de cima para baixo. O mesmo papel usa sempre a mesma entrada.
- Receita: quebre a frase em 2 a 4 add_text com at_ms escalonados, cada um com entrance slideUp curta, e todos saem juntos com exit fade.

## Cenas
- Monte em cenas em sequência: o fundo da cena na faixa de baixo, textos e detalhes acima. Cenas em sequência reaproveitam as faixas.
- add_text sem track_id fica acima do que está na tela; um fundo novo vai numa faixa abaixo (track_id).
- Cores e contraste seguem a marca e as regras de design-de-imagem. Sombra só sobre foto ou vídeo, translúcida.

## Transições (o padrão é o corte; transição precisa de motivo)
- Crossfade de 0,5 a 1 s: passagem de tempo ou mudança de clima. A cena nova começa antes do fim da anterior, em outra faixa, com exit fade numa e entrance fade na outra.
- Mergulho na cor de 0,5 a 1,5 s: fim de capítulo ou salto grande. Retângulo de tela cheia com opacity de 0 a 1 e de volta a 0.
- Wipe de 0,4 a 0,7 s: antes e depois ou troca de item. Retângulo de tela cheia na faixa mais alta, animate em x de -0,5 a 0,5 (cobre) e de 0,5 a 1,5 (revela), easeInOut; troque o conteúdo por baixo enquanto ele cobre.
- Push: o que sai vai de x 0,5 para -0,5 e o que entra de 1,5 para 0,5, juntos e com a mesma curva. Uma direção só no vídeo.
- Zoom de 0,3 a 0,6 s para ênfase: a cena que sai cresce de 1 para 1,15 enquanto some; a que entra vai de 0,9 para 1 enquanto aparece. Use pouco.
- Um tipo de transição no vídeo inteiro, variando no máximo na abertura e no fechamento.

## Logo e marca
- A marca aparece cedo, nos primeiros 5 segundos, e volta ao longo do vídeo; não deixe o logo só para o fim.
- Revelação de logo de 2 a 4 s, com o logo final parado por pelo menos 1 s. Receita: opacity de 0 a 1 e scale de 0,9 para 1 com easeOut em cerca de 600 ms.
- O logo entra como mídia (add_media com o media_id do anexo) com fit contain, sem distorcer, com área de respiro.

## Acessibilidade
- No máximo 3 flashes por segundo. Nada de tremor de tela cheia nem zoom em excesso.
- Nunca passe informação só pelo movimento: o texto continua legível parado.

## Erros comuns
- Movimento linear, bounce em tudo, tudo animando ao mesmo tempo ou a mesma entrada em todos os elementos.
- Elementos pequenos com mais de 800 ms. Texto que some antes de ser lido.
- Direção trocando a cada cena. Exagero em opacidade.

## Conferir
Corrija os erros do campo checks de cada studio_edit_video antes da próxima cena. Veja com studio_look em times_ms no começo, no meio e no fim de cada entrada.
