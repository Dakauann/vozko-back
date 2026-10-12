---
name: transicoes
surface: studio
description: Quando cortar e quando usar transição, o catálogo das transições profissionais com a duração de cada uma (corte, J e L, crossfade, mergulho na cor, flash, push, cobrir, wipe, varredura em tons, persiana, blocos, leque, zoom, chicote, glitch, luz), como montar cada uma no Estúdio e como fazer um clipe entrar ou sair com qualquer uma delas; carregue antes de ligar cenas, trocar de assunto, revelar um logo ou montar abertura e encerramento.
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
| Varredura em tons (a imagem aparece na ordem de um mapa, do escuro para o claro) | 18 a 36 quadros (0,6 a 1,2 s) | clima, abertura elegante; com a borda suave parece tinta ou fumaça |
| Persiana (lâminas abrem juntas) | 12 a 24 quadros | troca de item com cara de vitrine, catálogo |
| Blocos (aparece em quadradinhos soltos) | 10 a 20 quadros | tecnologia, revelação divertida |
| Leque (abre do centro para os dois lados) | 12 a 24 quadros | revelar um produto ou um selo, contagem |
| Zoom com desfoque | 6 a 15 quadros (0,2 a 0,5 s) | energia, ênfase |
| Chicote (passagem rápida desfocada) | 5 a 10 quadros | esconder o corte num movimento |
| Glitch | 3 a 8 quadros | tecnologia, empolgação; cuidado com flashes |
| Luz ou película queimada por cima | 15 a 30 quadros | estilo de vida, clima quente |

## Como montar
- A transição fica no corte entre dois clipes que se encostam na mesma faixa visual e não move nenhum deles: add_transition com clip_id do clipe que sai e transition_kind. Sem to_clip_id, ela vai para o clipe que encosta logo depois.
- Os tipos: dissolve (fusão), dipToColor (mergulho na cor; color #ffffff faz o flash branco), push (direction), slide (direction e moving in ou out), wipe (angle: 0 varre para a direita, 90 para baixo), iris (iris_shape circle, box ou diamond; reverse fecha o clipe que sai), clockWipe (angle de início, clockwise), split (orientation; reverse fecha), zoom (amount e origin_x, origin_y), whip (direction e amount de desfoque), blurDissolve (amount), gradientWipe (wipe_map linear com angle, radial ou diamond com origin_x e origin_y, clouds em manchas, ou wipe_map_media_id com uma imagem da biblioteca; softness deixa a borda suave e reverse faz o claro aparecer primeiro), venetianBlinds (angle e piece_size, a largura de cada lâmina), blockDissolve (piece_size, o lado de cada bloco) e radialWipe (angle de início, 0 é meio-dia, e origin_x, origin_y). Os quatro últimos aceitam softness de 0 (corte seco) a 1.
- duration_ms vai de 100 a 4000 e cabe na metade de cada clipe; alignment center (padrão), start começa no corte, end termina no corte. dipToColor, zoom e whip trocam de clipe no meio e só ficam em center.
- easing aceita as mesmas curvas das chaves (menos hold): easeInOut ou cubic-bezier(0.2,0,0,1) para push e slide, linear para dissolve e mergulho, backOut para um push com exagero.
- Os clipes precisam se encostar: aproxime com move_clip ou trim_clip antes. Um vídeo mostra o último quadro antes do corte durante a parte da transição que passa dele, como nos editores profissionais sem sobra de mídia; se isso aparecer, prefira um tipo que troca no meio (dipToColor, zoom, whip) ou alignment start ou end.
- Ajuste com update_transition(transition_id, ...) e tire com remove_transition. studio_read mostra as transições de cada faixa com window_ms.
- Exemplo: [{"op":"add_transition","ref":"t","clip_id":"ID_DA_CENA_1","transition_kind":"push","direction":"left","duration_ms":500,"easing":"cubic-bezier(0.2,0,0,1)"}].
- Luz ou película por cima continua sendo uma mídia na faixa mais alta, centrada no corte (blend_mode screen quando for texto ou forma de cor).

## Entrada e saída com transição
- Um clipe sozinho também entra ou sai com uma transição, contra o que está abaixo dele: entrance ou exit com transition_kind no lugar de effect, mais duration_ms, easing e as opções do tipo. Serve para revelar um logo com wipe sobre o vídeo, abrir um selo em leque, fazer um produto aparecer em blocos ou sumir um título numa varredura em tons.
- A transição é medida na caixa do próprio clipe: o wipe atravessa o logo, não o quadro inteiro, e origin_x e origin_y são frações do clipe. Push, slide e whip trazem o clipe de fora do quadro.
- Vale todo tipo visual menos dissolve; para só aparecer ou sumir, use effect fade. Em slide o lado decide (entrada entra, saída sai), então moving não vale. Mergulho na cor, zoom e chicote usam a metade do clipe esticada na borda inteira: a entrada nasce da cor ou do zoom e a saída termina nele.
- Cada lado tem uma coisa só: transition_kind troca o fade e o deslize daquele lado, e effect (até none) troca a transição. Um lado que já tem transição de corte com o vizinho não aceita entrada ou saída própria; mude o corte com update_transition ou tire com remove_transition.
- Entrada e saída somadas cabem no clipe; uma entrada longa demais encurta para o espaço que sobra e a resposta avisa.
- Durações boas: 300 a 600 ms para logo, selo e texto curto; 600 a 1000 ms para varredura em tons com borda suave. Texto que vai ser lido entra rápido e fica parado.
- Exemplo: [{"op":"entrance","clip_id":"ID_DO_LOGO","transition_kind":"radialWipe","angle":180,"origin_y":1,"softness":0.15,"duration_ms":500,"easing":"easeOut"},{"op":"exit","clip_id":"ID_DO_LOGO","transition_kind":"gradientWipe","wipe_map":"clouds","softness":0.4,"duration_ms":700}].
- studio_read mostra transition_in e transition_out de cada clipe com as opções em uso.

## Áudio na transição
- Corte J ou L: arraste o áudio da fala para começar antes ou terminar depois da imagem.
- Em crossfade e mergulho, fade de áudio do mesmo tamanho nos dois clipes; num corte seco, um fade de 2 a 4 quadros evita estalo.

## Conferir
Veja com studio_look no início, no meio e no fim de cada transição. O meio do mergulho precisa estar totalmente coberto; o push não pode mostrar fundo vazio entre as cenas.
