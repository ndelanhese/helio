# Task 2 — Bootstrap, Login e Onboarding

## Resultado

- Criadas as rotas `/bootstrap` e `/login`, com guarda global baseada no estado de bootstrap e sessão.
- Redirect de login preserva somente destinos relativos seguros da mesma origem.
- Login hidrata sessão e CSRF no React Query/memória; nenhum segredo é persistido no navegador.
- Onboarding em cinco etapas envia uma única operação atômica com todos os campos configuráveis.
- Defaults: 7 painéis × 610 W, PV1 ativo, PV2 inativo, porta 8899, slave 1 e retenção 730 dias.
- Erros locais/422 focam e descrevem o campo; 429 usa `Retry-After` em contagem regressiva.
- Direção editorial solar responsiva, controles de 44 px+, tema claro/escuro e revisão com segredos redigidos.

## TDD e verificação

- RED observado: `npm --prefix web test -- --run LoginForm OnboardingWizard` falhou por componentes ausentes.
- GREEN inicial: `npm --prefix web test -- --run` — 11 arquivos, 33 testes aprovados.
- Axe executado nos formulários em temas claro e escuro, sem violações (contraste excluído no jsdom).
- `npm --prefix web run typecheck` — aprovado.
- `npm --prefix web run lint` — aprovado, zero warnings.
- `npm --prefix web run build` — aprovado, 1.808 módulos transformados.

## Auto-revisão

- Senhas limitadas localmente a 12–128 bytes e série mascarada por padrão/revisão.
- Double-submit protegido por latch síncrono além do estado visual de loading.
- IPv4 privado é orientação de UI; aceitação final permanece sob autoridade do servidor.
- Build altera o diretório embarcado rastreado; artefatos gerados foram restaurados para manter o commit no escopo-fonte.

## Observação

- A regra axe de contraste precisa de layout real e ficou desabilitada no jsdom; contraste visual deve continuar na checklist de navegador/E2E.

## Correções após revisão

- Redirect de retorno agora usa `URL`, compara origem exata, normaliza path/query/hash e rejeita barras invertidas, formas codificadas e loops.
- O handler 401 global preserva path, query e hash e não redireciona recursivamente em login/bootstrap.
- Parsers rejeitam vazio, não finito, inteiro inseguro e decimal inválido; zero explícito de tarifa permanece válido.
- Série limitada a decimal uint32; senha usa mínimo em pontos de código Unicode e máximo em bytes UTF-8.
- Timezone usa validação IANA real; moeda usa `Intl.supportedValuesOf` com fallback integral compatível com o backend.
- Todos os campos do bootstrap têm mapeamento seguro de 422, etapa e foco. O backend ainda não envia metadado de campo: o frontend depende dos códigos estáveis `invalid_request`, `invalid_password`, `invalid_settings` e de fragmentos estáveis das mensagens, sem refletir detalhes do servidor.
- Login e bootstrap usam trava síncrona contra double-submit; 409 refaz o status e sempre segue para login.
- Token de eyebrow claro passou a usar `#173B2D` (AA sobre o canvas); amarelo permanece em fundo escuro/masthead.
- Contagem regressiva usa anúncio `polite`, MPPT expõe `aria-invalid`, e falhas de rede/servidor têm mensagens distintas.

### Verificação da revisão

- RED observado separadamente para redirects/401, 22 casos de schema/mapeamento, double-submit, 409, contraste, fallback ISO e falha do refetch.
- GREEN final: `npm --prefix web test -- --run` — 14 arquivos, 73 testes aprovados.
- `typecheck`, `lint` e `build` aprovados; `git diff --check` limpo.

## Correções finais da revisão

- Moedas agora usam sempre uma constante TypeScript gerada do conjunto autoritativo de `internal/config/validation.go`, sem delegar autoridade ao `Intl`.
- Um teste de drift compara a constante frontend com o validador Go inteiro; códigos especiais como XAU, XTS, XXX, BOV, CHE, CLF e USN permanecem aceitos.
- IPv4 exige quatro octetos ASCII decimais não vazios, sem espaços, sinais ou zeros à esquerda ambíguos, cada um entre 0 e 255.
- Tarifa aceita somente decimal não negativo com até duas casas; conversão para unidade menor usa `BigInt`, sem expoente/arredondamento e só retorna inteiro JSON seguro.
- Rate limit anuncia uma mensagem estável uma única vez em região `polite`; a contagem regressiva visual fica fora da região e com `aria-hidden`.

### Verificação final

- RED observado para conjunto monetário especial, IPv4 ambíguo, tarifa inexata/overflow e live region mutável.
- GREEN: `npm --prefix web test -- --run` — 15 arquivos, 96 testes aprovados.
- `typecheck`, `lint`, `build` e `git diff --check` aprovados.
