// LinCos page core: live snapshots, the sidebar, routing between pages, and the
// helpers every page uses. Pages register in LinCos.views (views.js, container.js).
"use strict";

const LinCos = (() => {
  const HISTORY = 90; // samples kept for sparklines: 3 minutes at 2 s
  const S = {
    snap: null,
    paused: false,
    history: { cpu: [], netIn: [], netOut: [] },
    view: null, // the page shown: { name, key, impl }
  };
  const views = {};

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
  const pct = (v) => `${Math.round(v ?? 0)}%`;
  const pct1 = (v) => `${(v ?? 0).toFixed(1)}%`;
  function duration(sec) {
    if (!isFinite(sec) || sec < 0) return "";
    const d = Math.floor(sec / 86400), h = Math.floor((sec % 86400) / 3600), m = Math.floor((sec % 3600) / 60);
    if (d) return `${d} day${d > 1 ? "s" : ""}${d < 10 && h ? ` ${h} h` : ""}`;
    if (h) return `${h} h ${m} min`;
    if (m) return `${m} min`;
    return `${Math.floor(sec)} s`;
  }
  const valid = (iso) => { const t = Date.parse(iso); return isNaN(t) || t < 86400000 ? null : t; };
  const since = (iso) => { const t = valid(iso); return t ? duration((Date.now() - t) / 1000) : ""; };
  const ago = (iso) => { const s = since(iso); return s ? `${s} ago` : ""; };
  function when(iso) {
    const t = valid(iso);
    if (!t) return "";
    const d = new Date(t);
    const today = new Date().toDateString() === d.toDateString();
    return today ? d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
                 : d.toLocaleString([], { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
  }
  const short = (id) => (id || "").slice(0, 12);
  const svc = (name) => (name || "").replace(/\.service$/, "");

  function spark(series, colors, max) {
    const w = 300, h = 34;
    const top = max ?? Math.max(1, ...series.flat());
    return `<svg class="spark" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none">${series.map((s, k) => {
      if (s.length < 2) return "";
      const step = w / (HISTORY - 1), off = w - (s.length - 1) * step;
      const pts = s.map((v, i) => `${(off + i * step).toFixed(1)},${(h - 2 - (v / top) * (h - 4)).toFixed(1)}`).join(" ");
      return `<polyline points="${pts}" fill="none" stroke="${colors[k]}" stroke-width="1.6" vector-effect="non-scaling-stroke" stroke-linejoin="round"/>`;
    }).join("")}</svg>`;
  }
  const bar = (parts) => `<div class="bar">${parts.map(([v, c]) => `<i style="width:${Math.max(0, Math.min(100, v)).toFixed(2)}%;background:${c}"></i>`).join("")}</div>`;
  const levelColor = (p, warn, bad) => (p >= bad ? "var(--bad)" : p >= warn ? "var(--warn)" : "var(--accent)");

  // A container's or service's state as a colored dot and a word
  function containerState(c) {
    if (c.state === "running") {
      if (c.health === "unhealthy") return ["bad", "unhealthy"];
      if (c.health === "starting") return ["warn", "starting"];
      return ["good", c.health === "healthy" ? "healthy" : "running"];
    }
    if (c.state === "restarting") return ["bad", "restarting"];
    if (c.state === "exited") return [c.exitCode ? "bad" : "off", c.exitCode ? `stopped, error ${c.exitCode}` : "stopped"];
    return ["off", c.state];
  }
  function serviceState(v) {
    if (v.active === "failed") return ["bad", "failed"];
    if (v.sub === "running") return ["good", "running"];
    if (v.active === "activating") return ["warn", "starting"];
    if (v.sub === "exited") return ["off", "done"];
    return ["off", v.sub || v.active];
  }

  // Every running container and service with the memory and CPU it uses,
  // biggest first: what "which app uses the memory" means
  function apps(s) {
    const list = [];
    for (const c of s.containers || []) {
      if (c.state !== "running") continue;
      list.push({ kind: "container", name: c.name, mem: c.memory, cpu: c.cpu, cls: containerState(c)[0],
        page: `#/containers/${short(c.id)}`, log: `#/logs/container/${short(c.id)}` });
    }
    for (const v of s.services || []) {
      if (!v.memory || v.active === "inactive") continue;
      list.push({ kind: "service", name: svc(v.name), mem: v.memory, cpu: v.cpu, cls: serviceState(v)[0],
        page: `#/services/${v.name}`, log: `#/logs/service/${v.name}` });
    }
    return list.sort((a, b) => b.mem - a.mem);
  }

  // ---------- talking to the agent ----------
  async function api(path, opt = {}) {
    const init = { method: opt.method || "GET", headers: {} };
    if (init.method !== "GET") init.headers["X-LinCos"] = "1";
    if (opt.json !== undefined) {
      init.headers["Content-Type"] = "application/json";
      init.body = JSON.stringify(opt.json);
    } else if (opt.body) {
      init.body = opt.body;
    }
    const res = await fetch(path, init);
    const type = res.headers.get("Content-Type") || "";
    const data = type.includes("json") ? await res.json() : await res.text();
    if (!res.ok) {
      const err = new Error((data && data.error) || `${res.status} ${res.statusText}`);
      err.status = res.status;
      err.data = data;
      throw err;
    }
    return data;
  }

  function toast(msg, bad) {
    const t = document.createElement("div");
    t.className = `toast${bad ? " bad" : ""}`;
    t.textContent = msg;
    document.getElementById("toasts").append(t);
    setTimeout(() => t.remove(), bad ? 6000 : 3000);
  }

  // A confirm step for every change: says exactly what will happen.
  function confirmDialog({ title, text, pre, ok = "OK", danger = false }) {
    const d = document.getElementById("dialog");
    d.innerHTML = `<form method="dialog">
      <div class="d-body"><h3>${esc(title)}</h3>${text ? `<div>${text}</div>` : ""}${pre ? `<pre>${esc(pre)}</pre>` : ""}</div>
      <div class="d-foot"><button class="btn" value="no">Cancel</button><button class="btn ${danger ? "danger solid" : "primary"}" value="yes">${esc(ok)}</button></div>
    </form>`;
    return new Promise((resolve) => {
      d.onclose = () => resolve(d.returnValue === "yes");
      d.returnValue = "";
      d.showModal();
      d.querySelector('[value="no"]').focus();
    });
  }

  // ---------- routing ----------
  // #/overview, #/diagnosis, #/services[/name], #/containers[/id[/tab]], #/processes
  function parse() {
    const parts = location.hash.replace(/^#\/?/, "").split("/").map(decodeURIComponent);
    const name = views[parts[0]] ? parts[0] : "overview";
    return { name, parts: parts.slice(1) };
  }

  function route() {
    const r = parse();
    const impl = views[r.name];
    const key = impl.key ? impl.key(r.parts) : "";
    const view = document.getElementById("view");
    if (S.view && S.view.name === r.name && S.view.key === key) {
      S.view.impl.route?.(r.parts);
    } else {
      S.view?.impl.unmount?.();
      view.innerHTML = "";
      view.className = "view";
      S.view = { name: r.name, key, impl };
      impl.mount(view, r.parts);
      if (S.snap) impl.update?.(S.snap);
    }
    renderNav();
    renderCrumbs();
  }

  function renderCrumbs() {
    const r = parse();
    const crumbs = S.view?.impl.crumbs?.(r.parts, S.snap) || [S.view?.impl.title || ""];
    document.getElementById("crumbs").innerHTML = crumbs.map((c, i) =>
      i < crumbs.length - 1 && Array.isArray(c) ? `<a href="${c[1]}">${esc(c[0])}</a><span class="sep">›</span>` : esc(Array.isArray(c) ? c[0] : c)).join("");
  }

  // ---------- sidebar ----------
  function renderNav() {
    const s = S.snap, r = parse();
    const findings = s?.findings || [];
    const bad = findings.filter((f) => f.level === "bad").length, warn = findings.filter((f) => f.level === "warn").length;
    const failed = (s?.services || []).filter((v) => v.active === "failed").length;
    const ctrs = s?.containers || [];
    const item = (href, label, on, count, cls = "") =>
      `<a class="nav-item ${on ? "on" : ""}" href="${href}">${label}${count !== undefined && count !== "" ? `<span class="count ${cls}">${count}</span>` : ""}</a>`;
    let html = item("#/overview", "Overview", r.name === "overview")
      + item("#/diagnosis", "Diagnosis", r.name === "diagnosis", bad || warn || (s ? "✓" : ""), bad ? "bad" : warn ? "warn" : "")
      + item("#/logs", "Logs", r.name === "logs")
      + item("#/services", "Services", r.name === "services", failed ? `${failed} failed` : (s?.services || []).filter((v) => v.sub === "running").length || "", failed ? "bad" : "")
      + item("#/containers", "Containers", r.name === "containers" && !r.parts[0], ctrs.length || "");
    const groups = new Map();
    for (const c of ctrs) {
      const k = c.project || "";
      if (!groups.has(k)) groups.set(k, []);
      groups.get(k).push(c);
    }
    // Compose stacks by name, then containers started by hand
    const ordered = [...groups].sort(([a], [b]) => (a === "") - (b === "") || a.localeCompare(b));
    for (const [project, cs] of ordered) {
      html += `<div class="nav-group">${esc(project || "not in a stack")}</div>`;
      for (const c of cs) {
        const [cls] = containerState(c);
        const on = r.name === "containers" && r.parts[0] && c.id.startsWith(r.parts[0]);
        html += `<a class="nav-item nav-sub ${on ? "on" : ""}" href="#/containers/${short(c.id)}" title="${esc(c.name)}"><span class="dot ${cls}"></span><span class="name">${esc(c.name)}</span></a>`;
      }
    }
    html += item("#/processes", "Processes", r.name === "processes");
    document.getElementById("nav").innerHTML = html;
  }

  function renderSide(s) {
    const h = s.host;
    document.getElementById("hostline").innerHTML = `<b>${esc(h.name)}</b> · ${esc(h.os)} · ${h.cores} cores · up ${duration(h.uptime)}`;
    document.getElementById("agent").textContent = `agent ${bytes(s.agent.rss)} · ${esc(s.agent.version)}`;
    document.title = `${h.name} · LinCos`;
  }

  // ---------- live data ----------
  function push(arr, v) { arr.push(v); if (arr.length > HISTORY) arr.shift(); }

  function receive(s) {
    push(S.history.cpu, s.cpu.total);
    push(S.history.netIn, s.net.reduce((a, n) => a + n.inRate, 0));
    push(S.history.netOut, s.net.reduce((a, n) => a + n.outRate, 0));
    S.snap = s;
    if (S.paused) return;
    renderSide(s);
    renderNav();
    renderCrumbs();
    S.view?.impl.update?.(s);
  }

  function setLive(cls, text) {
    document.getElementById("live").className = `live ${cls}`;
    document.getElementById("liveText").textContent = text;
  }

  function connect() {
    const es = new EventSource("api/stream");
    es.onmessage = (e) => {
      receive(JSON.parse(e.data));
      if (!S.paused) setLive("ok", `live · every ${S.snap.agent.interval} s`);
    };
    es.onerror = () => setLive("bad", es.readyState === EventSource.CLOSED ? "disconnected" : "reconnecting…");
  }

  function togglePause() {
    S.paused = !S.paused;
    const b = document.getElementById("pause");
    b.classList.toggle("on", S.paused);
    b.textContent = S.paused ? "Resume" : "Pause";
    setLive(S.paused ? "paused" : "ok", S.paused ? "paused" : "live");
    if (!S.paused && S.snap) receive(S.snap);
  }

  function start() {
    document.getElementById("pause").addEventListener("click", togglePause);
    document.addEventListener("keydown", (e) => {
      const typing = /INPUT|TEXTAREA|SELECT/.test(e.target.tagName);
      if (e.key.toLowerCase() === "p" && !typing && !e.metaKey && !e.ctrlKey) togglePause();
    });
    window.addEventListener("hashchange", route);
    route();
    connect();
  }

  return {
    S, views, start, route, api, toast, confirmDialog, renderCrumbs, apps,
    esc, bytes, rate, pct, pct1, duration, since, ago, when, short, svc, spark, bar, levelColor, containerState, serviceState,
  };
})();
