# logsurge

`logsurge` is a small log capture and forwarding tool for commands, scripts,
files, local sockets, and journald. It is meant for low power Linux hosts where
logging must not become a noisy neighbor, such as Raspberry Pi Compute Module 4
systems running timing sensitive 3D printer control stacks. It timestamps
records as they arrive, keeps memory bounded, and writes to stdout, rotating
directories, or HTTP collectors.

The main reason to use it is the mix of command wrapper and bounded forwarder.
You can wrap one command, put it in a shell script, or run it under a supervisor
as a host forwarder. The directory sink uses a familiar `current` file with
rotated timestamp files. Other sinks can add JSON metadata, HTTP forwarding,
auth, and health metrics.

`logsurge` is not trying to replace Fluent Bit, Vector, rsyslog, or syslog-ng.
It fits smaller jobs where a predictable wrapper and simple forwarder are more
useful than a full observability agent, especially when CPU, memory, and I/O
headroom matter more than a plugin ecosystem.

## Quick Examples

```
logsurge -- make test
tail -f app.log | logsurge
logsurge --file /var/log/myjob/current
logsurge --listen tcp://127.0.0.1:5514
logsurge --config config.toml
logsurge --format json -- terraform plan
logsurge --format json -- ./job
logsurge --format json --metadata hostname,source,line_end -- ./job
logsurge --output dir=/var/log/myjob -- ./job
logsurge --format plain --output dir=/var/log/cron/myjob -- /usr/local/bin/myjob
logsurge --output stdout --output http=http://127.0.0.1:8080/logs -- ./job
logsurge --format json --output http=http://127.0.0.1:8080/logs -- ./job
logsurge --output http=https://collector.example/logs \
  --http-auth bearer --http-auth-secret-file /etc/logsurge/secrets/collector -- ./job
logsurge --output http=https://collector.example/logs \
  --http-auth basic --http-auth-secret-file /etc/logsurge/secrets/collector-basic -- ./job
```

Common short aliases are available for frequent flags:
`-c/--config`, `-f/--file`, `-F/--format`, `-o/--output`,
and `-m/--metadata`.

Plain output:

```
2026-05-02T11:49:53,951493000+02:00 building target
```

JSON output:

```
{"time":"2026-05-02T11:49:53,951493000+02:00","line":"building target"}
```

## Usage Models

### Command Wrapper

Use `logsurge -- command [args...]` when you want to run one command and collect
its combined stdout/stderr stream:

```
logsurge -- make test
logsurge --format json -- terraform plan
logsurge --format json --metadata hostname,source,line_end -- ./job
```

This mode is useful for CI jobs, deployment commands, maintenance scripts, and
any task where the log collector should exit when the command exits.

For cron shell scripts, Bash process substitution plus the directory sink
gives you timestamped plain logs that can be inspected later:

```
#!/usr/bin/env bash
set -euo pipefail

exec > >(logsurge \
  --format plain \
  --output dir=/var/log/cron/db-backup) 2>&1

echo "backup started"
/usr/local/sbin/db-backup
echo "backup finished"
```

The script's combined output is kept under `/var/log/cron/db-backup/current`
and rotated files. `set -e` still makes the script fail when a command fails.

### Whole Script Capture With Bash

Bash process substitution can route a whole script's stdout and stderr through
`logsurge`:

```
#!/usr/bin/env bash
set -euo pipefail

exec > >(logsurge \
  --format json \
  --metadata source,line_end \
  --output http=https://collector.example/logs \
  --http-auth bearer \
  --http-auth-secret-file /etc/logsurge/secrets/collector) 2>&1

echo "script started"
./do-work
echo "script finished"
```

The `exec > >(logsurge ...) 2>&1` line is specific to Bash. It redirects the
script's stdout into `logsurge` and then points stderr at that same stream.

### Local Directory Logs

Use the directory sink when you want a simple local log directory:

```
logsurge --output dir=/var/log/myjob -- ./job
```

The directory sink writes `/var/log/myjob/current`, rotates it to timestamped
`.log` files such as `20260503T151459.815047695Z.log` before it grows past
`--dir-max-bytes`, and keeps the newest `--dir-max-files` rotated files. This
keeps the useful parts of `svlogd` with one append target, simple rotation, and
bounded retention.

