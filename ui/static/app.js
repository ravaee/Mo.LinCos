// LinCos page: receives a snapshot every few seconds over Server-Sent Events and
// redraws. Nothing here talks to anything but the agent it was served from.
"use strict";

const HISTORY = 90; // samples kept for the sparklines (3 minutes at 2s)
const history = { cpu: [], netIn: [], netOut: [] };
const state = {
  paused: false,
  last: null,
  svcFilter: "running",
  procSort: "cpu",
  procSearch: "",
  open: new Set(), // containers whose mounts are shown
};

const $ = (id) => document.getElementById(id);

// ---------- formatting ----------
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);

function bytes(n, digits = 1) {
  if (!n) return "0 B";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return `${n.toFixed(i === 0 ? 0 : n < 10 ? digits : 0)} ${u[i]}`;
}
const rate = (n) => `${bytes(n)}/s`;
const pct = (v) => `${(v ?? 0).toFixed(1)}%`;

function duration(sec) {
  if (!isFinite(sec) || sec < 0) return "";
  const d = Math.floor(sec / 86400), h = Math.floor((sec % 86400) / 3600), m = Math.floor((sec % 3600) / 60);
  if (d) return `${d}d ${h}h`;
  if (h) return `${h}h ${m}m`;
  if (m) return `${m}m`;
  return `${Math.floor(sec)}s`;
}
const since = (iso) => {
  const t = Date.parse(iso);
  return isNaN(t) || t < 86400000 ? "" : duration((Date.now() - t) / 1000);
};
const heat = (v, warm, hot) => (v >= hot ? "hot" : v >= warm ? "warm" : "");

function push(arr, v) { arr.push(v); if (arr.length > HISTORY) arr.shift(); }

function spark(series, colors, max) {
  const w = 300, h = 38;
  const top = max ?? Math.max(1, ...series.flat());
  const lines = series.map((s, k) => {
    if (s.length < 2) return "";
    const step = w / (HISTORY - 1), off = w - (s.length - 1) * step;
    const pts = s.map((v, i) => `${(off + i * step).toFixed(1)},${(h - 2 - (v / top) * (h - 4)).toFixed(1)}`).join(" ");
    return `<polyline points="${pts}" fill="none" stroke="${colors[k]}" stroke-width="1.6" vector-effect="non-scaling-stroke" stroke-linejoin="round"/>`;
  }).join("");
  return `<svg class="spark" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none">${lines}</svg>`;
}

function bar(parts) {
  return `<div class="bar">${parts.map(([v, c]) => `<i style="width:${Math.max(0, Math.min(100, v)).toFixed(2)}%;background:${c}"></i>`).join("")}</div>`;
}

// ---------- header and cards ----------
function renderHeader(s) {
  const h = s.host;
  $("hostline").innerHTML = `<b>${esc(h.name)}</b> · ${esc(h.os)} · kernel ${esc(h.kernel)} · ${h.cores} cores · up ${duration(h.uptime)}`;
  $("agent").textContent = `agent ${bytes(s.agent.rss)} · ${s.agent.sampleMs} ms per sample · ${esc(s.agent.version)}`;
  document.title = `${h.name} · LinCos`;
  const errs = s.errors || [];
  $("errors").hidden = errs.length === 0;
  $("errors").textContent = errs.length ? `Could not read: ${errs.join(" · ")}` : "";
}

