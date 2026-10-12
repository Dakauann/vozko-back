---
name: critica-de-design
surface: studio
description: Como avaliar e refinar uma arte depois de compor (olhar com studio_look, checklist de acabamento, falhas graves, nota de 0 a 32 em oito critérios, duas rodadas e quando refazer o conceito em vez de polir); carregue antes de dizer que uma arte está pronta.
---

# Crítica de design

Primeiro olhe: studio_look, descreva o que está de fato na tela e julgue contra o objetivo da peça, não contra gosto. Qual é o objetivo, que elementos servem a ele, eles funcionam, por quê.

Julgue cada coisa com o instrumento certo: a imagem para hierarquia, clima, equilíbrio e conceito; os números para o que o olho erra. view guides confirma alinhamentos e ângulos (linhas comuns e inclinações quase paralelas) e traz layout, view wireframe acha camada escondida, caixa vazia e empilhamento errado, legibility traz o contraste de cada texto medido nos pixels atrás dele e focus_id aproxima um elemento para conferir detalhe.

O olho, o seu e o de qualquer modelo, erra sombra: direção trocada, sombra que girou junto com uma camada inclinada, sombra cortada pela borda e sombra escura sumindo em fundo escuro passam despercebidas na imagem. Por isso:
- alerts vem em todo studio_look: são os problemas medidos no documento (sombras, alinhamento, camadas cobertas e, no vídeo, saltos, flashes, saídas da tela e textos que se cruzam). Leia antes de julgar a imagem; cada linha diz o que mediu e como resolver, e você decide se é de propósito.
- view depth desenha, na prancheta ou em cada quadro do vídeo, uma seta na direção de cada sombra (vermelha quando contraria as outras), as bordas que cortam sombras e as camadas cobertas, e traz depth: a luz geral com a concordância entre as sombras, cada sombra com direção, deslocamento, blur, alfa e a cor sobre a qual cai, e o que cada camada esconde. Use ao revisar profundidade, sempre que a peça tiver sombras em camadas inclinadas e sempre que o pedido falar em sombras, profundidade, camadas ou volume: antes de entregar, olhe com view depth e siga as receitas de profundidade dramática (profundidade-e-camadas).
- view guides traz layout: o que está alguns px fora de uma linha comum, o menor respiro de cada lado, quem quase encosta em quem, sobreposições, os níveis de texto com a razão entre eles e a ordem de leitura. Use ao revisar alinhamento e hierarquia.
- No vídeo, olhar com focus_id ou from_ms traz motion_report: cada movimento com valores, tempos e easing, mais os problemas daquela janela.
No vídeo, depth e layout vêm por quadro. alerts também vem em studio_read. A pessoa liga as mesmas marcas no menu Exibir do editor de imagem e nos botões da barra do editor de vídeo (Sombras e camadas cobertas, Problemas de layout).

## O que de fato mudou
- Antes de julgar ou de dizer que está certo, confira o resultado da última edição: applied, refused e not_applied. Uma operação em refused ou em not_applied não está na tela, mesmo que a imagem pareça boa.
- Uma mudança recusada se corrige e reenvia, ou se conta ao usuário como não feita. Nunca a apresente como feita nem a descreva como outra coisa.

## Checklist
- Ideia: qual é a ideia em uma frase, e um modelo pronto já teria isso? Se teria, recuse o conceito: a peça está bem executada mas sem ideia, e o ganho costuma estar numa metáfora que não seja a foto literal do produto e no elemento de assinatura (direcao-de-arte), não em mais acabamento. Sinais de que ainda é o briefing executado ao pé da letra: o produto centralizado com o texto do pedido em volta, um repertório que qualquer marca do segmento usaria.
- Eixo: quando o layout de view guides avisa que a composição está toda centrada ou espelhada, pergunte se a simetria serve à ideia (selo, emblema, retrato de frente) ou se é só o ponto de partida. É informação, não regra: se não serve, experimente um eixo deslocado, uma diagonal ou um elemento que quebre o espelho.
- Miniatura: em cerca de 10% do tamanho, o herói e a mensagem ainda se leem. Em 3 segundos: o quê, quando ou qual oferta, e a ação.
- Alinhamento: cada elemento numa margem, numa linha da grade ou na borda de outro elemento. Quase alinhado (diferença de 0.005 a 0.02) parece erro.
- Óptica: caixa alta centralizada fica de 1% a 2% acima do centro geométrico; pontuação fica para fora de uma borda alinhada à esquerda.
- Ritmo: espaço dentro dos grupos no máximo 1/3 do espaço entre grupos.
- Texto: sem palavra sozinha na última linha, quebras que seguem o sentido, caixa alta pequena espaçada, título grande apertado, números alinhados em preço e data, nenhuma letra esticada, um efeito por palavra, caixa alta e centralizado só em texto curto.
- Contagens: um herói, no máximo 3 tamanhos de texto, no máximo 4 cores além da foto, acento em no máximo 2 lugares, decoração dentro do orçamento (formas-e-decoracao).
- Imagem: nada esticado ou pixelado, recorte limpo, sem defeitos de IA (mãos, texto derretido), logos não espelhados.
- Bordas: nada a menos de 0.02 da borda por acidente; o que sangra passa pelo menos 0.03 dela.
- Uma direção de luz, uma família de cantos arredondados, uma família de traço.
- Teste de remoção: apague um elemento; se nada se perdeu, mantenha apagado.
- Fatos, acentos (ç, ã) e zonas seguras.

