import Fastify from 'fastify';
import os from 'node:os';
import path from 'node:path';
import fs from 'node:fs/promises';
import http from 'node:http';
import https from 'node:https';
import { Worker } from 'node:worker_threads';
import { randomUUID } from 'node:crypto';
import { Readable } from 'node:stream';
import { setTimeout as sleep } from 'node:timers/promises';

const app = Fastify({ logger: true });

const PORT = Number(process.env.PORT ?? 3333);
const DATA_DIR = process.env.DATA_DIR ?? '/tmp/stress-api';
const TZ_OFFSET = process.env.TZ_OFFSET ?? '-03:00';

const MAX_DURATION_SEC = Number(process.env.MAX_DURATION_SEC ?? 900);
const MAX_RAM_PERCENT = Number(process.env.MAX_RAM_PERCENT ?? 85);
const MAX_DISK_MB = Number(process.env.MAX_DISK_MB ?? 10240);
const MAX_NET_MB = Number(process.env.MAX_NET_MB ?? 10240);

const MB = 1024 * 1024;

type JobStatus =
  | 'scheduled'
  | 'running'
  | 'stopping'
  | 'finished'
  | 'failed'
  | 'cancelled';

type DiskSpec = {
  mb?: number;
  mbps?: number;
  path?: string;
  keepFile?: boolean;
  fsync?: boolean;
};

type NetworkSpec = {
  url: string;
  mb?: number;
  mbps?: number;
};

type StressRequest = {
  startAt?: string;
  start?: string;

  durationSec?: number;
  time?: number;

  cpuPercent?: number;
  cpu?: number;

  ramPercent?: number;
  ram?: number;
  ramMb?: number;

  diskWrite?: DiskSpec;
  diskRead?: DiskSpec;

  networkWrite?: NetworkSpec;
  networkRead?: NetworkSpec;
};

type Job = {
  id: string;
  status: JobStatus;
  createdAt: string;
  scheduledFor: string;
  startedAt?: string;
  finishedAt?: string;
  error?: string;
  request: StressRequest;
  durationMs: number;
  abort: AbortController;
  timer?: NodeJS.Timeout;
  workers: Worker[];
  buffers: Buffer[];
  files: string[];
  metrics: {
    diskWrittenBytes: number;
    diskReadBytes: number;
    networkWrittenBytes: number;
    networkReadBytes: number;
  };
};

const jobs = new Map<string, Job>();

const CPU_WORKER_CODE = `
const { parentPort, workerData } = require('node:worker_threads');

let running = true;

parentPort.on('message', (msg) => {
  if (msg === 'stop') running = false;
});

function busy(ms) {
  const end = Date.now() + ms;
  let x = 0;

  while (running && Date.now() < end) {
    x += Math.sqrt(Math.random() * Date.now());
  }

  return x;
}

async function main() {
  const percent = Math.max(0, Math.min(100, Number(workerData.percent || 0)));
  const durationMs = Number(workerData.durationMs || 0);
  const endAt = Date.now() + durationMs;

  const windowMs = 100;
  const busyMs = Math.floor(windowMs * (percent / 100));
  const idleMs = windowMs - busyMs;

  while (running && Date.now() < endAt) {
    if (busyMs > 0) busy(busyMs);
    if (idleMs > 0) {
      await new Promise((resolve) => setTimeout(resolve, idleMs));
    }
  }
}

main().catch((err) => {
  parentPort.postMessage({ error: err?.message || String(err) });
});
`;

function clampNumber(value: unknown, min: number, max: number, fallback = 0): number {
  const n = Number(value);
  if (!Number.isFinite(n)) return fallback;
  return Math.max(min, Math.min(max, n));
}

function normalizeRequest(input: any): StressRequest {
  return {
    ...input,
    startAt: input.startAt ?? input.start,
    durationSec: input.durationSec ?? input.time,
    cpuPercent: input.cpuPercent ?? input.cpu,
    ramPercent: input.ramPercent ?? input.ram
  };
}

function parseStartAt(input?: string): number {
  if (!input) return Date.now();

  let value = String(input).trim();

  const legacy = value.match(/^(\d{4}-\d{2}-\d{2})[:\s](\d{2}:\d{2}:\d{2})$/);

  if (legacy) {
    value = `${legacy[1]}T${legacy[2]}${TZ_OFFSET}`;
  } else if (
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}$/.test(value)
  ) {
    value = `${value}${TZ_OFFSET}`;
  }

  const timestamp = Date.parse(value);

  if (Number.isNaN(timestamp)) {
    throw new Error(`Invalid startAt/start format: ${input}`);
  }

  return timestamp;
}

