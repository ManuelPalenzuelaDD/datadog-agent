# Pipeline Recorder — Architecture Specification

## Overview

The pipeline recorder is a two-part system that captures signal data (metrics and
logs) flowing through the Datadog Agent and writes it to Vortex columnar files on
disk for offline analysis.

**Part 1 — Go fx component** (`comp/pipelinesink`): subscribes to the existing
`hook.Hook[MetricView]` / `hook.Hook[LogView]` publish/subscribe channels, batches
payloads in per-type ring buffers, encodes them with Cap'n Proto, and forwards them
over a Unix domain socket to the sidecar.

**Part 2 — Rust sidecar binary** (`tools/pipelinerecorder`): listens on the Unix
socket, decodes Cap'n Proto frames, and writes rows to Vortex columnar files on disk.

The system is designed for Kubernetes deployment where the agent container and the
recorder sidecar share an `emptyDir` volume for both the socket and the output files.

---

## Architecture Diagram

```
┌──────────────────────────────────────────────────────────────────┐
│  Datadog Agent Process                                           │
│                                                                  │
│  hook.Hook[MetricView] ──┐                                       │
│  hook.Hook[LogView]    ──┼──► comp/pipelinesink                  │
│                          │         │                             │
│                          │    ring buffers (per type)            │
│                          │         │                             │
│                          │    flush goroutine (100ms)            │
│                          │         │                             │
│                          │    Cap'n Proto encode                 │
│                          │         │                             │
│                          │    Transport.Send([]byte)             │
│                          │         │                             │
└──────────────────────────┼─────────┼─────────────────────────────┘
                           │         │  Unix socket
           emptyDir volume │         ▼ /var/run/pipelinesink/pipeline.sock
                           │         │
┌──────────────────────────┼─────────┼─────────────────────────────┐
│  Rust sidecar            │         │                             │
│  (tools/pipelinerecorder)│         │                             │
│                          │    Transport::recv() -> Bytes         │
│                          │         │                             │
│                          │    Cap'n Proto decode                 │
│                          │         │                             │
│                          │    dispatch on SignalEnvelope union   │
│                          │       /         \                     │
│                     MetricWriter       LogWriter                 │
│                          │                  │                    │
│                     Vortex flush        Vortex flush             │
│                          │                  │                    │
│                    /data/signals/      /data/signals/            │
│                    metrics-*.vortex    logs-*.vortex             │
└──────────────────────────────────────────────────────────────────┘
```

---

## Transport Iterations

The transport is abstracted behind a thin interface so the switch from Unix sockets
to zero-copy shared memory is mechanical (no logic changes required).

### Iteration 1 (this spec) — Unix Domain Socket

- Socket path: `/var/run/pipelinesink/pipeline.sock` (configurable)
- Shared between agent container and sidecar via Kubernetes `emptyDir` volume
- Agent side: reconnects with exponential backoff (100ms → 30s) if sidecar not ready
- Frame format: Cap'n Proto packed stream (self-delimiting, no separate length prefix)
- If socket not connected when flush fires, the batch is dropped and a drop counter
  is incremented

### Iteration 2 (future) — Iceoryx2 Shared Memory

- Zero-copy: Cap'n Proto bytes written into iceoryx2 shared memory samples
- Same Cap'n Proto schema; only the `Transport` implementation changes
- Go: `iceoryx2Transport` replaces `unixTransport` behind the `Transport` interface
- Rust: `Iceoryx2Transport` replaces `UnixSocketTransport` behind the `Transport` trait
- Source: `/Users/maxime.riaud/Dev/iceoryx2`, branch `feat/go-language-bindings`

---

## Cap'n Proto Schema

Schema location: `schema/pipelinesink/signals.capnp`

Scope for this iteration: **metrics and logs only**. Fields 2–4 of the union are
reserved to allow adding traces, profiles, and trace stats later without renumbering
or breaking wire compatibility.

```capnp
@0xdeadbeefcafe0001;

struct SignalEnvelope {
  union {
    metricBatch @0 :MetricBatch;
    logBatch    @1 :LogBatch;
    # @2 reserved for TraceBatch
    # @3 reserved for ProfileBatch
    # @4 reserved for TraceStatBatch
  }
}

struct MetricBatch {
  samples @0 :List(MetricSample);
}

struct MetricSample {
  name        @0 :Text;
  value       @1 :Float64;
  tags        @2 :List(Text);
  timestampNs @3 :Int64;
  sampleRate  @4 :Float64;
  source      @5 :Text;
}

struct LogBatch {
  entries @0 :List(LogEntry);
}

struct LogEntry {
  content     @0 :Data;
  status      @1 :Text;
  tags        @2 :List(Text);
  hostname    @3 :Text;
  timestampNs @4 :Int64;
  source      @5 :Text;
}
```

