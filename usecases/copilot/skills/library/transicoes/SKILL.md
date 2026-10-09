---
name: transicoes
description: Quando cortar e quando usar transição, o catálogo das transições profissionais com a duração de cada uma (corte, J e L, crossfade, mergulho na cor, flash, push, cobrir, wipe, zoom, chicote, glitch, luz) e como montar cada uma no Estúdio; carregue antes de ligar cenas, trocar de assunto ou montar abertura e encerramento.
---

# Transições

O padrão é o corte. Uma transição precisa significar algo: passagem de tempo, troca de lugar, mudança de clima ou um respiro na estrutura. Transição em todo corte deixa o vídeo amador. A 30 quadros por segundo, 1 quadro dura 33 ms.

## Regras
- Uma família de transição no vídeo inteiro, variando no máximo na abertura e no encerramento.
- Uma direção só para empurrões e deslizes no vídeo inteiro (por exemplo, sempre da direita para a esquerda, como quem avança).
- A transição acompanha o som: corte e transição caem na batida ou no fim de uma frase da locução.
- Nada de transição em cima de texto que está sendo lido.

## Catálogo
| Transição | Duração | Quando |
|---|---|---|
| Corte seco, corte no movimento, corte por semelhança | 0 | quase sempre |
| Corte J ou L (o som entra antes ou sai depois da imagem) | 0,5 a 2 s de som | fala e entrevista, para o corte não pesar |
| Crossfade | 12 a 30 quadros (0,4 a 1 s) | passagem de tempo, mudança de clima, montagem |
| Mergulho no preto | 15 a 45 quadros (0,5 a 1,5 s) | fim de capítulo, salto grande, abertura e encerramento |
| Mergulho no branco | 6 a 20 quadros (0,2 a 0,7 s) | flash, lembrança |
| Flash branco curto | 2 a 6 quadros | impacto na batida |
| Push (os dois andam juntos) | 10 a 20 quadros (0,3 a 0,7 s) | próximo item de uma sequência |
| Cobrir (o novo entra por cima) | 10 a 20 quadros | trocar de assunto mantendo o ritmo |
| Wipe (uma borda revela) | 12 a 24 quadros (0,4 a 0,8 s) | antes e depois, troca de item |
| Zoom com desfoque | 6 a 15 quadros (0,2 a 0,5 s) | energia, ênfase |
| Chicote (passagem rápida desfocada) | 5 a 10 quadros | esconder o corte num movimento |
| Glitch | 3 a 8 quadros | tecnologia, empolgação; cuidado com flashes |
| Luz ou película queimada por cima | 15 a 30 quadros | estilo de vida, clima quente |

## Como montar hoje
As cenas ficam em faixas: a cena que sai numa faixa e a que entra em outra, sobrepostas pelo tempo da transição. Use a mesma curva nos dois lados.
- **Crossfade.** A cena nova começa antes do fim da anterior, em outra faixa acima; exit fade na que sai e entrance fade na que entra, com a mesma duração.
- **Mergulho na cor.** Um retângulo de tela cheia na faixa mais alta, com opacity de 0 a 1 até o meio do mergulho e de volta a 0; o corte entre as cenas acontece embaixo dele no instante em que está opaco.
- **Flash.** O mesmo retângulo branco, de 0 a 1 e de volta a 0 em 2 a 6 quadros, centrado no corte.
- **Push.** O que sai vai de x 0,5 para -0,5 e o que entra de 1,5 para 0,5, juntos, com easeInOut ou cubic-bezier(0.2,0,0,1).
- **Cobrir.** Só o novo se move (x de 1,5 para 0,5 com cubic-bezier(0.05,0.7,0.1,1)); o antigo fica parado embaixo, ou recua um pouco (scale de 1 para 0,95) e escurece.
- **Wipe.** Um retângulo de tela cheia na faixa mais alta, animate em x de -0,5 a 0,5 (cobre) e de 0,5 a 1,5 (revela), easeInOut; troque o conteúdo por baixo enquanto ele cobre.
- **Zoom com desfoque.** A cena que sai cresce de 1 para 1,15 e desfoca (blur de 0 para 20) enquanto some; a que entra vai de 1,1 para 1 e de blur 20 para 0 enquanto aparece.
- **Chicote.** As duas cenas andam rápido na mesma direção (como o push) em 5 a 10 quadros, com blur subindo para 24 a 40 no meio e voltando a 0; a curva é easeIn na que sai e easeOut na que entra.
- **Luz por cima.** Uma mídia de vazamento de luz ou película (com blend_mode screen se for um texto ou forma de cor) na faixa mais alta, centrada no corte.

## Áudio na transição
- Corte J ou L: arraste o áudio da fala para começar antes ou terminar depois da imagem.
- Em crossfade e mergulho, fade de áudio do mesmo tamanho nos dois clipes; num corte seco, um fade de 2 a 4 quadros evita estalo.

## Conferir
Veja com studio_look no início, no meio e no fim de cada transição. O meio do mergulho precisa estar totalmente coberto; o push não pode mostrar fundo vazio entre as cenas.