### Daemon Forwarding

Use daemon mode when `logsurge` should run as a long running process, often as
root, and forward one or more service logs or journal streams into a central
place:

```
logsurge --config /etc/logsurge/config.toml
```

Example `/etc/logsurge/config.toml`:

```
format = "json"
metadata = ["source", "hostname", "line_end"]
custom_metadata = ["terraform_run=initial"]
custom_metadata_file = "/etc/logsurge/metadata.json"
ansi = "strip"
flush_interval = "100ms"
health_listen = "127.0.0.1:9099"

[output]
kind = "http"
target = "https://collector.example/logs"
http_format = "jsonline"
http_batch_bytes = "4M"
auth = "basic"
auth_secret_file = "/etc/logsurge/secrets/collector-basic"

[queue]
records = 65536
bytes = "64M"
overflow = "drop-oldest"

[file]
start = "end"
poll_interval = "250ms"
partial_flush_interval = "1s"

[[inputs]]
path = "/var/log/nginx/access.log"
source = "nginx-access"
ansi = "keep"
queue_records = 32768
queue_bytes = "32M"
file_poll_interval = "100ms"
partial_flush_interval = "250ms"

[[inputs]]
path = "/var/log/app/current"
source = "app"

[[inputs]]
kind = "journal"
directory = "/var/log/journal"
source = "journald"
start = "end"

[[inputs]]
kind = "tcp"
listen = "127.0.0.1:5514"
source = "local-tcp"

[[inputs]]
kind = "udp"
listen = "127.0.0.1:5515"
source = "local-udp"
```

For Basic auth, the secret file contains `user:password`. For Bearer auth, it
contains the raw token. Keep `/etc/logsurge/secrets` readable only by the account
running `logsurge`.

## Behavior

- stdout and stderr are intentionally merged into one combined stream
- when no `-- command` is provided, stdin is used as the input source
- `--file PATH` follows a file by path, including initial absence and rotation
- `--listen tcp://...` or `--listen udp://...` accepts loopback network input
- complete lines are emitted promptly
- bytes without a trailing newline are emitted after `--partial-flush-interval`
- the child exit code is propagated
- if stdout breaks, `logsurge` terminates the child process group
- after the direct child exits, inherited stdout/stderr are drained for
  `--post-exit-drain-timeout`

## Overflow

Ad hoc mode defaults to blocking overflow so records are not dropped unless you
choose a lossy policy:

```
logsurge --overflow block -- ./important-command
```

In blocking mode, a full queue backpressures the input source. For exec input,
that can eventually fill the pipe and block the child process. This is useful
when complete retention matters and the sink is local and predictable, but it
can be painful with slow disks, stalled stdout consumers, or network output.

For HTTP or any remote receiver, prefer `drop-oldest` unless you explicitly want
the wrapped command to slow down or stall when the receiver cannot keep up.
`drop-oldest` keeps recent records flowing through a bounded queue. Use
`drop-newest` when preserving the queued backlog matters more than keeping the
freshest logs:

```
logsurge --overflow drop-oldest -- ./very-chatty-command
logsurge --overflow drop-newest -- ./preserve-backlog
```

Lossy modes emit a visible diagnostic when records are dropped. Daemon mode
handles multiple inputs and defaults to `drop-oldest`. It does not support
`overflow = "block"` because one slow file should not stall the others.

## Buffering

`logsurge` can only print bytes after the child process writes them to stdout or
stderr. It does not use PTY emulation, `stdbuf`, or preload shims.

If a specific command buffers too much, use that command's own options or
environment. Examples:

```
logsurge -- python3 -u script.py
PYTHONUNBUFFERED=1 logsurge -- python3 script.py
CI=1 logsurge -- npm test
grep --line-buffered pattern file | logsurge -- cat
```

This is the same practical limit as any collector that is not attached to a
terminal. It can stream bytes that are written, but it cannot display data still
buffered inside the child process.

## Performance Design

