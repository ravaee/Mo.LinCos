# LinCos

A small monitor for Linux servers. One Go binary runs on each server and you
look at it from your computer through SSH, in a page that fits one screen:

- **Overview** and **Diagnosis**: the machine in plain sentences, and a
  checklist of what is wrong (a failed service, a crashing container, a full
  disk, a program killed for lack of memory, a pending restart).
- **Services**: each one's state and its live log.
- **Containers**: each one's page with its **Logs**, its **Files** (the folders
  and volumes bound into it: browse, preview, download, upload, delete) and, for
  SQL Server, PostgreSQL and MySQL, its **Database** (databases, tables, rows,
  and a SQL box).

```
Mac                                          Linux server
browser  ── lincos open <host> ── SSH ──►  127.0.0.1:7070   lincos agent
                                                            ├─ /proc, /sys     CPU, memory, load, disks, network, processes
                                                            ├─ systemctl       services and their state
                                                            ├─ Docker socket   containers, compose stacks, mounts, events
                                                            └─ cgroups         CPU and memory per container and service
```

## Quick start

```bash
# 1. On your Linux server (installs or updates the agent):
curl -fsSL https://raw.githubusercontent.com/ravaee/Mo.LinCos/main/install/server.sh | sudo sh

# 2. On your Mac or Linux computer, then open the server:
curl -fsSL https://raw.githubusercontent.com/ravaee/Mo.LinCos/main/install/local.sh | sh
lincos open <ssh-host>
```

On Windows, in PowerShell: `irm https://raw.githubusercontent.com/ravaee/Mo.LinCos/main/install/local.ps1 | iex`

## Light by design

Measured on a 2-core server running 4 containers and about 460 tasks:

| | |
|---|---|
| Memory | 12–16 MB while a page is open |
| CPU per snapshot | 10–60 ms of mostly waiting, every 2 s |
| With no page open | nothing is sampled at all |
| Cost to Docker | none: the agent follows Docker's event stream instead of asking for the container list (which costs dockerd ~40 ms of CPU per call) |
| Dependencies | the Go standard library, plus the SQL Server, PostgreSQL and MySQL drivers (used only while a Database tab is open) |

- **Reads, never copies.** Numbers come straight from `/proc`, `/sys` and
  cgroups. Container CPU and memory come from their cgroups, not from Docker's
  stats API.
- **Each thing at its own pace.** Process command lines, users and cgroups are
  read once per process. Services are listed every 5 s with the cheap
  `systemctl list-units`, and their details are read only when one changes
  state. `systemctl show '*.service'` scans every unit file and takes half a
  second.
- **Memory as it really is.** Container and service memory leave out the file
  cache the kernel drops first under pressure, as `docker stats` does. Otherwise
  containerd or journald look like they use hundreds of MB.
- **Logs and files are read where they are,** never copied: container logs
  through Docker, service logs through journald, files straight from disk.
- **Capped.** The systemd unit sets `GOMEMLIMIT=32MiB`, `MemoryMax=64M` and the
  lowest CPU and IO priority.

## Requirements

- **Server:** Linux with systemd and cgroup v2, amd64 or arm64. Docker is
  optional. The agent goes to `/usr/local/bin/lincos` and runs as the
  `lincos` service. To remove it:
  `systemctl disable --now lincos && rm /usr/local/bin/lincos /etc/systemd/system/lincos.service`
- **Your computer:** macOS, Linux or Windows with `ssh`, which Windows 10 and
  later include.

## Use

```bash
lincos open <ssh host>
```

This opens an SSH tunnel to the agent on a free local port and shows the page
in your browser. Ctrl+C closes the tunnel. `<ssh host>` is anything `ssh`
accepts, such as an alias from `~/.ssh/config`.

On the page, the sidebar has Overview, Diagnosis, Services, Containers (by
compose stack) and Processes. Long lists, logs and tables scroll inside their
own area; the page itself never does.

- **Logs** follow the newest line, search with highlights, show problems only,
  load older lines, and download what is shown.
- **Files** are the container's bound folders and volumes. Click a file to
  preview text or images.
- **Database** signs in with the login the container was started with
  (`MSSQL_SA_PASSWORD`, `POSTGRES_PASSWORD`, `MYSQL_ROOT_PASSWORD`, …) and asks
  for one when there is none. Click a table to page through its rows, or write
  SQL and press Ctrl+Enter.
- **Pause** (or the P key) freezes the numbers so you can read them.

## Security

The agent sees every process and container, so it only ever listens on a
loopback address. It refuses to start on any other address. SSH is the only
way in, and your SSH login is the login. It also refuses requests whose `Host`
header is not `localhost` or `127.0.0.1`, so a web page can't reach it
through your open tunnel by pointing a domain of its own at 127.0.0.1 (DNS
rebinding).

It runs as root, because reading every process and the Docker socket needs
that. systemd keeps the system itself (`/usr`, `/boot`, `/etc`) and home
folders read-only to it.

**Changes need a confirm step.** Deleting or uploading files and SQL that can
change data each open a dialog naming exactly what will happen. SQL counts as
a change unless every statement only reads. Each change is written to the
agent's log, so `journalctl -u lincos | grep change:` lists who deleted,
uploaded or changed what. Requests that change something must carry an
`X-LinCos` header, which a web page on another site can't add, so it can't
make your browser change anything through an open tunnel.

The Database tab never shows or stores the container's password. Container
pages show the names of its settings, never their values.

## Roadmap

1. **Live overview:** host, services, containers, processes. *Done.*
2. **Containers and services in depth:** Diagnosis, logs, files, databases.
   *Done.*
3. **The whole system's logs:** journald and `/var/log`, one searchable view.
4. **History:** metrics kept for days, so last night can be looked at.
5. **Requests:** reverse-proxy access logs counted exactly per IP, path and
   status, with only a sample of raw lines kept. Top talkers, rate spikes,
   error floods and scanners.

## Development

```bash
make test                 # go vet + go test
make build                # every release binary in dist/
make install HOST=<ssh>   # build and install your working copy on a server
```

Every reader takes its root (`/proc`, `/sys`, the cgroup root) as a parameter,
so the tests run on a Mac against copies of real files in `testdata/`.

| Path | What |
|---|---|
| `cmd/lincos` | the command: `agent`, `open`, `version` |
| `internal/host` | `/proc` and `/sys` readers, cgroup memory and CPU |
| `internal/docker` | Docker API over the Unix socket: details, logs, event stream, container cgroups |
| `internal/systemd` | `systemctl` list and details, journald logs |
| `internal/agent` | the sampler, Diagnosis, the HTTP API and the live streams (Server-Sent Events) |
| `internal/files` | reading and changing files inside one folder, never outside it |
| `internal/database` | SQL Server, PostgreSQL and MySQL: detect, sign in, browse, query |
| `internal/client` | the Mac side: SSH tunnel and browser |
| `ui/static` | the page: plain HTML, CSS and JavaScript, no build step, built into the binary |
| `install` | the one-line installers for the server and your computer |

MIT licensed.
