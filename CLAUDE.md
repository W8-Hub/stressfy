# CLAUDE.md

Guia para o Claude Code (e qualquer agente) trabalhar neste repositório.

## O que é

`stressfy` é uma API HTTP (Fastify + TypeScript) para **testes de estresse da máquina**.
Ela expõe endpoints que criam *jobs* de carga controlada sobre CPU, RAM, disco e rede.
Cada job roda por uma duração definida, pode ser agendado para o futuro e pode ser
cancelado a qualquer momento. Limites de segurança são aplicados via variáveis de ambiente.

Todo o serviço vive em um único arquivo: [src/index.ts](src/index.ts).

## Comandos

```bash
npm install        # instala dependências
npm run dev        # roda em watch via tsx (src/index.ts)
npm run build      # compila TypeScript para dist/ (tsc)
npm start          # roda o build (node dist/index.js)
```

Docker:

```bash
docker build -t stressfy .
docker run -p 3333:3333 stressfy
```

Não há suíte de testes nem linter configurados. Ao validar mudanças, suba o servidor
(`npm run dev`) e exercite os endpoints com `curl`.

## Arquitetura

Tudo está em [src/index.ts](src/index.ts). Conceitos centrais:

- **Job** (`type Job`): unidade de carga em memória, guardada no `Map` global `jobs`.
  Tem `status`, `AbortController`, workers de CPU, buffers de RAM, arquivos de disco e
  métricas acumuladas. Não há persistência — reiniciar o processo zera tudo.
- **Ciclo de vida do status**: `scheduled` → `running` → `stopping` → `finished` /
  `failed` / `cancelled`.
- **CPU**: um `Worker` (worker_threads) por núcleo, executando código inline
  (`CPU_WORKER_CODE`) que alterna janelas de busy/idle para atingir o percentual alvo.
- **RAM**: aloca `Buffer`s em chunks e toca as páginas (escreve 1 byte a cada 4 KB)
  para forçar alocação real de memória residente. Alvo por `ramMb` ou `ramPercent`.
  O limite real respeita cgroups (v2/v1) antes de cair em `os.totalmem()`.
- **Disco**: `runDiskWrite` escreve um arquivo; `runDiskRead` cria um arquivo-semente
  e o relê em loop. Ambos suportam throttling por `mbps` e respeitam o sinal de abort.
- **Rede**: `runNetworkWrite` faz POST em streaming para uma URL; `runNetworkRead` faz
  GET em streaming. Os endpoints `/net/source` e `/net/sink` servem como contraparte
  local (gera/absorve tráfego) para testar rede sem depender de serviços externos.

### Endpoints

| Método | Rota | Função |
| --- | --- | --- |
| `GET` | `/health` | Status detalhado: memória, contagem de jobs por status |
| `GET` | `/healthz` | Liveness mínimo (`{ ok: true }`) |
| `GET` | `/ready` | Readiness |
| `POST` | `/jobs` | Cria um job de stress (corpo JSON, veja abaixo) |
| `GET` | `/jobs` | Lista todos os jobs |
| `GET` | `/jobs/:id` | Detalhe de um job |
| `POST` | `/jobs/:id/stop` | Cancela/para um job |
| `GET` | `/net/source?mb=&chunkMb=` | Gera tráfego de download (para `networkRead`) |
| `POST` | `/net/sink` | Absorve tráfego de upload (para `networkWrite`) |

### Convenções importantes

- **Aliases de campos**: `normalizeRequest` aceita formas curtas e longas —
  `start`/`startAt`, `time`/`durationSec`, `cpu`/`cpuPercent`, `ram`/`ramPercent`.
  Ao editar o schema do request, mantenha os dois nomes funcionando.
- **`POST /jobs` aceita query params além do body**: ambos são mesclados
  (`{ ...query, ...body }`), com o body tendo precedência.
- **Tudo é "clampado"**: use `clampNumber(value, min, max, fallback)` para qualquer
  entrada numérica nova. Os tetos vêm das env `MAX_*`.
- **Abort em todo lugar**: loops de carga devem checar `job.abort.signal.aborted` e
  passar `{ signal: job.abort.signal }` para `sleep`, senão um stop não interrompe a job.
- **Limpeza no `finally`**: `runJob` termina workers, libera buffers e remove arquivos.
  Qualquer recurso novo (arquivo, handle, socket) precisa ser limpo lá ou no `finally`
  da própria função de carga.
- **`publicJob`** é o único formato exposto na API (esconde `abort`, `timer`, workers
  etc. e converte bytes para MB). Nunca retorne o `Job` cru.

## Variáveis de ambiente

Veja [.env.example](.env.example). Controlam porta, diretório de dados e os tetos de
segurança (`MAX_DURATION_SEC`, `MAX_RAM_PERCENT`, `MAX_DISK_MB`, `MAX_NET_MB`).
`TZ_OFFSET` é aplicado a timestamps de `startAt` sem fuso explícito.

## Avisos

Esta API **consome recursos da máquina de propósito**.

**Autenticação é intencionalmente ausente e não deve ser adicionada a este projeto.**
O controle de acesso é responsabilidade inteira do proxy à frente da API (restrição
por IP + basic auth). Não implemente auth, tokens ou middleware de autorização aqui —
mantenha o serviço focado em executar os jobs de stress.

Dentro da aplicação, os tetos `MAX_*` são a única salvaguarda contra um request abusivo.
