# Helio Alexa relay

Relay público para skill e widget nativo do Echo Show. Helio continua na rede local; somente um resumo assinado sai para a VPS.

```text
Helio local -- HTTPS + HMAC --> Coolify relay --> Alexa Data Store --> Echo Show widget
                                      ^
                                      +---------- Alexa skill requests
```

Dados enviados: potência atual, energia do dia, estado, indicador de dado desatualizado e horário da leitura. Sem IP/serial do inversor, coordenadas, credenciais, histórico completo ou banco SQLite.

## 1. Criar a skill

1. No [Alexa Developer Console](https://developer.amazon.com/alexa/console/ask), crie uma **Custom Skill**, idioma **Português (BR)** e hosting **Provision your own**.
2. Copie o **Skill ID** no formato `amzn1.ask.skill...`.
3. Em **Tools > Permissions**, copie **Alexa Client Id** e **Alexa Client Secret**.

O backend não usa AWS Lambda. Alexa chama o relay por HTTPS.

## 2. Subir no Coolify

Crie um recurso Docker apontando para este repositório:

| Campo | Valor |
|---|---|
| Dockerfile | `/integrations/alexa-relay/Dockerfile` |
| Build context | raiz do repositório |
| Porta | `3000` |
| Domínio | `https://helio-alexa.ndelanhese.online` |
| Health check | `/healthz` |
| Volume persistente | `/data` |

Gere segredo compartilhado:

```sh
openssl rand -base64 32
```

Configure variáveis do recurso:

```dotenv
PORT=3000
STATE_FILE=/data/state.json
HELIO_TIMEZONE=America/Sao_Paulo
ALEXA_DATASTORE_ENDPOINT=https://api.amazonalexa.com
ALEXA_SKILL_ID=amzn1.ask.skill.REPLACE_ME
ALEXA_CLIENT_ID=REPLACE_ME
ALEXA_CLIENT_SECRET=REPLACE_ME
HELIO_SHARED_SECRET=BASE64_GERADO_ACIMA
```

`ALEXA_CLIENT_SECRET` e `HELIO_SHARED_SECRET` devem ser secrets no Coolify. DNS precisa apontar `helio-alexa.ndelanhese.online` para a VPS. Coolify termina TLS com certificado público; Alexa exige HTTPS na porta 443.

Após deploy:

```sh
curl --fail https://helio-alexa.ndelanhese.online/healthz
curl --fail https://helio-alexa.ndelanhese.online/privacy
```

Resposta do health check: `{"status":"ok"}`.

## 3. Publicar pacote da skill

Instale e autentique [ASK CLI v2](https://developer.amazon.com/en-US/docs/alexa/smapi/quick-start-alexa-skills-kit-command-line-interface.html):

```sh
npm install -g ask-cli
ask configure
cd integrations/alexa-relay
ask init
```

No `ask init`, selecione skill existente e o Skill ID criado no passo 1. Confirme que `skillMetadata.src` aponta para `./skill-package`. Depois:

```sh
ask deploy --target skill-metadata
```

Isso publica manifest, modelo pt-BR e pacote `HelioWidget`; não provisiona Lambda. No Developer Console:

1. **Build > Build Model**.
2. **Multimodal Responses > Widget > Helio Solar**.
3. Use **Install > Send to Device** no Echo Show vinculado à mesma conta.
4. Habilite a skill em **Test > Skill testing is enabled in: Development**.

Comandos de voz:

- “Alexa, abrir hélio solar”
- “Alexa, perguntar ao hélio solar quanto estou gerando”

## 4. Ligar Helio local

Use o mesmo segredo gerado no passo 2:

```dotenv
HELIO_ALEXA_RELAY_URL=https://helio-alexa.ndelanhese.online/ingest
HELIO_ALEXA_SHARED_SECRET=BASE64_GERADO_ACIMA
```

Reinicie o container Helio. Primeira leitura é enviada imediatamente; mudanças seguintes são consolidadas e enviadas no máximo uma vez por minuto. Falhas de internet não interrompem coleta ou armazenamento local.

Não publique porta HTTP do Helio. Apenas relay Coolify recebe tráfego público.

## Operação

- `/data/state.json` guarda último resumo e até 20 IDs de dispositivos Alexa; monte volume persistente.
- Pedidos `/alexa` usam verificação oficial de certificado, assinatura, timestamp e Skill ID.
- Pedidos `/ingest` aceitam até 8 KiB, exigem HTTPS, HMAC-SHA256 e timestamp com tolerância de cinco minutos; o relay limita tentativas e aceita até dez pedidos autenticados por minuto.
- Troca de segredo exige atualizar relay e Helio local.

Referências: [web service customizado](https://developer.amazon.com/en-US/docs/alexa/custom-skills/host-a-custom-skill-as-a-web-service.html), [widgets APL](https://developer.amazon.com/en-US/docs/alexa/alexa-presentation-language/create-and-manage-widgets.html), [Data Store REST API](https://developer.amazon.com/en-US/docs/alexa/alexa-presentation-language/data-store-rest-api-reference.html).
