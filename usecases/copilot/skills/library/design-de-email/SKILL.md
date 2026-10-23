---
name: design-de-email
surface: email
description: Como desenhar um e-mail marketing que é lido e clicado (brief, assunto e preheader, estrutura para escanear, uma ação principal, celular primeiro, imagens com IA e texto ao vivo, contraste e modo escuro, personalização, entregabilidade, rodapé e LGPD) e o fluxo das ferramentas do editor de e-mail; carregue antes de criar, refazer ou revisar qualquer e-mail.
---

# Design de e-mail

Um e-mail é decidido em segundos, quase sempre no celular: a pessoa vê remetente, assunto e preheader, abre, passa o olho no topo e decide se toca no botão. Desenhe para esse caminho.

## Antes de criar
- Junte o brief: o único objetivo do e-mail (vender um produto, levar a um evento, dar boas-vindas), quem recebe (o segmento), a oferta e o prazo, o link de destino, a marca (logo, cores, fotos do usuário) e os dados do rodapé (nome da empresa e endereço).
- O rascunho vem primeiro. Monte o e-mail com o que já sabe e deixe à vista o que falta: um botão com o rótulo certo e sem link, o rodapé com os textos de exemplo, um bloco de imagem esperando a foto. Essas lacunas voltam em open_gaps e não impedem o rascunho; só impedem salvar e enviar.
- Depois do rascunho na tela, peça numa só chamada de ask_user, com opções quando couber, tudo o que só o usuário tem (link de destino, nome, endereço, dados da oferta). Não pergunte o que dá para propor: proponha e mostre.
- Nunca invente links, nomes, endereços, preços nem prazos: o que não veio do usuário ou das ferramentas fica como lacuna.
- Negócio de nicho com regras próprias de publicidade (médicos, dentistas, advogados, imóveis): carregue também nicho-medicina, nicho-odontologia, nicho-advocacia ou nicho-imobiliario antes de escrever, porque o que vale para anúncio vale para o e-mail.
- Logo e fotos do usuário são material da marca: use o media_id dos anexos ou da biblioteca. Nunca gere um logo no lugar do logo dele.

## Assunto e preheader
- O assunto mora na campanha, não no design; quando ajudar, sugira dois ou três assuntos ao usuário.
- Assunto curto (até uns 50 caracteres aparecem inteiros no celular), concreto, sem caixa alta e sem pontuação em excesso.
- O preheader (em settings) completa o assunto, nunca repete: o assunto traz a promessa e o preheader o detalhe que faz abrir. Entre 40 e 90 caracteres.

## Estrutura para escanear
- Pirâmide invertida: topo com a promessa (imagem herói ou título), duas ou três linhas de apoio, o botão principal, depois os detalhes em blocos curtos, um reforço da ação e o rodapé.
- Uma ação principal. O mesmo link pode aparecer no herói, no botão e no reforço; ações secundárias vão como link no texto, não como outro botão igual.
- Hierarquia: um título de nível 1, seções com nível 2, itens com nível 3, sem pular nível.
- Parágrafos de até três linhas no celular. Negrito só no que a pessoa precisa ler se ler uma coisa só.
- Respiro: espaçador e padding separam ideias; divisória só quando muda de assunto.

## Celular primeiro
- Uma coluna é o padrão. Colunas servem para cartões pares (produtos, quando e onde) e empilham no celular, então cada coluna precisa fazer sentido sozinha, na ordem.
- Largura do e-mail de 600 px (width em settings); acima de 640 alguns apps cortam.
- Botão principal com full_width e rótulo de 2 a 4 palavras que começa com verbo e diz o que acontece: "Ver ofertas", "Garantir minha vaga". Rótulos longos quebram em duas linhas no celular.

## Imagens e texto ao vivo
- O que decide o clique (oferta, preço, prazo, ação) fica em texto ao vivo: aparece com imagens bloqueadas, é lido por leitores de tela, se adapta ao modo escuro e ajuda a entrega.
- Texto dentro de uma imagem gerada é permitido e às vezes é a própria peça (um selo de desconto, um cartaz da campanha). Quando a mensagem principal estiver na imagem, repita o essencial em texto ao vivo perto dela e no alt.
- Equilíbrio: um e-mail que é uma imagem só cai no spam e some com imagens desligadas. Cada imagem pede texto de verdade ao redor.
- O alt descreve o que a imagem comunica, com a oferta quando houver: "Tênis de corrida azul por R$ 199". Nunca "imagem" nem o nome do arquivo.
- A imagem herói leva ao mesmo destino do botão (href).

