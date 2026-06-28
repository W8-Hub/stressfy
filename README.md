# stressfy

> API HTTP rápida e simples para criar **testes de estresse** (CPU, RAM, disco e rede)
> em uma máquina, sob demanda.

`stressfy` expõe uma API onde você dispara *jobs* de carga controlada. Cada job aplica
estresse sobre um ou mais recursos da máquina por uma duração definida, pode ser
**agendado** para começar no futuro e pode ser **cancelado** a qualquer instante.
Ideal para validar limites, autoscaling, alertas de monitoramento e comportamento de
infraestrutura sob pressão.

Construída em [Go](https://go.dev/) + [chi](https://github.com/go-chi/chi), sem banco de
dados — o estado dos jobs vive em memória. Compila para um binário estático único.

## Recursos

- **CPU** — ocupa todos os núcleos até um percentual alvo (`cpuPercent`).
- **RAM** — aloca memória residente real até `ramMb` ou `ramPercent` (respeita cgroups).
- **Disco** — escrita e/ou leitura contínua, com throttling opcional (`mbps`).
- **Rede** — upload e/ou download em streaming, com endpoints auxiliares embutidos.
- **Agendamento** — `startAt` para iniciar o job em um horário futuro.
- **Limites de segurança** — tetos configuráveis por variáveis de ambiente.
- **Métricas** — bytes de disco/rede acumulados por job, expostos na consulta.

## Início rápido

Requer **Go 1.23+** (apenas para rodar a partir do fonte; o Docker não precisa de Go local).

```bash
go run ./cmd/stressfy        # sobe em http://localhost:3333
```

Configure a porta ou os tetos via variáveis de ambiente (veja [Configuração](#configuração)):

```bash
PORT=8080 MAX_DURATION_SEC=300 go run ./cmd/stressfy
```

Ou compile o binário:

```bash
go build -o stressfy ./cmd/stressfy
./stressfy
```

Ou via Docker:

```bash
docker build -t stressfy .
docker run -p 3333:3333 stressfy
```

Verifique se está no ar:

```bash
curl http://localhost:3333/health
```

## Desenvolvimento

```bash
go run ./cmd/stressfy                  # roda a API localmente
go build -o stressfy ./cmd/stressfy    # compila o binário
go vet ./...                           # análise estática
gofmt -l .                             # lista arquivos fora do padrão (use -w para corrigir)
```

### Testes

```bash
go test ./...            # roda toda a suíte
go test -race ./...      # com detector de data races (recomendado)
go test -cover ./...     # com relatório de cobertura por pacote
go test ./internal/api   # apenas um pacote
```

Os testes não dependem de rede externa nem de portas fixas (usam `httptest`) e as cargas
de stress rodam com durações curtas, então a suíte completa leva ~2s.

## Uso

### Criar um job

`POST /jobs` com um corpo JSON. Todos os campos de carga são opcionais — combine os
que quiser. O job só estressa os recursos que você especificar.

```jsonc
{
  "startAt": "2026-06-27T22:00:00",   // opcional; padrão = agora. Sem fuso usa TZ_OFFSET
  "durationSec": 60,                   // duração total do job (clampada por MAX_DURATION_SEC)

  "cpuPercent": 80,                    // carga de CPU alvo (0–100, em todos os núcleos)

  "ramPercent": 50,                    // OU "ramMb": 1024 — memória a ocupar
  "ramMb": 1024,

  "diskWrite": { "mb": 2048, "mbps": 200, "fsync": true },
  "diskRead":  { "mb": 1024, "mbps": 200 },

  "networkWrite": { "url": "http://localhost:3333/net/sink", "mb": 1024, "mbps": 100 },
  "networkRead":  { "url": "http://localhost:3333/net/source?mb=1024", "mbps": 100 }
}
```

**Aliases aceitos** (formas curtas): `start`=`startAt`, `time`=`durationSec`,
`cpu`=`cpuPercent`, `ram`=`ramPercent`.

A resposta (`201`) traz o job criado, incluindo `id` e `status`:

```json
{
  "id": "f1e2d3c4-...",
  "status": "running",
  "scheduledFor": "2026-06-27T22:00:00.000Z",
  "request": { "...": "..." },
  "metrics": { "diskWrittenMb": 0, "diskReadMb": 0, "networkWrittenMb": 0, "networkReadMb": 0 }
}
```

### Consultar e parar

```bash
curl http://localhost:3333/jobs            # lista todos
curl http://localhost:3333/jobs/<id>       # detalhe de um job (com métricas)
curl -X POST http://localhost:3333/jobs/<id>/stop   # cancela/para
```

### Endpoints

| Método | Rota | Descrição |
| --- | --- | --- |
| `GET` | `/health` | Status detalhado (memória, jobs por status) |
| `GET` | `/healthz` | Liveness mínimo |
| `GET` | `/ready` | Readiness |
| `POST` | `/jobs` | Cria um job de stress |
| `GET` | `/jobs` | Lista todos os jobs |
| `GET` | `/jobs/:id` | Detalhe de um job |
| `POST` | `/jobs/:id/stop` | Cancela/para um job |
| `GET` | `/net/source?mb=&chunkMb=` | Gera tráfego (contraparte de `networkRead`) |
| `POST` | `/net/sink` | Absorve tráfego (contraparte de `networkWrite`) |

## Casos de uso

- **Validar autoscaling** — agende um job de CPU a 90% por 10 minutos e observe se o
  cluster escala horizontalmente como esperado.
- **Testar alertas de monitoramento** — gere uso alto de RAM/disco para confirmar que
  Prometheus/Grafana/Datadog disparam os alertas certos nos limiares certos.
- **Capacity planning** — meça a partir de qual carga a latência da aplicação degrada,
  combinando stress de CPU + disco.
- **Saturação de I/O de disco** — `diskWrite`/`diskRead` com `mbps` controlado para
  reproduzir contenção de disco e avaliar o impacto em serviços vizinhos.
- **Testes de rede/banda** — sature upload/download usando `/net/source` e `/net/sink`
  como par local, sem depender de serviços externos.
- **Caos agendado / fire drills** — use `startAt` para programar picos de carga em
  horários específicos e ensaiar a resposta da equipe e da infraestrutura.
- **Verificar limites de container** — confirme que limites de CPU/memória (cgroups)
  estão sendo respeitados e que o orquestrador mata/reinicia como configurado.

## Configuração

Variáveis de ambiente (veja [.env.example](.env.example)):

| Variável | Padrão | Descrição |
| --- | --- | --- |
| `PORT` | `3333` | Porta HTTP |
| `DATA_DIR` | `/tmp/stress-api` | Diretório para arquivos de stress de disco |
| `TZ_OFFSET` | `-03:00` | Fuso aplicado a `startAt` sem timezone explícito |
| `MAX_DURATION_SEC` | `900` | Duração máxima de um job (s) |
| `MAX_RAM_PERCENT` | `85` | Teto de RAM por job (%) |
| `MAX_DISK_MB` | `10240` | Teto de disco por job (MB) |
| `MAX_NET_MB` | `10240` | Teto de rede por job (MB) |

## ⚠️ Aviso de segurança

Esta API **consome recursos da máquina de propósito** e **não possui autenticação**.

- Nunca a exponha na internet pública sem uma camada de proteção (rede privada,
  autenticação, firewall, reverse proxy).
- Os tetos `MAX_*` são a única salvaguarda contra requests abusivos — ajuste-os à
  capacidade real da máquina.
- Use apenas em ambientes que você controla e tem autorização para estressar.

## Licença

Uso interno — W8 Soluções.