`logsurge` is designed to stay small and predictable under load. The target
environment includes embedded and low power Linux systems where extra CPU,
memory, or disk pressure can hurt the workload the machine is actually meant to
run. On 3D printer controllers, for example, resource spikes can contribute to
timing problems in the motion and CAN bus stack. The main resource guard is the
queue. It is bounded by both record count and bytes, so a large number of tiny
lines and a smaller number of large lines are both capped.
Ad hoc command wrapping defaults to `block` because losing logs from a command
run is often surprising. Daemon mode defaults to `drop-oldest` and rejects
blocking overflow because one slow input or output should not stall unrelated
inputs.

The framer uses a chunk reader instead of `bufio.Scanner`. Scanner has token
limits that are awkward for logs, while the chunk reader can split long logical
lines at `--max-fragment-bytes`, flush unterminated partial lines after an idle
timeout, and still keep memory bounded. ANSI stripping happens after records are
popped from the queue, so terminal cleanup work does not sit on the ingest path.

JSON formatting on hot paths is written directly instead of building maps for
every record and relying on reflection. That is less idiomatic Go, but it avoids
avoidable allocation and CPU cost in the formatter and HTTP encoders. The JSON
escaping helpers are covered by tests for quotes, backslashes, control bytes,
Unicode text, and invalid UTF-8 so the output remains valid JSON.

A reference benchmark on Linux amd64 with `GOMAXPROCS=1` compared the earlier
`encoding/json` map and struct based implementation with the manual encoder.
These are focused encoder and writer benchmarks, so they show the hot path
improvement rather than the whole process cost:

```
path                         old ns/op   current ns/op   speedup   allocs/op
JSON writer                      392.6          225.6       1.74x      3 to 1
JSON writer with metadata       1389.0          402.6       3.45x     18 to 3
HTTP jsonline                    668.3          181.5       3.68x     11 to 1
HTTP jsonline with metadata     1590.0          343.7       4.63x     25 to 3
HTTP GELF                        854.6          147.8       5.78x     15 to 1
HTTP GELF with metadata         2037.0          305.4       6.67x     31 to 3
```

The full binary path also improved. With 3 million records sent to JSON stdout,
plain JSON went from about `1.35s` to `0.97s`. JSON with
`source,line_end,continued` metadata went from about `2.97s` to `1.20s`.

Metadata is intentionally simple. Static metadata is parsed once at startup.
Per record metadata is limited to fields already known while framing records.
Dynamic metric fields such as CPU, memory, or load average are left out of log
metadata so they do not add sampling and allocation cost to every line.

HTTP output batches records and retries within fixed bounds. Batches are capped
by both record count and encoded bytes, but the sink is not a durable offline
spool. For remote receivers, prefer `drop-oldest` so a stalled collector does
not backpressure the wrapped command forever. Output fanout is deliberately
simple. Outputs are written through one serialized writer path, which keeps
resource use low, but a slow sink can slow the whole fanout.

Some features are left out on purpose. That includes PTY emulation, `stdbuf` or
preload tricks, cgo, direct `.journal` parsing, libsystemd integration, and a
plugin framework. They would make this tool heavier and harder to reason about.
The tradeoff is that child side buffering must be handled by the child command,
stdout and stderr are not labeled by default, and long remote outages either
drop or fail according to the configured overflow and sink behavior.

## ANSI Cleanup

ANSI color and terminal control sequences are stripped by default:

```
logsurge --format json -- terraform plan
```

This keeps JSON and later ingestion clean for tools that colorize output. Use
`--ansi keep` when terminal escape sequences are meaningful and should be
preserved:

```
logsurge --ansi keep -- ./colored-command
```

The cleanup happens after records leave the queue, immediately before
formatting and sink writes. Ingestion still timestamps and queues the original
bytes, so slow ANSI parsing cannot back up the input path.

## Input Modes

`logsurge -- command [args...]` runs a child command and captures its merged
stdout/stderr stream.

`producer | logsurge` reads from stdin and exits after stdin reaches EOF.

`logsurge --file PATH` follows `PATH` until interrupted. If the file is missing
at startup, `logsurge` waits for it to appear. Existing files start at EOF by
default, like `tail -F`. Use `--file-start beginning` to backfill existing
contents. When the file is renamed or replaced, `logsurge` drains the old file
and then switches to the new file at the watched path. If the same file is
truncated, reading resumes from the beginning.

