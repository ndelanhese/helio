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
- GREEN final: `npm --prefix web test -- --run` — 11 arquivos, 33 testes aprovados.
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