function renderCards(s) {
  const m = s.memory;
  const used = m.total - m.available;
  const cache = Math.max(0, Math.min(m.cached, m.available - m.free));
  const cores = s.cpu.cores || [];
  const netIn = s.net.reduce((a, n) => a + n.inRate, 0), netOut = s.net.reduce((a, n) => a + n.outRate, 0);
  const dRead = s.diskIO.reduce((a, d) => a + d.inRate, 0), dWrite = s.diskIO.reduce((a, d) => a + d.outRate, 0);
  const swapUsed = m.swapTotal - m.swapFree;
  const load = s.load, n = s.host.cores || 1;

  $("cards").innerHTML = `
  <div class="card">
    <h3>CPU</h3>
    <div class="big ${heat(s.cpu.total, 70, 90)}">${pct(s.cpu.total)}<small>of ${n} cores</small></div>
    ${spark([history.cpu], ["var(--cpu)"], 100)}
    <div class="cores" title="Each core">${cores.map((c, i) => `<div class="core" title="core ${i}: ${pct(c)}"><i style="height:${c}%"></i></div>`).join("")}</div>
  </div>
  <div class="card">
    <h3>Memory</h3>
    <div class="big ${heat(used / m.total * 100, 80, 92)}">${bytes(used)}<small>used of ${bytes(m.total)}</small></div>
    ${bar([[used / m.total * 100, "var(--mem)"], [cache / m.total * 100, "color-mix(in srgb, var(--mem) 35%, transparent)"]])}
    <div class="legend">
      <span style="--c:var(--mem)">used ${bytes(used)}</span>
      <span style="--c:color-mix(in srgb, var(--mem) 35%, transparent)" title="Files the kernel keeps in memory; given back the moment a program needs it">cache ${bytes(cache)}</span>
    </div>
    <div class="row"><span title="What programs can still get, cache included">available</span><b>${bytes(m.available)}</b></div>
    ${m.swapTotal ? `<div class="row"><span>swap</span><b>${bytes(swapUsed)} of ${bytes(m.swapTotal)}</b></div>`
                  : `<div class="note" title="If memory runs out, the kernel kills a process at once">no swap</div>`}
  </div>
  <div class="card">
    <h3>Load</h3>
    <div class="big ${heat(load.one / n, 0.8, 1.5)}">${load.one.toFixed(2)}<small>${load.five.toFixed(2)} · ${load.fifteen.toFixed(2)}</small></div>
    ${bar([[load.one / n * 100, "var(--cpu)"]])}
    <div class="row"><span title="1-minute load per core; above 1.0 work is waiting">per core</span><b>${(load.one / n).toFixed(2)}</b></div>
    <div class="row"><span>tasks</span><b>${load.running} running / ${load.tasks}</b></div>
  </div>
  <div class="card">
    <h3>Network</h3>
    <div class="row"><span style="color:var(--in)">↓ in</span><b>${rate(netIn)}</b></div>
    <div class="row"><span style="color:var(--out)">↑ out</span><b>${rate(netOut)}</b></div>
    ${spark([history.netIn, history.netOut], ["var(--in)", "var(--out)"])}
    ${s.net.map((i) => `<div class="row"><span>${esc(i.name)}</span><b title="since boot">↓ ${bytes(i.inTotal)} · ↑ ${bytes(i.outTotal)}</b></div>`).join("")}
  </div>
  <div class="card">
    <h3>Disks</h3>
    ${s.disks.map((d) => {
      const p = d.size ? (d.size - d.avail) / d.size * 100 : 0;
      return `<div class="disk">
        <div class="row"><span class="mono" title="${esc(d.device)} · ${esc(d.fs)}">${esc(d.mount)}</span><b class="${heat(p, 80, 92)}">${bytes(d.used)} / ${bytes(d.size)}</b></div>
        ${bar([[p, p >= 92 ? "var(--bad)" : p >= 80 ? "var(--warn)" : "var(--accent)"]])}
      </div>`;
    }).join("")}
    <div class="row"><span>read · write</span><b>${rate(dRead)} · ${rate(dWrite)}</b></div>
  </div>`;
}

// ---------- containers ----------
function stateBadge(c) {
  if (c.state === "running") {
    if (c.health === "unhealthy") return `<span class="badge bad">unhealthy</span>`;
    if (c.health === "starting") return `<span class="badge warn">starting</span>`;
    return `<span class="badge good">${c.health === "healthy" ? "healthy" : "running"}</span>`;
  }
  if (c.state === "restarting") return `<span class="badge bad">restarting</span>`;
  if (c.state === "exited") return `<span class="badge ${c.exitCode ? "bad" : ""}">exited ${c.exitCode}</span>`;
  return `<span class="badge">${esc(c.state)}</span>`;
}