The open file descriptor protects rename/unlink rotation. Even if the rotated
path is compressed and removed, `logsurge` can still drain bytes that were in
the old file. `copytruncate` is different because truncation destroys unread
bytes in place, and very fast multiple rotations can still lose intermediate
replacement files that were never opened.

`logsurge --config PATH` runs in daemon mode. It supports one or more file,
journald, TCP, or UDP inputs, rejects user supplied exec commands, and uses a
separate bounded queue per input before writing to one shared sink or output
fanout.
Daemon mode defaults to `drop-oldest` and does not support `overflow = "block"`
because one slow file should not stall the others.

The `[queue]` and `[file]` sections define defaults for all inputs. Each
`[[inputs]]` can override those defaults when one file is much noisier, more
latency sensitive, needs different follow behavior, or needs different ANSI
handling. Journal inputs support their own `kind`, `directory`, `start`, and
`cursor_file` keys. Network inputs support `kind`, `listen`, and TCP
`max_connections`. Daemon mode can use either a single `[output]` section or
repeated `[[outputs]]` sections with distinct output kinds.

Per-input override keys:

- `queue_records`
- `queue_bytes`
- `overflow`
- `max_fragment_bytes`
- `partial_flush_interval`
- `file_start`
- `file_poll_interval`
- `ansi`

Journal input keys:

- `kind = "journal"`
- `directory`
- `start`
- `cursor_file`

Network input keys:

- `kind = "tcp"` or `kind = "udp"`
- `listen`
- `max_connections` for TCP

Example `config.toml`:

```
format = "json"
metadata = ["source", "line_end"]
custom_metadata = ["environment=prod"]
custom_metadata_file = "/etc/logsurge/metadata.json"
ansi = "strip"
flush_interval = "100ms"

[output]
kind = "stdout"

# Or replace [output] with repeated [[outputs]] sections:
# [[outputs]]
# kind = "stdout"
#
# [[outputs]]
# kind = "http"
# target = "https://collector.example/logs"

[queue]
records = 65536
bytes = "64M"
overflow = "drop-oldest"
max_fragment_bytes = "64K"

[file]
start = "end"
poll_interval = "250ms"
partial_flush_interval = "1s"

[[inputs]]
path = "/var/log/app/current"
source = "app"

[[inputs]]
path = "/var/log/nginx/access.log"
source = "nginx-access"
ansi = "keep"
queue_records = 32768
queue_bytes = "32M"
file_poll_interval = "100ms"
partial_flush_interval = "250ms"

[[inputs]]
kind = "journal"
directory = "/var/log/journal"
source = "journald"
start = "end"

[[inputs]]
kind = "tcp"
listen = "127.0.0.1:5514"
source = "local-tcp"
max_connections = 64

[[inputs]]
kind = "udp"
listen = "127.0.0.1:5515"
source = "local-udp"
```

### Journald Input

Journal input only runs in daemon mode and uses one fixed internal `journalctl`
helper per configured input. It does not parse `.journal` files directly,
because the journal file format is binary and systemd's supported reader already
handles rotation, compression, binary fields, and cursor state.

```
[[inputs]]
kind = "journal"
directory = "/var/log/journal"
source = "journald"
start = "end"
cursor_file = "/var/lib/logsurge/journal.cursor"
```

`start = "end"` follows only new entries. `start = "all"` backfills accessible
entries and then follows. If `cursor_file` is set, `journalctl` resumes after the
saved cursor and updates the file as it reads.

`logsurge` extracts `MESSAGE` as the log line, uses
`__REALTIME_TIMESTAMP` as the record timestamp when available, and adds selected
journal fields under metadata names such as `journal_cursor`, `journal_unit`,
`journal_identifier`, `journal_priority`, `journal_pid`, `journal_transport`,
and `journal_boot_id`. Journal input still uses the same bounded queue,
overflow behavior, formatting, and sinks as file inputs.

### Network Input

Network input accepts local TCP or UDP log streams. It is loopback only by
design. `localhost`, `127.0.0.0/8`, and `::1` are allowed, while wildcard and
other addresses are rejected. This is meant for local syslog style forwarding or
local tools that can stream to a socket, not for public log collection.

