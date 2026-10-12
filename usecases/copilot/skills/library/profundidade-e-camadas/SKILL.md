---
name: profundidade-e-camadas
surface: studio
description: Como dar profundidade a uma arte no Estúdio (ordem dos planos, texto atrás da pessoa com remoção de fundo, quebrar a moldura, sombras de contato, brilho, vazamento de luz, vinheta, tratamento de cor da foto, duotone e atmosfera); carregue ao empilhar camadas ou tratar fotos.
---

# Profundidade e camadas

Ordem típica, de baixo para cima: cor ou degradê do fundo, foto de fundo, camadas de tratamento, texto de trás, recorte da pessoa ou do produto, texto da frente e rótulos, luz, textura, informação sobre um campo limpo. De 3 a 6 planos dão uma peça com profundidade. order_layers com above_id ou below_id posiciona com precisão.

## Recortar o fundo
studio_start_job kind cutout não custa nada e costuma ser o caminho profissional quando:
- fotos de produto têm fundos diferentes entre si ou confusos, ou uma série precisa parecer uma sessão só;
- o produto ou a pessoa vai para a cor da marca ou para uma cena nova;
- o texto passa atrás do assunto, em adesivos e recortes com contorno (strokes) e em composições.

Costuma não valer quando o ambiente é a mensagem (estilo de vida, o clima do lugar) ou quando a borda ficaria falsa: cabelo solto, vidro, fumaça, tecido transparente. Depois do corte, veja a borda com studio_look e focus_id no recorte; halo ou serrilhado pedem outro caminho.

O recorte perde o chão e a luz que tinha. Devolva os dois:
- um fundo consistente embaixo: a cor da marca, um degradê ou um mesmo fundo gerado para todos;
- sombra de contato sob o produto (receita em Sombras e luz), no nível de elevação e na direção de luz da peça;
- luz em comum com set_color ou gradient_map, para os recortes parecerem da mesma foto (cor-e-tratamento);
- numa série, a mesma escala e a mesma linha de base em todos os cartões: o produto pousa na mesma altura e ocupa uma área parecida.

Ao propor correções para fotos que não combinam, ponha o recorte ao lado das opções pagas, como gerar de novo, e diga qual é grátis.