async function readNumberFile(file: string): Promise<number | null> {
  try {
    const raw = (await fs.readFile(file, 'utf8')).trim();
    if (raw === 'max') return null;
    const n = Number(raw);
    return Number.isFinite(n) ? n : null;
  } catch {
    return null;
  }
}

async function getMemoryLimitBytes(): Promise<number> {
  const cgroupV2 = await readNumberFile('/sys/fs/cgroup/memory.max');
  if (cgroupV2 && cgroupV2 > 0) return cgroupV2;

  const cgroupV1 = await readNumberFile('/sys/fs/cgroup/memory/memory.limit_in_bytes');
  if (cgroupV1 && cgroupV1 > 0 && cgroupV1 < Number.MAX_SAFE_INTEGER) {
    return cgroupV1;
  }

  return os.totalmem();
}

async function waitJobDuration(job: Job): Promise<void> {
  try {
    await sleep(job.durationMs, undefined, { signal: job.abort.signal });
  } catch {
    // aborted
  }
}

function throwIfAborted(job: Job) {
  if (job.abort.signal.aborted) {
    throw new Error('aborted');
  }
}

function runCpu(job: Job, cpuPercent: number) {
  const parallelism = os.availableParallelism?.() ?? os.cpus().length;
  const workersCount = Math.max(1, parallelism);

  for (let i = 0; i < workersCount; i++) {
    const worker = new Worker(CPU_WORKER_CODE, {
      eval: true,
      workerData: {
        percent: cpuPercent,
        durationMs: job.durationMs
      }
    });

    job.workers.push(worker);
  }

  job.abort.signal.addEventListener('abort', () => {
    for (const worker of job.workers) {
      worker.postMessage('stop');
      worker.terminate().catch(() => undefined);
    }
  });
}

async function runRam(job: Job, req: StressRequest) {
  const ramMb = req.ramMb;
  const ramPercent = req.ramPercent;

  if (!ramMb && !ramPercent) return;

  const limitBytes = await getMemoryLimitBytes();

  let targetBytes: number;

  if (ramMb) {
    targetBytes = ramMb * MB;
  } else {
    const percent = clampNumber(ramPercent, 1, MAX_RAM_PERCENT);
    targetBytes = Math.floor(limitBytes * (percent / 100));
  }

  const currentRss = process.memoryUsage().rss;
  const bytesToAllocate = Math.max(0, targetBytes - currentRss);

  const chunkSize = 16 * MB;
  let allocated = 0;

  while (allocated < bytesToAllocate) {
    throwIfAborted(job);

    const size = Math.min(chunkSize, bytesToAllocate - allocated);
    const buffer = Buffer.alloc(size, 0x7f);

    // Toca as páginas para forçar alocação real.
    for (let i = 0; i < buffer.length; i += 4096) {
      buffer[i] = 1;
    }

    job.buffers.push(buffer);
    allocated += size;

    await sleep(10);
  }

  await waitJobDuration(job);

  job.buffers = [];
}

async function createSeedFile(file: string, sizeMb: number) {
  await fs.mkdir(path.dirname(file), { recursive: true });

  const handle = await fs.open(file, 'w');
  const chunk = Buffer.alloc(MB, 0x61);

  try {
    for (let i = 0; i < sizeMb; i++) {
      await handle.write(chunk);
    }
  } finally {
    await handle.close();
  }
}

async function runDiskWrite(job: Job, spec?: DiskSpec) {
  if (!spec) return;

  const dir = spec.path || DATA_DIR;
  await fs.mkdir(dir, { recursive: true });

  const targetMb = clampNumber(spec.mb ?? MAX_DISK_MB, 1, MAX_DISK_MB, 512);
  const mbps = spec.mbps ? clampNumber(spec.mbps, 1, 100000, 0) : 0;

  const file = path.join(dir, `${job.id}-write.dat`);
  job.files.push(file);

  const handle = await fs.open(file, 'w');
  const chunk = Buffer.alloc(MB, 0x77);

  const start = Date.now();
  const end = start + job.durationMs;
  const targetBytes = targetMb * MB;

  let written = 0;

  try {
    while (!job.abort.signal.aborted && Date.now() < end && written < targetBytes) {
      const remaining = targetBytes - written;
      const size = Math.min(chunk.length, remaining);

      await handle.write(chunk, 0, size);
      written += size;
      job.metrics.diskWrittenBytes += size;

      if (spec.fsync) {
        await handle.sync();
      }

      if (mbps > 0) {
        const expectedElapsedMs = (written / (mbps * MB)) * 1000;
        const actualElapsedMs = Date.now() - start;

        if (expectedElapsedMs > actualElapsedMs) {
          await sleep(expectedElapsedMs - actualElapsedMs, undefined, {
            signal: job.abort.signal
          }).catch(() => undefined);
        }
      }
    }
  } finally {
    await handle.close();

    if (!spec.keepFile) {
      await fs.rm(file, { force: true }).catch(() => undefined);
    }
  }
}