Ad hoc network input runs until Ctrl-C, SIGTERM, or sink/source failure:

```
logsurge --listen tcp://127.0.0.1:5514 --output stdout
logsurge --listen udp://127.0.0.1:5515 --format json
```

Daemon mode supports TCP and UDP inputs alongside file and journal inputs:

```
[[inputs]]
kind = "tcp"
listen = "127.0.0.1:5514"
source = "local-tcp"
max_connections = 64

[[inputs]]
kind = "udp"
listen = "127.0.0.1:5515"
source = "local-udp"
```

TCP input uses the same newline framing, partial flush interval, and maximum
fragment behavior as stdin and file input. UDP treats each datagram as one
record after trimming a trailing newline. UDP cannot reliably backpressure the
sender, so use `drop-oldest` when the receiving side must stay bounded under
bursts.

### Health And Metrics

The health endpoint is optional and loopback only:

```
logsurge --health-listen 127.0.0.1:9099 --listen tcp://127.0.0.1:5514
```

Daemon config uses the root key:

```
health_listen = "127.0.0.1:9099"
```

`GET /health` returns `ok` while the process is serving. `GET /metrics` returns
Prometheus text metrics for input records/bytes accepted, written, dropped,
queue occupancy, source errors, sink errors, active TCP connections, and UDP
datagrams.

### Positioning

Use Fluent Bit, Vector, Grafana Alloy, rsyslog, or syslog-ng when you need a
fleet scale observability pipeline, service discovery, a plugin ecosystem,
durable spooling, or a rich transform language. Use `logsurge` when you need a
small command wrapper and bounded host forwarder with predictable resource
limits.

## JSON Metadata

JSON output can include metadata:

```
logsurge --format json \
  --metadata hostname,source,line_end,continued \
  -- ./job
```

The metadata appears under a nested `metadata` object:

```
{"time":"...","line":"building","metadata":{"hostname":"builder","source":"combined","line_end":"newline","continued":false}}
```

Supported fields:

- `hostname`: system hostname
- `source`: source identifier such as `combined`, `stdin`, or a configured file source
- `line_end`: why the line or fragment was emitted
- `continued`: whether this record continues an earlier partial fragment

`hostname` is captured once when the pipeline starts. `source`, `line_end`, and
`continued` are computed for each record. Metrics such as CPU, memory, and load
average should be gathered separately and correlated by timestamp.

Static custom metadata can be added from repeatable `KEY=VALUE` pairs or a JSON
file read once at startup:

```
logsurge --format json \
  --metadata-field terraform_run=initial \
  --metadata-field workspace=prod \
  --metadata-file /etc/logsurge/metadata.json \
  -- ./terraform-wrapper
```

The JSON file must be a flat object. String, number, boolean, and `null` values
are allowed. Nested objects and arrays are rejected. File values load first.
Inline `--metadata-field` values override matching file keys. Custom keys must
start with a letter and may contain letters, digits, `_`, `-`, and `.`. Built in
and wire format keys such as `time`, `line`, `metadata`, `hostname`, `source`,
`line_end`, `continued`, `_time`, `_msg`, `host`, and `short_message` are
reserved.

For stdout and directory JSON output, custom metadata is placed under the same
nested `metadata` object. Plain output is unchanged. For HTTP output,
`--http-format jsonline` emits custom metadata as top level JSON fields, while
`--http-format gelf` emits them as GELF additional fields with an underscore
prefix, such as `_terraform_run`.

Daemon config supports the same metadata loaded at startup:

```
custom_metadata = ["terraform_run=initial", "workspace=prod"]
custom_metadata_file = "/etc/logsurge/metadata.json"
```

## Sinks

`--output stdout` writes formatted records to stdout. This is useful for
pipelines, tests, and command wrapping.

`--output dir=/path` writes to `/path/current`, rotates `current` to timestamped
`.log` files such as `20260503T151459.815047695Z.log` when it would exceed
`--dir-max-bytes`, and retains the newest `--dir-max-files` rotated files. A
`lock` file prevents concurrent writers on Unix. This is the local `svlogd`
style sink.