function renderContainers(s) {
  const list = s.containers || [];
  const running = list.filter((c) => c.state === "running").length;
  $("ctrSummary").textContent = list.length ? `${running} running of ${list.length}` : "";
  if (!list.length) {
    $("containers").innerHTML = `<tbody><tr><td class="empty">No containers, or Docker is not installed.</td></tr></tbody>`;
    return;
  }
  const groups = new Map();
  for (const c of list) {
    const k = c.project || "";
    if (!groups.has(k)) groups.set(k, []);
    groups.get(k).push(c);
  }
  let rows = "";
  for (const [project, cs] of groups) {
    const cpu = cs.reduce((a, c) => a + c.cpu, 0), mem = cs.reduce((a, c) => a + c.memory, 0);
    rows += `<tr class="group"><td colspan="11">${project ? esc(project) : "Not in a compose stack"} <span class="muted">· ${cs.length} container${cs.length > 1 ? "s" : ""} · ${pct(cpu)} CPU · ${bytes(mem)}</span></td></tr>`;
    for (const c of cs) {
      const memPct = c.memLimit ? c.memory / c.memLimit * 100 : 0;
      const open = state.open.has(c.id);
      rows += `<tr>
        <td><button class="toggle" data-ctr="${c.id}" title="Bound folders and volumes">${open ? "▾" : "▸"}</button> <b>${esc(c.name)}</b>${c.service && c.service !== c.name ? ` <span class="muted">${esc(c.service)}</span>` : ""}</td>
        <td class="mono id" data-copy="${c.id}" title="Click to copy the full ID">${c.id.slice(0, 12)}</td>
        <td class="mono muted">${esc(c.image)}</td>
        <td>${stateBadge(c)}</td>
        <td class="num" title="${esc(c.status)}">${c.startedAt ? since(c.startedAt) : esc(c.status)}</td>
        <td class="num ${c.restarts ? "warm" : ""}">${c.restarts}</td>
        <td class="num ${heat(c.cpu, 50, 90)}">${c.state === "running" ? pct(c.cpu) : ""}</td>
        <td class="num">${c.state === "running" ? `<span class="minibar">${bytes(c.memory)}${c.memLimit ? ` <span class="muted">/ ${bytes(c.memLimit, 0)}</span>${bar([[memPct, memPct > 90 ? "var(--bad)" : "var(--mem)"]])}` : ""}</span>` : ""}</td>
        <td class="num">${c.state === "running" ? c.pids : ""}</td>
        <td class="num">${c.mounts.length}</td>
        <td class="muted">${esc(c.logDriver)}</td>
      </tr>`;
      if (open) {
        rows += `<tr class="mounts"><td colspan="11"><table>
          ${c.mounts.length ? c.mounts.map((m) => `<tr><td>${esc(m.Type)}</td><td>${esc(m.Source)}</td><td>→ ${esc(m.Destination)}</td><td>${m.RW ? "read-write" : "read-only"}</td></tr>`).join("") : `<tr><td>No folders or volumes bound.</td></tr>`}
          ${c.logPath ? `<tr><td>logs</td><td>${esc(c.logPath)}</td><td></td><td></td></tr>` : ""}
        </table></td></tr>`;
      }
    }
  }
  $("containers").innerHTML = `<thead><tr>
    <th>Name</th><th>ID</th><th>Image</th><th>State</th><th class="num">Up</th><th class="num">Restarts</th>
    <th class="num" title="% of one core">CPU</th><th class="num">Memory</th><th class="num">PIDs</th><th class="num">Mounts</th><th>Logs</th>
  </tr></thead><tbody>${rows}</tbody>`;
}

// ---------- services ----------
function renderServices(s) {
  const all = s.services || [];
  const failed = all.filter((v) => v.active === "failed");
  const list = state.svcFilter === "running" ? all.filter((v) => v.sub === "running")
             : state.svcFilter === "failed" ? failed : all;
  list.sort((a, b) => (b.cpu - a.cpu) || (b.memory - a.memory) || a.name.localeCompare(b.name));
  $("svcSummary").textContent = `${all.filter((v) => v.sub === "running").length} running · ${failed.length} failed · ${all.length} loaded`;
  document.querySelector('#svcFilter [data-v="failed"]').classList.toggle("bad", failed.length > 0);
  if (!list.length) {
    $("services").innerHTML = `<tbody><tr><td class="empty">${state.svcFilter === "failed" ? "No failed services." : "No services."}</td></tr></tbody>`;
    return;
  }
  $("services").innerHTML = `<thead><tr>
    <th>Service</th><th>State</th><th class="num">PID</th><th class="num">Restarts</th><th class="num" title="% of one core">CPU</th><th class="num">Memory</th><th class="num">Up</th>
  </tr></thead><tbody>${list.map((v) => `<tr>
    <td class="fill" title="${esc(v.description)}"><b>${esc(v.name.replace(/\.service$/, ""))}</b> <span class="desc">${esc(v.description)}</span></td>
    <td><span class="badge ${v.active === "failed" ? "bad" : v.sub === "running" ? "good" : ""}">${esc(v.sub || v.active)}</span></td>
    <td class="num mono">${v.pid || ""}</td>
    <td class="num ${v.restarts ? "warm" : ""}">${v.restarts}</td>
    <td class="num ${heat(v.cpu, 50, 90)}">${v.sub === "running" ? pct(v.cpu) : ""}</td>
    <td class="num">${v.memory ? bytes(v.memory) : ""}</td>
    <td class="num">${v.sub === "running" ? since(v.since) : ""}</td>
  </tr>`).join("")}</tbody>`;
}

// ---------- processes ----------
function ownerLabel(p) {
  const o = p.owner || {};
  if (o.kind === "container") return `<span class="owner container" title="container ${esc(o.id)}">${esc(p.ownerName || o.id.slice(0, 12))}</span>`;
  if (o.kind === "service") return `<span class="owner service">${esc(o.id.replace(/\.service$/, ""))}</span>`;
  return `<span class="owner">${esc(o.kind || "")}</span>`;
}