## Imagens com IA
- email_generate_image gera e já coloca a imagem num bloco: block_id troca a imagem de um bloco que existe, at cria um bloco novo. Ela fica na biblioteca com um endereço https público e permanente.
- Formato: landscape para o herói na largura toda, square para produtos em colunas, portrait raramente.
- Prompt de diretor de arte: assunto, cenário, luz, estilo e paleta da marca, ponto de vista e espaço livre perto de onde o texto ao vivo vai ficar. Mande os anexos do usuário em reference_media_ids para seguir produto, rosto ou estilo.
- Combine o fundo da imagem com o fundo do e-mail (content_background_color) para a imagem não parecer colada.
- Gere uma de cada vez, confira com email_look e ajuste o prompt se precisar. Uma imagem que já veio nunca se gera de novo: o media_id dela entra num bloco de imagem pelo email_edit. Projetos exportados do Estúdio também estão na biblioteca e entram pelo media_id.

## Cor e contraste
- Neutros no fundo (fundo externo claro, conteúdo branco ou quase), uma cor de destaque da marca nos botões e nos links.
- Contraste mínimo de 4,5 para 1 no texto e de 3 para 1 em títulos grandes; a review aponta onde não chega. Texto do botão claro sobre cor escura ou escuro sobre cor clara, nunca dois tons médios.
- Fundo de bloco (background_color) destaca uma oferta ou uma faixa; use pouco.

## Modo escuro
- Alguns apps invertem as cores sozinhos (Gmail no celular, Outlook) e outros mostram o e-mail sobre um fundo escuro. Veja com email_look e scheme dark.
- Logos e ícones escuros em PNG transparente somem no escuro: prefira o logo sobre um fundo claro dentro da imagem ou uma versão com contorno.
- Texto ao vivo se adapta; texto escuro dentro de imagem transparente não.

## Personalização e segmentos
- Merge tags sempre com reserva: {{first_name|cliente}}. Use o nome onde soa natural (saudação, às vezes o título), não em todo parágrafo.
- Campos do CRM entram como {{field.chave}}, só com chaves que existem.
- Segmentos diferentes pedem e-mails diferentes: o mesmo esqueleto com herói, oferta e ação trocados. Crie um design por segmento com new_email_design, ou duplique e adapte os blocos.

## Entregabilidade
- Links https para o domínio do próprio negócio, sem encurtadores, e poucos links por e-mail.
- E-mail leve: o Gmail corta mensagens com HTML acima de uns 100 KB e esconde o resto, inclusive o rodapé. Prefira poucos blocos bem escritos a dezenas de parágrafos.
- Imagens leves: o email_look avisa arquivo pesado ou resolução baixa para telas densas.

## Rodapé e LGPD
- Todo e-mail termina no rodapé, com o nome da empresa e um endereço verdadeiro. No Brasil isso não é uma exigência geral da lei para e-mail marketing (é regra nos Estados Unidos, para quem envia a destinatários de lá); aqui é uma prática forte, recomendada pela autorregulação do setor: identifica quem envia, passa confiança e ajuda a entrega. Nunca diga ao usuário que a lei exige o endereço.
- Troque os textos de exemplo pelos dados do usuário; enquanto ele não informar, eles ficam como lacuna visível no rascunho.
- O link para sair da lista entra sozinho em todo envio de marketing; não crie outro. O texto do rodapé diz por que a pessoa recebe ("Você recebe porque se cadastrou na nossa loja").
- Para consentimento, listas importadas e dados sensíveis, carregue lgpd-no-marketing.

## Fluxo no editor
1. email_read para saber o que já existe e quais ids usar.
2. Diga em poucas palavras o plano (estrutura, tom, imagens).
3. email_edit em lotes: primeiro a estrutura e os textos, depois cores e settings, depois as imagens. Dê ref aos blocos novos e use @ref no mesmo lote. O que depende do usuário entra como lacuna, sem travar o lote.
4. email_look no computador e no celular; aproxime com focus_id para ler detalhes; até duas rodadas de ajuste.
5. email_look com scheme dark quando houver logo, fundo colorido ou imagem transparente.
6. Peça numa só pergunta os dados das lacunas (open_gaps), feche as que dependem só de você, pese hints e checks e termine lembrando o usuário de tocar em Salvar quando open_gaps vier vazio.

## Receitas
- Promoção: herói com o produto, título com o benefício ou o desconto, prazo em texto ao vivo, botão cheio, condições curtas no fim.
- Newsletter: título da edição, um destaque com imagem e botão, de 2 a 4 notícias em cartões (colunas 50-50) com link no texto, rodapé.
- Novidade ou aviso: título claro do que mudou, quando vale, o que a pessoa precisa fazer, botão só se houver ação.
- Evento: convite no título, quando e onde em duas colunas, botão de confirmar presença, lembrete de vagas.
- Boas-vindas: obrigado, o que ela vai receber e com que frequência, um único próximo passo.
- Carrinho abandonado ou reativação: o produto ou o motivo para voltar, uma imagem, um botão de volta e ajuda por resposta ao e-mail.