`--output http=http://...` posts batches to an HTTP endpoint. HTTP wire format
is controlled by `--http-format`, separately from stdout/directory `--format`.
The default `--http-format jsonline` sends NDJSON with `_time` and `_msg` fields
for receivers such as VictoriaLogs `/insert/jsonline`.
`--http-format gelf` sends GELF JSON records for Graylog GELF HTTP inputs.
Tune batching and delivery with `--http-batch-records`, `--http-batch-bytes`,
`--http-timeout`, and `--http-retries`. `--http-batch-records` caps records per
POST, while `--http-batch-bytes` caps encoded payload bytes. The byte cap matters
for difficult input where JSON escaping expands quotes, backslashes, control
bytes, or invalid UTF-8. `--flush-interval` also bounds how long a partial batch
waits before being posted. GELF defaults to one record per POST unless
`--http-batch-records` is set explicitly.

HTTP output is not a durable offline spool. If the receiver is slow, unreachable,
or repeatedly failing, blocking overflow can push that pain back into the child
command. For network forwarding, set `--overflow drop-oldest` in ad hoc mode
unless blocking the producer is the behavior you want.

Multiple outputs can be enabled by repeating `--output` with distinct kinds:

```
logsurge --output stdout --output http=https://collector.example/logs -- ./job
logsurge --output stdout --output dir=/var/log/myjob -- ./job
```

All outputs receive the same format and metadata. The writer is intentionally
serialized. A slow output slows the whole pipeline, and a fatal sink error stops
the child/source as it does for a single output.

HTTP output supports Bearer and Basic auth. For Bearer, set
`--http-auth bearer`. For Basic, set `--http-auth basic`. If
`--http-auth-secret-file PATH` is set, the secret file is read once when the
sink is opened and trailing newlines are stripped. If no secret file is set,
`logsurge` reads `LOGSURGE_HTTP_AUTH_SECRET` instead. Empty or unset env means no
auth header is sent. Bearer secrets are sent as `Authorization: Bearer
<secret>`. Basic secrets must contain `user:password`. Keep secret files and
secret directories readable only by the account running `logsurge`.

An explicit secret file wins over `LOGSURGE_HTTP_AUTH_SECRET`. Missing, empty, or
malformed explicit secret files are startup errors. A malformed Basic auth env
value is also a startup error if it is not empty.

After bounded retries, HTTP `401` and `403` responses are treated as nonfatal:
`logsurge` writes a warning to stderr, drops that batch, and keeps the input or
child process alive. Other HTTP statuses and network failures remain sink
failures.

Daemon mode uses the same HTTP output kind for `http://` and `https://` targets:

```
[output]
kind = "http"
target = "https://collector.example/logs"
http_format = "jsonline"
http_batch_bytes = "4M"
auth = "bearer"
auth_secret_file = "/etc/logsurge/secrets/collector"
```

For Basic auth in daemon mode:

```
[output]
kind = "http"
target = "https://collector.example/logs"
http_format = "jsonline"
auth = "basic"
auth_secret_file = "/etc/logsurge/secrets/collector-basic"
```

For Graylog GELF HTTP:

```
[output]
kind = "http"
target = "https://graylog.example/gelf"
http_format = "gelf"
```

## Build

```
make test
make build
make static
```

The binary is written under `build/$(GOOS)-$(GOARCH)/bin/logsurge`.
`make static` writes `build/$(GOOS)-$(GOARCH)/bin/logsurge-static` with
`CGO_ENABLED=0` plus pure Go DNS and user lookup tags.

## Performance Profiling

For optional local profiling, run:

```
make perf
make perf-adhoc-smoke
make flood-smoke
```

`make perf` runs Go benchmarks with allocation output and writes benchmark CPU
and heap profiles under `build/$(GOOS)-$(GOARCH)/perf/`.

`make perf-adhoc-smoke` profiles binary level ad hoc command wrapping for plain
stdout, JSON stdout, JSON metadata, and heavy UTF-8 plus ANSI stripping. It
writes profiles under `build/$(GOOS)-$(GOARCH)/perf/adhoc/`.

This builds `logsurge`, runs it as the receiver, spawns a hidden deterministic
producer as the child process, and writes profiles or flood logs under
`build/$(GOOS)-$(GOARCH)/`.
