# LinCos

A small monitor for Linux servers. One Go binary runs on each server and you
look at it from your Mac through SSH: the whole machine, every systemd service
and every Docker container on one live page, with the container's compose
stack, ID, bound folders and log file.

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
# On the server, as root (on ARM use lincos-linux-arm64):
curl -fsSLo /usr/local/bin/lincos https://github.com/ravaee/Mo.LinCos/releases/latest/download/lincos-linux-amd64 && chmod +x /usr/local/bin/lincos
curl -fsSLo /etc/systemd/system/lincos.service https://raw.githubusercontent.com/ravaee/Mo.LinCos/main/deploy/lincos.service && systemctl enable --now lincos
# On your Mac (Apple Silicon), then open any server you can ssh to:
sudo curl -fsSL --create-dirs -o /usr/local/bin/lincos https://github.com/ravaee/Mo.LinCos/releases/latest/download/lincos-darwin-arm64 && sudo chmod +x /usr/local/bin/lincos
lincos open <ssh-host>
```

## Light by design

Measured on a 2-core server running 4 containers and about 460 tasks:

| | |
|---|---|
| Memory | 12–13 MB while a page is open |
| CPU per snapshot | 10–60 ms of mostly waiting, every 2 s |
| With no page open | nothing is sampled at all |
| Cost to Docker | none: the agent follows Docker's event stream instead of asking for the container list (which costs dockerd ~40 ms of CPU per call) |
| Dependencies | the Go standard library only |

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
- **Capped.** The systemd unit sets `GOMEMLIMIT=32MiB`, `MemoryMax=64M`, the
  lowest CPU and IO priority, and a read-only view of the filesystem.

## Install

On the server, as root. Linux with systemd and cgroup v2, amd64 or arm64.
Docker is optional.

```bash
make install HOST=<ssh host>      # from a clone, with Go 1.27+
```

This builds for the server's architecture, copies the binary to
`/usr/local/bin/lincos` and the unit to `/etc/systemd/system/lincos.service`,
and (re)starts it. Without Go, take the binary from a
[release](https://github.com/ravaee/Mo.LinCos/releases) and follow the steps
at the top of [`deploy/lincos.service`](deploy/lincos.service).

On your Mac, put the `lincos` binary (`make mac`, or `lincos-darwin-arm64` from
a release) anywhere on your PATH.

## Use

```bash
lincos open <ssh host>
```

This opens an SSH tunnel to the agent on a free local port and shows the page
in your browser. Ctrl+C closes the tunnel. `<ssh host>` is anything `ssh`
accepts, such as an alias from `~/.ssh/config`.

On the page:
- **Containers** are grouped by compose stack. ▸ shows a container's bound
  folders and volumes, and the path of its log file. Click an ID to copy it.
- **Services:** running, failed or all.
- **Processes:** the 60 busiest by CPU and by memory. Each shows the container
  or service it belongs to. Filter by any text, and sort by CPU or memory.
- **Pause** (or the P key) freezes the numbers so you can read them.

## Security

The agent sees every process and container, so it only ever listens on a
loopback address. It refuses to start on any other address. SSH is the only
way in, and your SSH login is the login. It also refuses requests whose `Host`
header is not `localhost` or `127.0.0.1`, so a web page can't reach it
through your open tunnel by pointing a domain of its own at 127.0.0.1 (DNS
rebinding).

It runs as root, because reading every process and the Docker socket needs
that. systemd makes the whole filesystem read-only to it.

## Roadmap

1. **Live overview:** host, services, containers, processes. *Done.*
2. **Logs:** one viewer for journald, Docker container logs and the log files
   inside each container's bound folders, with live tail, search, time range
   and filters by service or container. Logs are read where they already are,
   never copied.
3. **History:** metrics kept for days, so last night can be looked at.
4. **Requests:** reverse-proxy access logs counted exactly per IP, path and
   status, with only a sample of raw lines kept. Top talkers, rate spikes,
   error floods and scanners.

## Development

```bash
make test     # go vet + go test
make build    # dist/ for linux/amd64, linux/arm64, darwin/arm64
```

Every reader takes its root (`/proc`, `/sys`, the cgroup root) as a parameter,
so the tests run on a Mac against copies of real files in `testdata/`.

| Path | What |
|---|---|
| `cmd/lincos` | the command: `agent`, `open`, `version` |
| `internal/host` | `/proc` and `/sys` readers, cgroup memory and CPU |
| `internal/docker` | Docker API over the Unix socket, event stream, container cgroups |
| `internal/systemd` | `systemctl` list and details |
| `internal/agent` | the sampler, the HTTP server and the live stream (Server-Sent Events) |
| `internal/client` | the Mac side: SSH tunnel and browser |
| `ui/static` | the page: plain HTML, CSS and JavaScript, built into the binary |

MIT licensed.