function renderProcesses(s) {
  const q = state.procSearch.toLowerCase();
  let list = (s.processes || []).filter((p) => !q ||
    [p.name, p.cmd, p.user, p.ownerName, p.owner?.id, String(p.pid)].some((v) => v && v.toLowerCase().includes(q)));
  list.sort(state.procSort === "mem" ? (a, b) => b.rss - a.rss : (a, b) => (b.cpu - a.cpu) || (b.rss - a.rss));
  list = list.slice(0, 40);
  $("procSummary").textContent = `busiest ${s.processes.length} of ${s.load.tasks} tasks`;
  $("processes").innerHTML = `<thead><tr>
    <th class="num">PID</th><th>User</th><th>Owner</th><th>Name</th>
    <th class="num sort ${state.procSort === "cpu" ? "on" : ""}" data-sort="cpu" title="% of one core">CPU</th>
    <th class="num sort ${state.procSort === "mem" ? "on" : ""}" data-sort="mem">Memory</th>
    <th class="num">Threads</th><th>Command</th>
  </tr></thead><tbody>${list.length ? list.map((p) => `<tr>
    <td class="num mono">${p.pid}</td>
    <td>${esc(p.user)}</td>
    <td>${ownerLabel(p)}</td>
    <td><b>${esc(p.name)}</b>${p.state === "Z" ? ` <span class="badge bad">zombie</span>` : p.state === "D" ? ` <span class="badge warn" title="Waiting on disk">D</span>` : ""}</td>
    <td class="num ${heat(p.cpu, 50, 90)}">${pct(p.cpu)}</td>
    <td class="num">${bytes(p.rss)}</td>
    <td class="num">${p.threads}</td>
    <td class="fill cmd" title="${esc(p.cmd)}">${esc(p.cmd || `[${p.name}]`)}</td>
  </tr>`).join("") : `<tr><td colspan="8" class="empty">No process matches.</td></tr>`}</tbody>`;
}

// ---------- wiring ----------
function render(s) {
  renderHeader(s);
  renderCards(s);
  renderContainers(s);
  renderServices(s);
  renderProcesses(s);
}

function receive(s) {
  push(history.cpu, s.cpu.total);
  push(history.netIn, s.net.reduce((a, n) => a + n.inRate, 0));
  push(history.netOut, s.net.reduce((a, n) => a + n.outRate, 0));
  state.last = s;
  if (!state.paused) render(s);
}

function setLive(cls, text) {
  $("live").className = `live ${cls}`;
  $("liveText").textContent = text;
}

function connect() {
  const es = new EventSource("api/stream");
  es.onopen = () => setLive(state.paused ? "paused" : "ok", state.paused ? "paused" : "live");
  es.onmessage = (e) => {
    receive(JSON.parse(e.data));
    if (!state.paused) setLive("ok", `live · every ${state.last.agent.interval}s`);
  };
  es.onerror = () => setLive("bad", es.readyState === EventSource.CLOSED ? "disconnected" : "reconnecting…");
}

function togglePause() {
  state.paused = !state.paused;
  $("pause").classList.toggle("on", state.paused);
  $("pause").textContent = state.paused ? "Resume" : "Pause";
  setLive(state.paused ? "paused" : "ok", state.paused ? "paused" : "live");
  if (!state.paused && state.last) render(state.last);
}

document.addEventListener("click", (e) => {
  const t = e.target.closest("[data-ctr],[data-copy],[data-sort],#svcFilter button,#pause");
  if (!t) return;
  if (t.id === "pause") return togglePause();
  if (t.dataset.ctr) {
    const id = t.dataset.ctr;
    state.open.has(id) ? state.open.delete(id) : state.open.add(id);
  } else if (t.dataset.copy) {
    navigator.clipboard?.writeText(t.dataset.copy);
    t.textContent = "copied";
    setTimeout(() => (t.textContent = t.dataset.copy.slice(0, 12)), 900);
    return;
  } else if (t.dataset.sort) {
    state.procSort = t.dataset.sort;
  } else if (t.dataset.v) {
    state.svcFilter = t.dataset.v;
    document.querySelectorAll("#svcFilter button").forEach((b) => b.classList.toggle("on", b === t));
  }
  if (state.last) render(state.last);
});

$("procSearch").addEventListener("input", (e) => {
  state.procSearch = e.target.value;
  if (state.last) renderProcesses(state.last);
});

document.addEventListener("keydown", (e) => {
  if (e.key.toLowerCase() === "p" && e.target.tagName !== "INPUT" && !e.metaKey && !e.ctrlKey) togglePause();
});

connect();