## Texto atrás da pessoa
1. add_image da foto, sangrando a arte.
2. duplicate_layers da foto com ref: a cópia fica logo acima, um pouco deslocada; no mesmo lote, update_layer na cópia com o mesmo x, y, w e h da foto de baixo para as duas coincidirem.
3. add_text do título e order_layers com above_id na foto de baixo: o texto fica entre as duas.
4. studio_start_job kind cutout com o layer_id da cópia de cima. O editor troca a imagem dela pelo recorte quando ficar pronto; continue o resto da peça enquanto isso e acompanhe com studio_jobs.
5. A pessoa cobre de 15% a 35% das letras. Sombra suave no recorte: cor no tom mais escuro da paleta com alfa (por exemplo #1a0f0866 numa paleta marrom), x 0, y de 0.5% a 1% da altura, blur de cerca de 2% da altura.
6. Opcional: uma cópia da palavra só de contorno acima do recorte, para as letras escondidas continuarem sugeridas.
7. Veja com studio_look se ficaram halos no cabelo.

## Quebrar a moldura
Uma forma (rect, ellipse, path_preset arch ou um path), a foto logo acima com clip true e o recorte da mesma foto acima de tudo, saindo de 5% a 15% da altura da moldura por cima ou por um lado só.

## Sombras e luz
- Uma direção de luz para a peça inteira: todas as sombras projetadas apontam para o mesmo lado, com a mesma proporção entre x e y (luz de cima à esquerda dá x e y positivos em todas; luz de cima, com x 0, é a mais neutra). Uma sombra para o lado oposto denuncia montagem.
- Elevação em poucos níveis, sempre os mesmos na peça; três costumam bastar. Encostado (botão, cartão): y de 1% a 2% da altura do objeto, blur de 1 a 2 vezes o y, alfa de 0.2 a 0.3. Flutuando (cartão de destaque, produto): y de 4% a 6%, blur de 1.5 a 2 vezes, alfa de 0.15 a 0.25. Saltando da peça: y de 8% a 12%, blur de 2 vezes, alfa de 0.1 a 0.2 e uma sombra curta de contato junto. Quanto mais alto, maior o deslocamento e o blur e mais suave a sombra.
- Sombra com a cor da superfície: o matiz de onde ela cai, mais escuro e um pouco menos saturado, sem virar cinza (sobre laranja, um marrom avermelhado translúcido como #5a1e0066); cinza neutro sobre fundo colorido e preto puro costumam sujar a peça.
- Sombra em camadas: effects com duas drop-shadow, uma curta e firme junto ao objeto e outra longa e suave, imita luz real melhor que uma só.
- Sombra longa chapada (estilo flat): um path que sai do objeto até a borda a 45 graus, na cor do fundo um pouco mais escura, sem blur.
- Sombra de contato: só embaixo do que pousa numa superfície (produto na mesa, pessoa em pé, objeto no chão), encostada na base dele: ellipse com w de 0.8 a 1.0 e h de 0.06 a 0.10 do objeto, no tom escuro da superfície, opacity de 0.3 a 0.5, com degradê radial do escuro para transparente na borda. O que flutua de propósito ganha a drop-shadow de flutuando, não uma mancha embaixo.
- Brilho, só no que emite luz (neon, farol, tela, sol, vela, chama) e sobre fundo mais escuro que ele: outer-glow ou shadow com a mesma cor, mais saturada, x 0, y 0 e blur alto; ou uma ellipse com degradê radial da cor para transparente atrás dele, blend_mode screen, opacity de 0.4 a 0.7. Num fundo claro o halo não acende nada e só suja a cor.
- Vazamento de luz: add_vfx lightLeak, do lado da luz da foto, com color e color2 da paleta; um por peça, quando o tema pede clima de filme ou nostalgia (doses em efeitos-visuais).
- Vinheta: add_vfx vignette na prancheta, amount de 0.2 a 0.35 e softness alta; forte e preta deixa a peça com cara de barata (efeitos-visuais).
- Mancha radial não é sombra nem luz por padrão. Uma ellipse com degradê radial só entra quando explica algo que se vê: o contato de quem pousa, a luz de quem brilha, o foco de um holofote do conceito. Halo atrás do produto num fundo claro, mancha escura atrás de um texto, sombra que não encosta em nada e vários halos na mesma peça denunciam montagem: tire, e separe com a drop-shadow da elevação, um campo de cor, um contorno ou contraste de valor.

## Tratamento de cor da foto
- Ajustes na própria foto: filters (brightness, contrast, saturation) ou filter_preset.
- Foto sangrando a arte inteira: camadas de cor da arte inteira por cima com blend_mode:
  - unificar com uma cor da paleta: blend_mode color, opacity de 0.15 a 0.35;
  - quente e frio: degradê de frio para quente, blend_mode overlay ou soft-light, opacity de 0.2 a 0.4 (pele é quente, sombra fria separa a pessoa do fundo);
  - escurecer a área do texto: tom escuro da paleta, blend_mode multiply, opacity de 0.3 a 0.6, só sobre essa área;
  - filme fosco: azul acinzentado escuro, blend_mode screen, opacity de 0.1 a 0.2.
- Foto dentro de uma forma ou de outra camada: ajuste com filters e use, se precisar de tinta, uma camada de cor logo acima com clip true e opacity baixa.
- Duotone: saturation no mínimo e contrast mais alto na foto; acima dela, um rect da cor clara com blend_mode multiply e um rect da cor escura com blend_mode screen ou lighten. A cor escura perto de luminosidade 15 a 25, a clara acima de 75. Pares: marinho e rosa choque, violeta e amarelo ácido, verde floresta e menta, vinho e pêssego. Funciona melhor com a foto ocupando a arte inteira.

## Atmosfera e escala
- O que está longe fica mais frio, mais desfocado (filters blur) e com menos contraste; o que está perto, quente, nítido e saturado. Um leve desfoque no fundo atrás de um recorte separa os planos.
- Profundidade por escala: um elemento enorme cortado na borda e um pequeno ao longe.

## Profundidade dramática
Quando o pedido fala em profundidade, sombras, camadas ou volume, uma sombra suave por elemento não basta: a peça precisa de três planos que se leem de longe (fundo, meio e frente). Combine duas ou três receitas:
- Uma luz só: decida o ângulo antes da primeira sombra (de cima à esquerda é o mais comum) e repita a mesma proporção entre x e y em toda sombra projetada, no lado claro dos degradês e no brilho. A seta de cada sombra em view depth aponta para o mesmo lado.
- Planos em camadas: de 3 a 5 painéis, cartões ou formas empilhados, cada plano um pouco maior, mais claro ou mais saturado que o de trás, cada um no seu nível de elevação. O da frente tem o maior deslocamento e o maior blur.
- Sobreposição de verdade: o herói passa por cima de um painel, de uma faixa ou do título em 10% a 30% da área dele; encostar não cria profundidade, sobrepor cria. Acerte a ordem com order_layers e above_id.
- Sombra projetada e sombra de contato juntas: tudo que pousa ganha uma sombra longa e suave na direção da luz e uma curta, escura e estreita bem embaixo (receita de contato em Sombras e luz). Sem a de contato, o objeto flutua.
- Título em camadas duras: duas ou três cópias do título com duplicate_layers, deslocadas em passos iguais na direção da sombra, em tons cada vez mais escuros da cor do fundo e sem blur, e por cima de tudo uma drop-shadow larga e suave no conjunto.
- Desvanecer pela atmosfera: o plano de trás com opacity de 0.3 a 0.6, mais frio, com filters blur leve e menos contraste; um degradê da cor do fundo para transparente sobre a base de um plano faz ele sumir na distância.
- Contraste de escala: um elemento de 2 a 4 vezes o tamanho do resto, cortado na borda, e outro pequeno ao longe; só a diferença de tamanho já cria distância.
- Moldura de primeiro plano: uma forma escura ou desfocada na frente (folhagem, arco, borda de janela, uma faixa) sangrando um ou dois cantos, com opacity de 0.6 a 0.9, enquadra o assunto e empurra o resto para trás.
- Brilho no escuro: num fundo escuro, o objeto que emite luz ganha um halo atrás (ellipse com degradê radial da cor para transparente, blend_mode screen) e o fundo escurece nas bordas com add_vfx vignette; a luz parece sair do objeto. Um halo por peça, no herói que brilha.
- Antes de entregar, olhe com studio_look e view depth: a luz geral com concordância perto de 1, nenhuma sombra cortada pela borda e nenhuma camada coberta sem querer.