async function runDiskRead(job: Job, spec?: DiskSpec) {
  if (!spec) return;

  const dir = spec.path || DATA_DIR;
  await fs.mkdir(dir, { recursive: true });

  const targetMb = clampNumber(spec.mb ?? 512, 1, MAX_DISK_MB, 512);
  const mbps = spec.mbps ? clampNumber(spec.mbps, 1, 100000, 0) : 0;

  const file = path.join(dir, `${job.id}-read-seed.dat`);
  job.files.push(file);

  await createSeedFile(file, targetMb);

  const handle = await fs.open(file, 'r');
  const chunk = Buffer.alloc(MB);

  const start = Date.now();
  const end = start + job.durationMs;

  let readTotal = 0;
  let position = 0;

  try {
    while (!job.abort.signal.aborted && Date.now() < end) {
      const result = await handle.read(chunk, 0, chunk.length, position);

      if (result.bytesRead === 0) {
        position = 0;
        continue;
      }

      position += result.bytesRead;
      readTotal += result.bytesRead;
      job.metrics.diskReadBytes += result.bytesRead;

      if (mbps > 0) {
        const expectedElapsedMs = (readTotal / (mbps * MB)) * 1000;
        const actualElapsedMs = Date.now() - start;

        if (expectedElapsedMs > actualElapsedMs) {
          await sleep(expectedElapsedMs - actualElapsedMs, undefined, {
            signal: job.abort.signal
          }).catch(() => undefined);
        }
      }
    }
  } finally {
    await handle.close();
    await fs.rm(file, { force: true }).catch(() => undefined);
  }
}

function getHttpClient(url: URL) {
  return url.protocol === 'https:' ? https : http;
}

async function networkUploadOnce(
  rawUrl: string,
  totalMb: number,
  mbps: number | undefined,
  job: Job
): Promise<number> {
  const url = new URL(rawUrl);
  const client = getHttpClient(url);

  const totalBytes = totalMb * MB;
  const chunk = Buffer.alloc(MB, 0x6e);

  let sent = 0;
  const start = Date.now();

  return new Promise((resolve, reject) => {
    const req = client.request(
      url,
      {
        method: 'POST',
        headers: {
          'content-type': 'application/octet-stream'
        }
      },
      (res) => {
        res.resume();
        res.on('end', () => resolve(sent));
      }
    );

    req.on('error', reject);

    job.abort.signal.addEventListener('abort', () => {
      req.destroy(new Error('aborted'));
    });

    async function writeLoop() {
      try {
        while (!job.abort.signal.aborted && sent < totalBytes) {
          const remaining = totalBytes - sent;
          const size = Math.min(chunk.length, remaining);

          const canContinue = req.write(chunk.subarray(0, size));
          sent += size;
          job.metrics.networkWrittenBytes += size;

          if (!canContinue) {
            await new Promise<void>((resolveDrain) => req.once('drain', resolveDrain));
          }

          if (mbps && mbps > 0) {
            const expectedElapsedMs = (sent / (mbps * MB)) * 1000;
            const actualElapsedMs = Date.now() - start;

            if (expectedElapsedMs > actualElapsedMs) {
              await sleep(expectedElapsedMs - actualElapsedMs, undefined, {
                signal: job.abort.signal
              }).catch(() => undefined);
            }
          }
        }

        req.end();
      } catch (err) {
        req.destroy(err as Error);
      }
    }

    writeLoop();
  });
}