---

## Component Interface (`comp/pipelinesink`)

```go
// Package pipelinesink provides the Component interface for the pipeline sink.
package pipelinesink

// Component is the pipeline sink that forwards signal data to the Rust recorder.
type Component interface {
    // Stats returns current operational counters (for health endpoints / debugging).
    Stats() Stats
}

// Stats contains runtime counters for the pipeline sink.
type Stats struct {
    MetricsSent    uint64
    LogsSent       uint64
    MetricsDropped uint64
    LogsDropped    uint64
    BytesSent      uint64
    Reconnects     uint64
}
```

---

## Go Component Internal Design

### Files

| File | Responsibility |
|------|---------------|
| `comp/pipelinesink/def/component.go` | `Component` interface + `Stats` struct |
| `comp/pipelinesink/impl/transport.go` | `Transport` interface + `unixTransport` (reconnect loop) |
| `comp/pipelinesink/impl/encoder.go` | Cap'n Proto encoding helpers per signal type |
| `comp/pipelinesink/impl/batcher.go` | Per-type ring buffer + flush goroutine |
| `comp/pipelinesink/impl/sink.go` | `NewComponent`, hook subscription, lifecycle |
| `comp/pipelinesink/impl/telemetry.go` | Prometheus counters (sent/dropped/bytes/reconnects) |
| `comp/pipelinesink/fx/fx.go` | `fxutil.Module()` |

### Data Flow (Go side)

1. `sink.go` subscribes to all `hook.Hook[MetricView]` and `hook.Hook[LogView]`
   instances provided via the fx group `"hook"`.
2. Each hook callback copies the payload into the appropriate ring buffer in
   `batcher.go`. Copying is required because the `MetricView` / `LogView` interfaces
   document that the underlying data may be reused after the callback returns
   (see `comp/observer/def/component.go` interface doc).
3. A flush goroutine wakes every `pipelinesink.flush_interval` (default 100ms),
   drains all ring buffers, builds a `SignalEnvelope` Cap'n Proto message per signal
   type, and calls `Transport.Send()`.
4. `unixTransport.Send()` writes the packed Cap'n Proto bytes to the socket. If not
   connected it returns an error immediately (no blocking); the batcher records a drop.

### Ring Buffer Behaviour

- Capacity: `pipelinesink.buffer_capacity` (default 1000 per signal type)
- When full, the oldest item is overwritten (circular) and a drop counter increments
- This bounds memory regardless of producer speed

### Reconnect Loop (Go)

```
attempt 1: wait 100ms
attempt 2: wait 200ms
attempt 3: wait 400ms
...
cap at 30s
reset on successful write
```

---

## Rust Sidecar Design

### Files

| File | Responsibility |
|------|---------------|
| `tools/pipelinerecorder/src/main.rs` | Entry point, socket accept loop, SIGTERM handler |
| `tools/pipelinerecorder/src/config.rs` | CLI + env config (clap) |
| `tools/pipelinerecorder/src/transport.rs` | `Transport` trait + `UnixSocketTransport` |
| `tools/pipelinerecorder/src/framing.rs` | Async Cap'n Proto packed-stream frame reader |
| `tools/pipelinerecorder/src/writers/metrics.rs` | Vortex metrics writer |
| `tools/pipelinerecorder/src/writers/logs.rs` | Vortex logs writer |
| `tools/pipelinerecorder/build.rs` | capnpc-rust code generation |
| `tools/pipelinerecorder/tests/e2e_test.rs` | End-to-end test |

### Data Flow (Rust side)

1. `main.rs` binds a Unix socket listener at the configured path.
2. For each accepted connection a tokio task is spawned.
3. `UnixSocketTransport::recv()` reads bytes from the connection, feeds them to
   the Cap'n Proto packed-stream framer.
4. Each complete frame is decoded as `SignalEnvelope`; the union arm dispatches to
   the appropriate writer.
5. Writers accumulate rows. They flush to a new `.vortex` file when either:
   - `pipelinerecorder.flush_rows` rows are buffered (default 10 000), or
   - `pipelinerecorder.flush_interval_secs` seconds have elapsed (default 60)