## Falhas graves
Corrija antes de dar nota: informação chave ilegível, fato errado, item do pedido faltando, texto com linha escondida pela própria caixa, fora da zona segura, logo ou imagem distorcidos, contraste insuficiente na informação chave, tela piscando mais de 3 vezes por segundo. As linhas "erro" do campo checks e de alerts entram aqui.

## Nota
Cada critério de 0 a 4 (0 quebrado, 1 padrão genérico, 2 competente mas seguro, 3 distinto com detalhes a acertar, 4 com direção de arte e nada a tirar):
1. Conceito e especificidade
2. Hierarquia e leitura em 3 segundos
3. Composição (o arquétipo executado com tensão e espaço com propósito)
4. Tipografia (voz, par, escala, espaçamento, quebras)
5. Cor e tratamento da imagem
6. Profundidade e integração entre texto e imagem
7. Detalhe e acabamento
8. Adequação ao pedido e ao formato

De 0 a 32: de 28 a 32, entregue; de 22 a 27, corrija os um ou dois critérios mais fracos; de 16 a 21, troque o arquétipo ou o conceito em vez de mexer em detalhes; abaixo de 16, recomece pela frase do conceito (direcao-de-arte).

## Conceito, depois acabamento
Ideia e acabamento não competem: uma peça distinta e mal acabada perde para uma comum bem feita, e o objetivo é a distinta bem feita. Com a ideia e a assinatura no lugar, faça duas passadas:
1. Corte: tire tudo o que não serve à ideia (enfeite solto, efeito a mais, moldura ou monograma que o gênero não pede, texto que repete outro).
2. Acabamento: alinhamento numa grade, ritmo de espaços, escala de tipo com degraus claros, contraste, nenhum ornamento perdido, os mesmos cantos e os mesmos efeitos em tudo. A assinatura passa pelo mesmo crivo: executada com capricho e sem custar a leitura.

## Rodadas
- Rodada 1: todas as falhas graves e todo critério com 2 ou menos, num lote só.
- Rodada 2: confira, corrija só o que ainda falha e pare. Pare antes se alerts, checks e a imagem não mostram nada novo.
- Se conceito ou composição continuam em 2, refaça: polir um layout genérico dá um layout genérico polido.
- Ao entregar, diga em uma frase o conceito e a escolha principal, sem listar a nota.

## Cobertura do pedido
Último passo antes do resumo final, mesmo quando a peça já parece pronta:
1. Liste o que o pedido exige: cada texto citado (nome, preço, data, horário, endereço, chamada), cada seção ou bloco (numa ficha, cada atributo, barra e quadro), cada elemento visual nomeado (uma faixa arredondada, uma fita com pontas dobradas, a bebida do combo) e o formato. Numa réplica, a lista é cada texto e cada bloco da referência.
2. Chame studio_read e marque item por item contra o documento: texto pelo conteúdo exato, bloco e elemento pela camada que o desenha. A imagem não serve para isso; ela esconde o que ficou fora da arte, atrás de outra camada ou cortado pela caixa.
3. Item sem marca entra numa nova rodada, num lote só, e a lista é conferida de novo. Só termine com item faltando se não houver como fazer, e diga ao usuário qual ficou de fora e por quê.
