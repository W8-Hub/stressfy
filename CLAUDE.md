# CLAUDE.md

Guia para o Claude Code (e qualquer agente) trabalhar neste repositório.

## O que é

`stressfy` é uma API HTTP (Go + [chi](https://github.com/go-chi/chi)) para **testes de
estresse da máquina**. Ela expõe endpoints que criam *jobs* de carga controlada sobre
CPU, RAM, disco e rede. Cada job roda por uma duração definida, pode ser agendado para
o futuro e pode ser cancelado a qualquer momento. Limites de segurança são aplicados
via variáveis de ambiente.

> Histórico: o projeto começou como uma API Fastify/TypeScript de arquivo único e foi
> portado 100% para Go, mantendo os mesmos endpoints e o mesmo contrato de
> request/response (incluindo aliases de campos e merge query+body).

## Comandos

```bash
go run ./cmd/stressfy     # roda localmente (porta 3333 por padrão)
go build -o stressfy ./cmd/stressfy   # compila o binário
go vet ./...              # análise estática
gofmt -l .               # lista arquivos fora do padrão (use -w para formatar)
```

Docker (binário estático em imagem distroless):

```bash
docker build -t stressfy .
docker run -p 3333:3333 stressfy
```

Não há suíte de testes configurada. Ao validar mudanças, suba o servidor
(`go run ./cmd/stressfy`) e exercite os endpoints com `curl`.

## Arquitetura

Módulo Go `stressfy`. Entrada em [cmd/stressfy/main.go](cmd/stressfy/main.go); todo o
resto vive em `internal/`:

- **[internal/config](internal/config/config.go)** — carrega env vars e os tetos
  `MAX_*`; expõe `ClampNumber` (coerção tolerante number/string → float clampado) e
  `ParseStartAt` (parse de timestamp aplicando `TZ_OFFSET`).
- **[internal/job](internal/job/)** — o domínio:
  - `job.go` — `Job` (estado em memória), `JobStatus`, `Metrics`, os tipos de request
    (`StressRequest`, `DiskSpec`, `NetworkSpec`) e o tipo `Number` (numérico tolerante).
  - `store.go` — `Store`: `map[string]*Job` protegido por `sync.RWMutex`. Toda mutação
    de status passa por métodos do store (`Create`, `Stop`, `MarkRunning`, `Finish`).
  - `dto.go` — `PublicJob` / `Store.Public()`: a única forma exposta na API (bytes→MB,
    esconde internals).
- **[internal/stress](internal/stress/)** — a lógica de carga:
  - `stress.go` — `RunJob` (orquestra todas as cargas via `WaitGroup`, finaliza status,
    limpa arquivos) + helpers `sleepCtx` e `throttle`.
  - `cpu.go` — `RunCPU`: uma goroutine por núcleo com janelas busy/idle de 100ms.
  - `ram.go` — `RunRAM`: aloca `[][]byte` tocando páginas, lê limite via cgroups
    (v2/v1) com fallback para `/proc/meminfo`.
  - `disk.go` — `RunDiskWrite`/`RunDiskRead` (+ seed file) com throttle por `mbps`.
  - `net.go` — `RunNetworkWrite` (POST streaming via `io.Pipe`) e `RunNetworkRead`
    (GET streaming), com throttle.
- **[internal/mock](internal/mock/controller.go)** — `Controller` thread-safe para os
  endpoints de mock/caos: guarda o status-code atual (default 200), agenda troca via
  `time.AfterFunc` (mesmo padrão do store) e auto-reversão para 200. Expõe `Current`,
  `State`, `Schedule(code, startAt, revertAfter)` e `Reset`.
- **[internal/api](internal/api/)** — a camada HTTP:
  - `server.go` — `Server` (cfg + store + runner + `mock.Controller`) e `writeJSON`.
  - `request.go` — `parseRequest`: merge query+body (body vence) → `StressRequest`.
  - `handlers.go` — handlers de `/jobs*`, `/health`, `/healthz`, `/ready`.
  - `nethelpers.go` — `/net/source` e `/net/sink`.
  - `mock_handlers.go` — `/mock/status` (GET/POST), `/mock/error`, `/mock/latency`.
  - `router.go` — monta o `chi.Router` com todas as rotas.

### Endpoints

| Método | Rota | Função |
| --- | --- | --- |
| `GET` | `/health` | Status detalhado: memória, contagem de jobs por status |
| `GET` | `/healthz` | Liveness mínimo (`{ ok: true }`) |
| `GET` | `/ready` | Readiness |
| `POST` | `/jobs` | Cria um job de stress (corpo JSON, veja README) |
| `GET` | `/jobs` | Lista todos os jobs |
| `GET` | `/jobs/{id}` | Detalhe de um job |
| `POST` | `/jobs/{id}/stop` | Cancela/para um job |
| `GET` | `/net/source?mb=&chunkMb=` | Gera tráfego de download (para `networkRead`) |
| `POST` | `/net/sink` | Absorve tráfego de upload (para `networkWrite`) |
| `GET` | `/mock/status` | Responde com o status-code mockado (default 200) + body JSON |
| `POST` | `/mock/status` | Agenda troca do status-code (`statusCode`, `startAt?`, `durationSec?`) |
| `GET` | `/mock/error` | Sempre um 5xx aleatório (`math/rand` entre 500/502/503/504) |
| `GET` | `/mock/latency?ms=` | 200 após `ms` de atraso (limitado por `MAX_LATENCY_MS`) |

### Convenções importantes

- **Tipo `Number`**: campos numéricos do request são `*job.Number` (não `*float64`).
  Ele faz `UnmarshalJSON` tolerante (aceita número OU string numérica), porque query
  params chegam como string e o body como número. Ao ler, use `n.Val()` que retorna
  `(float64, ok)`. Ponteiro `nil` = ausente.
- **Aliases de campos**: `StressRequest.Normalize()` preenche o nome longo a partir do
  curto — `start`→`startAt`, `time`→`durationSec`, `cpu`→`cpuPercent`, `ram`→`ramPercent`.
  Ao editar o schema, mantenha os dois nomes.
- **`POST /jobs` mescla query + body**: `parseRequest` junta os dois com o body tendo
  precedência (espelha `{...query, ...body}` do original).
- **Tudo é "clampado"**: use `config.ClampNumber(value, min, max, fallback)` para
  qualquer entrada numérica nova. Os tetos vêm das env `MAX_*`.
- **Cancelamento via `context`**: cada `Job` tem `Ctx`/`Cancel`. Loops de carga checam
  `ctx.Err()` e usam `sleepCtx`/`throttle` (que respeitam o context). Sem isso, um stop
  não interrompe a job.
- **Cada carga se auto-limita à duração** (`time.Now().Before(end)`); não há timer
  global que cancele em `duration` — `Cancel()` é só para stop manual.
- **Limpeza**: funções de disco removem seus arquivos no `defer`; `RunJob` também
  remove `job.Files()` ao final. Registre arquivos novos com `job.AddFile`.
- **`Store.Public`** é o único formato exposto (esconde ctx, timer, goroutines; bytes→MB
  com 2 casas). Nunca serialize o `Job` cru.

### Diferenças intencionais em relação ao original TS

- **Job só-CPU realmente estressa pela duração**: no TS o `runCpu` não era aguardado e
  jobs só-CPU terminavam quase instantaneamente. Aqui `RunCPU` roda pela duração.
- **Status final após stop**: ao parar um job em execução, ele vai para `stopping` e,
  quando as goroutines encerram, para `cancelled` (no TS ficava preso em `stopping`).

## Variáveis de ambiente

Veja [.env.example](.env.example). Controlam porta, diretório de dados e os tetos de
segurança (`MAX_DURATION_SEC`, `MAX_RAM_PERCENT`, `MAX_DISK_MB`, `MAX_NET_MB`,
`MAX_LATENCY_MS`). `TZ_OFFSET` é aplicado a timestamps de `startAt` sem fuso explícito
(usado tanto pelos jobs quanto pelo agendamento de `/mock/status`).

> Nota: `RunRAM` (limite via cgroups + `/proc`) e a métrica `rssMb` do `/health`
> dependem do Linux. Em macOS (dev) a alocação por `ramMb` funciona, mas `ramPercent`
> cai no fallback e `rssMb` reporta 0.

## Avisos

Esta API **consome recursos da máquina de propósito**.

**Autenticação é intencionalmente ausente e não deve ser adicionada a este projeto.**
O controle de acesso é responsabilidade inteira do proxy à frente da API (restrição
por IP + basic auth). Não implemente auth, tokens ou middleware de autorização aqui —
mantenha o serviço focado em executar os jobs de stress.

Dentro da aplicação, os tetos `MAX_*` são a única salvaguarda contra um request abusivo.
