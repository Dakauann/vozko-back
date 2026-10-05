---
name: anuncios-para-site
description: Anúncios que levam para um site (tráfego e vendas): pixel e API de Conversões, qual resultado otimizar, página de destino coerente e erros que desperdiçam verba; carregue antes de criar ou avaliar um anúncio com destino site.
---

# Anúncios para site

## Tráfego ou vendas
- OUTCOME_TRAFFIC entrega cliques baratos de pouca intenção; serve quando a visita é o objetivo (conteúdo, blog) ou para alimentar o remarketing.
- Para vender ou gerar cadastro no site, use OUTCOME_SALES (ou OUTCOME_LEADS com o site) otimizando para o evento que importa: compra, cadastro, início de checkout. A Meta entrega para quem tende a fazer aquilo para que você otimiza.
- Sem pixel funcionando e sem eventos chegando, não use vendas: a Meta não tem o que aprender. Comece arrumando o rastreamento.

## Rastreamento confiável
- Pixel no site com os eventos do caminho de compra (ver conteúdo, adicionar ao carrinho, iniciar checkout, compra) e o valor da compra.
- API de Conversões (envio pelo servidor) junto com o pixel, mandando os mesmos eventos com o mesmo identificador de evento, para a Meta não contar duas vezes. Isso recupera conversões que o navegador perde.
- Qualidade de correspondência dos eventos boa ou ótima: quanto mais dados do cliente o servidor envia (com consentimento), mais conversões a Meta reconhece e menor o custo.
- No Vozko: list_ad_pixels, create_ad_pixel e get_ad_conversion_settings; as conversões do CRM também podem ir para a Meta.

## Página de destino
- A página precisa cumprir o que o anúncio promete: mesma oferta, mesmo preço, mesmo produto, logo de cara.
- Rápida no celular. Cada segundo de carregamento perde visitantes, e a Meta reprova páginas que não funcionam ou que impedem a pessoa de sair.
- Um único próximo passo claro (comprar, cadastrar, chamar no WhatsApp).
- Link https válido; teste antes de publicar.

## Ler os números
- Cliques no link e visualizações da página de destino são diferentes: muita diferença entre os dois indica página lenta ou clique acidental.
- Avalie pelo custo por compra ou por cadastro e pelo retorno sobre o investimento, não pelo custo por clique.
- Veja testes-e-atribuicao para entender como a Meta conta as conversões.
