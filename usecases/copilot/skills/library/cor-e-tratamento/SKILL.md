---
name: cor-e-tratamento
surface: studio
description: Cor de cartaz e post no Estúdio (paleta tirada do assunto ou da marca, estratégia de cor antes das amostras, disciplina do acento, os contrastes de Itten como ferramenta, foto puxada para a paleta, tema escuro e claro, degradês caros e baratos); carregue ao escolher cores ou quando a peça parecer sem identidade.
---

# Cor e tratamento

Contraste mínimo e legibilidade estão em design-de-imagem. Aqui fica a identidade.

## Da marca ou do assunto
- A cor principal da marca vem primeiro. Sem marca, tire cinco cores da foto: a sombra mais funda, um meio-tom, a cor do assunto, uma luz e um complemento para o acento.
- Quatro famílias de clima saem da mesma foto: viva (saturação e luminosidade altas), suave (saturação baixa, luminosidade alta), profunda (saturação alta, luminosidade baixa) e escura (as duas baixas). Uma família por peça.

## Estratégia antes das amostras
- Contida: neutros com um acento. Luxo, corporativo, editorial.
- Comprometida: uma cor dona de 30% a 60% da área. A maioria dos posts.
- Paleta cheia: 3 ou 4 cores com papéis definidos. Varejo, infantil, pop.
- Encharcada: a peça inteira numa cor, com tons dela. Noite, música, lançamentos.

## Disciplina do acento
- Um acento, em no máximo 10% da área, em uma ou duas coisas (palavra chave ou chamada, e um eco no motivo).
- No total, 3 a 4 cores além da foto.

## Contrastes como ferramenta (Itten)
- Claro e escuro: legibilidade e drama.
- Quente e frio: profundidade (quente vem para frente).
- Complementar: vibração, sempre com áreas desiguais.
- Saturação: um campo apagado e um elemento saturado parece o mais caro de todos.
- Extensão: uma área pequena e saturada equilibra uma grande e neutra.
- Simultâneo: um neutro ao lado de uma cor forte puxa para a complementar; tinja os neutros com a cor da peça.

## Foto puxada para a paleta
- Sombras na direção do tom mais escuro da paleta, luzes na direção do mais claro (camadas de tratamento em profundidade-e-camadas).
- A cor do assunto vira o acento ou é neutralizada; fundo menos saturado, assunto mais saturado.
- Uma série com fotos de origens diferentes (gerados um a um, recortes num fundo novo) pede uma luz só: iguale temperatura, exposição e contraste de cada foto com set_color, comparando com studio_look e artboard_id all, e amarre com o mesmo gradient_map em intensity baixa (0,2 a 0,4). O duotom cheio esconde a diferença, mas apaga a cor do produto; serve quando essa cor não importa.

## Correção de cor de foto e vídeo
- set_color corrige imagens (layer_id) e clipes de vídeo ou imagem (clip_id) como um colorista: primeiro a luz, depois a cor, por último o look.
- Luz: exposure em stops (0,3 a 0,7 salva uma foto escura; acima de 1,5 estoura o céu). Contraste de 10 a 30 dá corpo; highlights negativo recupera céu e pele brilhante; shadows positivo abre rostos no contraluz; blacks positivo desbota os pretos para um ar de filme.
- Cor: corrija a temperatura e o tint até o branco parecer branco antes de criar clima. Prefira vibrance a saturation para avivar sem queimar a pele; saturation entre -15 e 20 em quase tudo.
- Looks (look e look_intensity) são pontos de partida: warm, cool, faded, mono, punchy, dramatic, soft e vintage. Use de 0,4 a 0,8 de intensidade e ajuste color_grade por cima. Um look para a peça ou o vídeo inteiro, igual em todos os clipes.
- Combine os clipes de um vídeo: a mesma temperatura e a mesma exposição percebida entre cenas. Confira com studio_look lado a lado.
- reset_color tira tudo; um valor 0 em color_grade tira só aquele ajuste.

## Curvas, cor seletiva, LUT e mapa de gradiente
- A ordem da imagem é fixa: ajustes de luz e cor, LUT, curvas, cor seletiva e, por último, o mapa de gradiente. Pense nessa ordem ao montar um tratamento.
- Curvas (curves) refinam o tom depois dos ajustes: um S suave em master (curves_preset contrast) dá corpo sem estourar; levantar o primeiro ponto de master (fade) lava os pretos para um ar de filme. Curvas por canal criam clima: puxar o azul para cima nas sombras e para baixo nas luzes faz o contraste azul e laranja. Poucos pontos, sempre com x crescente; mexa de 0,03 a 0,08 por ponto.
- Cor seletiva (hsl) destaca uma cor: color com a cor do produto e desaturate 100 deixa só ela colorida e o resto em preto e branco; width de 30 a 60 e softness de 20 a 40 evitam bordas duras. shift troca a cor de um objeto (um carro vermelho vira azul) sem tocar no resto; saturation só nela reforça ou apaga a cor da marca.
- LUT (lut_media_id) aplica um look de cinema de um arquivo .cube que a pessoa importou para a biblioteca; comece com lut_intensity de 0,5 a 0,8 e corrija exposição e balanço de branco antes, porque o LUT espera uma imagem neutra.
- Mapa de gradiente (gradient_map) troca os tons por cores de marca, um duotom; em vídeo use intensity de 0,3 a 0,6 para manter a leitura do rosto.
- A correção também anima: animate com property exposure, saturation, temperature e as outras de color_grade faz uma cena sair do preto e branco para a cor, esquentar no momento da revelação ou escurecer antes de um texto. Duas chaves bastam na maioria dos casos.
- clear_color tira só uma parte (curves, hsl, lut ou gradient_map); look troca os ajustes de luz e cor e mantém o resto.
- Num clipe de efeito (o clipe de ajuste), set_color vale para tudo o que está abaixo dele durante o seu tempo: é o jeito de dar um look único a um vídeo montado com câmeras diferentes, sem repetir a correção em cada clipe. Corrija cada clipe primeiro (exposição e balanço de branco) e ponha o look no ajuste; as chaves de exposure e saturation no ajuste fazem a cena toda sair do preto e branco de uma vez.
- Para tratar só uma parte da tela, ponha uma máscara no clipe de ajuste (add_mask): uma elipse suave mais clara no rosto, uma faixa mais fria no céu. A força do clipe (opacity em update_clip) dosa o look inteiro.
- Se um LUT não abrir (arquivo apagado ou corrompido), a prévia mostra listras no lugar dele e a exportação e o studio_look recusam dizendo qual clipe ou camada usa o LUT; importe o .cube de novo e aponte com lut_media_id, ou tire com clear_color lut. Nada sai sem o LUT.

## Tema escuro e claro
- Escuro: quase preto tingido com a cor da peça (luminosidade 8 a 15, como #12101a), texto em branco tingido, acento de croma alto, brilho só no que emite luz.
- Claro: papel tingido em vez de branco puro (o branco puro combina com suíço e tecnologia limpa), tinta quase preta da cor da peça, uma versão mais escura do acento para texto pequeno.

## Degradês
- Caros: tons vizinhos (até 60 graus de distância na roda de cores) ou uma cor do claro ao escuro; de 10 a 25 pontos de luminosidade atrás de texto; radial e fora do centro como uma fonte de luz (cy 0.30 a 0.40); uma cor do meio saturada (via) para não passar pela zona cinza; grão por cima contra faixas.
- Baratos: arco-íris, complementares passando pelo cinza, índigo para roxo (o sinal de IA mais conhecido), dois tons saturados na vertical, degradê em todos os elementos.