async function networkDownloadOnce(
  rawUrl: string,
  mbps: number | undefined,
  job: Job
): Promise<number> {
  const url = new URL(rawUrl);
  const client = getHttpClient(url);

  let received = 0;
  const start = Date.now();

  return new Promise((resolve, reject) => {
    const req = client.get(url, async (res) => {
      try {
        for await (const chunk of res) {
          if (job.abort.signal.aborted) break;

          const size = Buffer.byteLength(chunk);
          received += size;
          job.metrics.networkReadBytes += size;

          if (mbps && mbps > 0) {
            const expectedElapsedMs = (received / (mbps * MB)) * 1000;
            const actualElapsedMs = Date.now() - start;

            if (expectedElapsedMs > actualElapsedMs) {
              await sleep(expectedElapsedMs - actualElapsedMs, undefined, {
                signal: job.abort.signal
              }).catch(() => undefined);
            }
          }
        }

        resolve(received);
      } catch (err) {
        reject(err);
      }
    });

    req.on('error', reject);

    job.abort.signal.addEventListener('abort', () => {
      req.destroy(new Error('aborted'));
    });
  });
}

async function runNetworkWrite(job: Job, spec?: NetworkSpec) {
  if (!spec?.url) return;

  const mb = clampNumber(spec.mb ?? MAX_NET_MB, 1, MAX_NET_MB, 512);
  const mbps = spec.mbps ? clampNumber(spec.mbps, 1, 100000, 0) : undefined;

  const end = Date.now() + job.durationMs;

  while (!job.abort.signal.aborted && Date.now() < end) {
    await networkUploadOnce(spec.url, mb, mbps, job).catch((err) => {
      if (!job.abort.signal.aborted) throw err;
    });
  }
}

async function runNetworkRead(job: Job, spec?: NetworkSpec) {
  if (!spec?.url) return;

  const mbps = spec.mbps ? clampNumber(spec.mbps, 1, 100000, 0) : undefined;
  const end = Date.now() + job.durationMs;

  while (!job.abort.signal.aborted && Date.now() < end) {
    await networkDownloadOnce(spec.url, mbps, job).catch((err) => {
      if (!job.abort.signal.aborted) throw err;
    });
  }
}

async function runJob(job: Job) {
  job.status = 'running';
  job.startedAt = new Date().toISOString();

  try {
    const req = job.request;

    const cpuPercent = clampNumber(req.cpuPercent, 0, 100, 0);

    if (cpuPercent > 0) {
      runCpu(job, cpuPercent);
    }

    const tasks = [
      runRam(job, req),
      runDiskWrite(job, req.diskWrite),
      runDiskRead(job, req.diskRead),
      runNetworkWrite(job, req.networkWrite),
      runNetworkRead(job, req.networkRead)
    ];

    await Promise.allSettled(tasks);

    if (job.abort.signal.aborted) {
      job.status = 'cancelled';
    } else {
      job.status = 'finished';
    }
  } catch (err: any) {
    job.status = job.abort.signal.aborted ? 'cancelled' : 'failed';
    job.error = err?.message || String(err);
  } finally {
    job.finishedAt = new Date().toISOString();

    for (const worker of job.workers) {
      worker.postMessage('stop');
      worker.terminate().catch(() => undefined);
    }

    job.workers = [];
    job.buffers = [];

    for (const file of job.files) {
      await fs.rm(file, { force: true }).catch(() => undefined);
    }
  }
}

function createJob(request: StressRequest): Job {
  const normalized = normalizeRequest(request);

  const durationSec = clampNumber(
    normalized.durationSec,
    1,
    MAX_DURATION_SEC,
    10
  );

  const startTs = parseStartAt(normalized.startAt);

  const job: Job = {
    id: randomUUID(),
    status: startTs > Date.now() ? 'scheduled' : 'running',
    createdAt: new Date().toISOString(),
    scheduledFor: new Date(startTs).toISOString(),
    request: normalized,
    durationMs: durationSec * 1000,
    abort: new AbortController(),
    workers: [],
    buffers: [],
    files: [],
    metrics: {
      diskWrittenBytes: 0,
      diskReadBytes: 0,
      networkWrittenBytes: 0,
      networkReadBytes: 0
    }
  };

  jobs.set(job.id, job);

  const delay = Math.max(0, startTs - Date.now());

  job.timer = setTimeout(() => {
    runJob(job);
  }, delay);

  return job;
}

function publicJob(job: Job) {
  return {
    id: job.id,
    status: job.status,
    createdAt: job.createdAt,
    scheduledFor: job.scheduledFor,
    startedAt: job.startedAt,
    finishedAt: job.finishedAt,
    error: job.error,
    request: job.request,
    metrics: {
      diskWrittenMb: Number((job.metrics.diskWrittenBytes / MB).toFixed(2)),
      diskReadMb: Number((job.metrics.diskReadBytes / MB).toFixed(2)),
      networkWrittenMb: Number((job.metrics.networkWrittenBytes / MB).toFixed(2)),
      networkReadMb: Number((job.metrics.networkReadBytes / MB).toFixed(2))
    }
  };
}