6. Old `.vortex` files are retained for `pipelinerecorder.retention_hours` (default 24).

---

## Configuration Keys

### Go component (`comp/pipelinesink`)

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `pipelinesink.enabled` | bool | `false` | Enable the component |
| `pipelinesink.socket_path` | string | `/var/run/pipelinesink/pipeline.sock` | Unix socket path |
| `pipelinesink.flush_interval` | duration | `100ms` | How often ring buffers are drained |
| `pipelinesink.buffer_capacity` | int | `1000` | Per-type ring buffer capacity |
| `pipelinesink.reconnect_max_interval` | duration | `30s` | Max backoff between reconnect attempts |

### Rust sidecar (`tools/pipelinerecorder`)

| Key (CLI flag / env var) | Default | Description |
|--------------------------|---------|-------------|
| `--socket-path` / `RECORDER_SOCKET_PATH` | `/var/run/pipelinesink/pipeline.sock` | Unix socket path |
| `--output-dir` / `RECORDER_OUTPUT_DIR` | `/data/signals` | Directory for `.vortex` files |
| `--flush-rows` / `RECORDER_FLUSH_ROWS` | `10000` | Rows per Vortex file |
| `--flush-interval-secs` / `RECORDER_FLUSH_INTERVAL_SECS` | `60` | Time-based flush interval |
| `--retention-hours` / `RECORDER_RETENTION_HOURS` | `24` | Hours to keep old files |

---

## Performance Targets

These are validation targets, not hard limits. Benchmarks must be checked in alongside implementation.

| Metric | Target |
|--------|--------|
| Agent CPU overhead (pipelinesink at 100k metrics/s) | < 5% additional |
| Agent RSS increase (pipelinesink enabled) | < 5 MB |
| Go encoder throughput | ≥ 500k metrics/s |
| Go socket write throughput | ≥ 500k messages/s |
| Rust Cap'n Proto frame decode | ≥ 500k frames/s (criterion) |
| Rust Vortex write throughput | ≥ 1M rows/s per signal type (criterion) |

---

## Docker Compose Dev Environment

`tools/pipelinerecorder/docker-compose.yml` provisions:

- `agent` service: Datadog Agent with `pipelinesink.enabled: true` and socket volume mounted
- `recorder` service: `pipelinerecorder` binary with socket + output volumes mounted
- Shared `emptyDir`-equivalent volume: `pipeline-socket` (for the socket)
- Output volume: `pipeline-data` (for `.vortex` files)

Development workflow:
```bash
cd tools/pipelinerecorder
docker compose up          # start both services
docker stats               # observe CPU / RSS
ls pipeline-data/          # inspect .vortex output files
docker compose down -v     # tear down
```

---

## Kubernetes Deployment

The sidecar is injected via a kustomize strategic-merge patch
(`deploy/kubernetes/pipelinerecorder/sidecar-patch.yaml`):

```yaml
spec:
  template:
    spec:
      containers:
      - name: pipelinerecorder
        image: datadog/pipelinerecorder:latest
        env:
        - name: RECORDER_SOCKET_PATH
          value: /var/run/pipelinesink/pipeline.sock
        - name: RECORDER_OUTPUT_DIR
          value: /data/signals
        volumeMounts:
        - name: pipeline-socket
          mountPath: /var/run/pipelinesink
        - name: pipeline-data
          mountPath: /data/signals
      volumes:
      - name: pipeline-socket
        emptyDir: {}
      - name: pipeline-data
        emptyDir: {}
```

The agent container gets the `pipeline-socket` volume mount added via the same patch.
Validation: `kubectl apply --dry-run=client -k deploy/kubernetes/pipelinerecorder/`

---

## Key Patterns Reused from Existing Code

| Pattern | Source |
|---------|--------|
| Hook group subscription with `fxutil.GetAndFilterGroup()` | `comp/anomalydetection/recorder/impl/recorder.go:102` |
| `Requires` / `Provides` + `NewComponent` constructor | `comp/anomalydetection/recorder/impl/recorder.go:23–45` |
| `fx.Module()` with `fxutil.ProvideOptional` | `comp/anomalydetection/recorder/fx/fx.go` |
| Data copy in hook callback (content safety) | `comp/anomalydetection/recorder/impl/recorder.go:123–125` |
| `hook.Hook[T]` interface | `pkg/hook/hook.go` |
| `MetricView` / `LogView` interfaces | `comp/observer/def/component.go` |
