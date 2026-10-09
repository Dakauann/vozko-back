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

## Como montar
- A transição fica no corte entre dois clipes que se encostam na mesma faixa visual e não move nenhum deles: add_transition com clip_id do clipe que sai e transition_kind. Sem to_clip_id, ela vai para o clipe que encosta logo depois.
- Os tipos: dissolve (fusão), dipToColor (mergulho na cor; color #ffffff faz o flash branco), push (direction), slide (direction e moving in ou out), wipe (angle: 0 varre para a direita, 90 para baixo), iris (iris_shape circle, box ou diamond; reverse fecha o clipe que sai), clockWipe (angle de início, clockwise), split (orientation; reverse fecha), zoom (amount e origin_x, origin_y), whip (direction e amount de desfoque) e blurDissolve (amount).
- duration_ms vai de 100 a 4000 e cabe na metade de cada clipe; alignment center (padrão), start começa no corte, end termina no corte. dipToColor, zoom e whip trocam de clipe no meio e só ficam em center.
- easing aceita as mesmas curvas das chaves (menos hold): easeInOut ou cubic-bezier(0.2,0,0,1) para push e slide, linear para dissolve e mergulho, backOut para um push com exagero.
- Os clipes precisam se encostar: aproxime com move_clip ou trim_clip antes. Um vídeo mostra o último quadro antes do corte durante a parte da transição que passa dele, como nos editores profissionais sem sobra de mídia; se isso aparecer, prefira um tipo que troca no meio (dipToColor, zoom, whip) ou alignment start ou end.
- Ajuste com update_transition(transition_id, ...) e tire com remove_transition. studio_read mostra as transições de cada faixa com window_ms.
- Exemplo: [{"op":"add_transition","ref":"t","clip_id":"ID_DA_CENA_1","transition_kind":"push","direction":"left","duration_ms":500,"easing":"cubic-bezier(0.2,0,0,1)"}].
- Luz ou película por cima continua sendo uma mídia na faixa mais alta, centrada no corte (blend_mode screen quando for texto ou forma de cor).

## Áudio na transição
- Corte J ou L: arraste o áudio da fala para começar antes ou terminar depois da imagem.
- Em crossfade e mergulho, fade de áudio do mesmo tamanho nos dois clipes; num corte seco, um fade de 2 a 4 quadros evita estalo.

## Conferir
Veja com studio_look no início, no meio e no fim de cada transição. O meio do mergulho precisa estar totalmente coberto; o push não pode mostrar fundo vazio entre as cenas.