app.get('/health', async () => {
  return {
    ok: true,
    status: 'healthy',
    service: 'stress-api',
    hostname: os.hostname(),
    uptimeSec: Math.floor(process.uptime()),
    now: new Date().toISOString(),
    memory: {
      rssMb: Number((process.memoryUsage().rss / MB).toFixed(2)),
      heapUsedMb: Number((process.memoryUsage().heapUsed / MB).toFixed(2)),
      heapTotalMb: Number((process.memoryUsage().heapTotal / MB).toFixed(2))
    },
    jobs: {
      total: jobs.size,
      scheduled: Array.from(jobs.values()).filter((job) => job.status === 'scheduled').length,
      running: Array.from(jobs.values()).filter((job) => job.status === 'running').length,
      stopping: Array.from(jobs.values()).filter((job) => job.status === 'stopping').length,
      finished: Array.from(jobs.values()).filter((job) => job.status === 'finished').length,
      failed: Array.from(jobs.values()).filter((job) => job.status === 'failed').length,
      cancelled: Array.from(jobs.values()).filter((job) => job.status === 'cancelled').length
    }
  };
});

app.get('/healthz', async () => {
  return {
    ok: true
  };
});

app.get('/ready', async () => {
  return {
    ok: true,
    ready: true,
    hostname: os.hostname(),
    uptimeSec: Math.floor(process.uptime())
  };
});

app.post('/jobs', async (request, reply) => {
  const body = request.body && typeof request.body === 'object' ? request.body : {};
  const query = request.query && typeof request.query === 'object' ? request.query : {};

  const job = createJob({
    ...(query as any),
    ...(body as any)
  });

  return reply.code(201).send(publicJob(job));
});

app.get('/jobs', async () => {
  return Array.from(jobs.values()).map(publicJob);
});

app.get('/jobs/:id', async (request: any, reply) => {
  const job = jobs.get(request.params.id);

  if (!job) {
    return reply.code(404).send({ error: 'job_not_found' });
  }

  return publicJob(job);
});

app.post('/jobs/:id/stop', async (request: any, reply) => {
  const job = jobs.get(request.params.id);

  if (!job) {
    return reply.code(404).send({ error: 'job_not_found' });
  }

  if (
    job.status === 'finished' ||
    job.status === 'failed' ||
    job.status === 'cancelled'
  ) {
    return publicJob(job);
  }

  if (job.timer) {
    clearTimeout(job.timer);
    job.timer = undefined;
  }

  job.abort.abort();

  for (const worker of job.workers) {
    worker.postMessage('stop');
    worker.terminate().catch(() => undefined);
  }

  if (job.status === 'scheduled') {
    job.status = 'cancelled';
    job.finishedAt = new Date().toISOString();
  } else {
    job.status = 'stopping';
  }

  return publicJob(job);
});

app.get('/net/source', async (request: any, reply) => {
  const mb = clampNumber(request.query?.mb, 1, MAX_NET_MB, 100);
  const chunkMb = clampNumber(request.query?.chunkMb, 1, 16, 1);

  const totalBytes = mb * MB;
  const chunkBytes = chunkMb * MB;

  let sent = 0;

  const stream = Readable.from((async function* () {
    while (sent < totalBytes) {
      const remaining = totalBytes - sent;
      const size = Math.min(chunkBytes, remaining);
      sent += size;
      yield Buffer.alloc(size, 0x73);
    }
  })());

  reply.header('content-type', 'application/octet-stream');
  reply.header('content-length', String(totalBytes));

  return reply.send(stream);
});

app.post('/net/sink', async (request) => {
  let bytes = 0;

  for await (const chunk of request.raw) {
    bytes += Buffer.byteLength(chunk);
  }

  return {
    ok: true,
    receivedBytes: bytes,
    receivedMb: Number((bytes / MB).toFixed(2))
  };
});

async function bootstrap() {
  await fs.mkdir(DATA_DIR, { recursive: true });

  await app.listen({
    host: '0.0.0.0',
    port: PORT
  });
}

bootstrap().catch((err) => {
  app.log.error(err);
  process.exit(1);
});
