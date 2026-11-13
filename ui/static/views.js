// The pages: Overview, Diagnosis, Services and Processes. Each mounts once into
// the view and redraws only its live parts when a new snapshot comes.
"use strict";

(() => {
  const L = LinCos;
  const { esc, bytes, rate, pct, pct1, duration, since, short, svc, spark, bar, levelColor, containerState, serviceState } = L;
  const ICON = { bad: "✕", warn: "!", info: "i", ok: "✓" };

  const ownerLabel = (p) => {
    const o = p.owner || {};
    if (o.kind === "container") return `<a class="owner container" href="#/containers/${short(o.id)}">${esc(p.ownerName || short(o.id))}</a>`;
    if (o.kind === "service") return `<a class="owner service" href="#/services/${esc(o.id)}">${esc(svc(o.id))}</a>`;
    return `<span class="owner">${esc(o.kind || "")}</span>`;
  };

  function findingHTML(f) {
    return `<div class="finding ${f.level}">
      <span class="icon ${f.level}">${ICON[f.level]}</span>
      <span class="title">${esc(f.title)}</span>
      ${f.detail ? `<span class="detail">${esc(f.detail)}</span>` : ""}
      ${f.link ? `<a class="btn small" href="${esc(f.link)}">Open</a>` : ""}
    </div>`;
  }

  // ---------- Overview ----------
  L.views.overview = {
    title: "Overview",
    mount(el) {
      el.innerHTML = `<section class="vitals" id="vitals"></section>
        <section class="overview-grid">
          <div class="panel"><div class="panel-head"><h2>Health</h2><a class="more" href="#/diagnosis">All checks ›</a></div><div class="scroll" id="ovHealth"></div></div>
          <div class="panel"><div class="panel-head"><h2>Containers</h2><a class="more" href="#/containers">All containers ›</a></div><div class="scroll" id="ovContainers"></div></div>
          <div class="panel"><div class="panel-head"><h2>Busiest programs</h2><a class="more" href="#/processes">All processes ›</a></div><div class="scroll" id="ovProcs"></div></div>
        </section>`;
    },
    update(s) {
      const m = s.memory, H = L.S.history;
      const used = m.total - m.available;
      const cache = Math.max(0, Math.min(m.cached, m.available - m.free));
      const disks = s.disks.filter((d) => d.size >= 1 << 30);
      const root = disks.find((d) => d.mount === "/") || disks[0];
      const dPct = root ? (root.size - root.avail) / root.size * 100 : 0;
      const write = s.diskIO.reduce((a, d) => a + d.outRate, 0), read = s.diskIO.reduce((a, d) => a + d.inRate, 0);
      const netIn = s.net.reduce((a, n) => a + n.inRate, 0), netOut = s.net.reduce((a, n) => a + n.outRate, 0);
      const inTotal = s.net.reduce((a, n) => a + n.inTotal, 0), outTotal = s.net.reduce((a, n) => a + n.outTotal, 0);
      const perCore = s.load.five / (s.host.cores || 1);

      document.getElementById("vitals").innerHTML = `
        <div class="vital"><h3>CPU</h3>
          <div class="big">${pct(s.cpu.total)}<small>busy</small></div>
          <div class="say">${s.host.cores} cores · <b>${perCore.toFixed(2)}</b> waiting per core <span title="Load over 5 minutes. Below 1 means nothing waits for the CPU.">ⓘ</span></div>
          ${spark([H.cpu], ["var(--cpu)"], 100)}
        </div>
        <div class="vital"><h3>Memory</h3>
          <div class="big">${bytes(used)}<small>used of ${bytes(m.total)}</small></div>
          ${bar([[used / m.total * 100, levelColor(used / m.total * 100, 80, 90)], [cache / m.total * 100, "color-mix(in srgb, var(--mem) 30%, transparent)"]])}
          <div class="say"><b>${bytes(m.available)}</b> free for programs</div>
          <div class="say" title="Linux keeps recently used files in spare memory and hands it back the moment a program needs it">${bytes(cache)} is file cache · ${m.swapTotal ? `swap ${bytes(m.swapTotal - m.swapFree)} of ${bytes(m.swapTotal)}` : "no swap"}</div>
        </div>
        <div class="vital"><h3>Disk${root ? ` <span class="mono">${esc(root.mount)}</span>` : ""}</h3>
          ${root ? `<div class="big">${pct(dPct)}<small>of ${bytes(root.size)} used</small></div>
          ${bar([[dPct, levelColor(dPct, 80, 90)]])}
          <div class="say"><b>${bytes(root.avail)}</b> free</div>
          <div class="say">reading ${rate(read)} · writing ${rate(write)}</div>` : `<div class="say">No disk found.</div>`}
        </div>
        <div class="vital"><h3>Network</h3>
          <div class="big"><span style="color:var(--in)">↓</span> ${rate(netIn)}<small><span style="color:var(--out)">↑</span> ${rate(netOut)}</small></div>
          <div class="say">since start ↓ ${bytes(inTotal)} · ↑ ${bytes(outTotal)}</div>
          ${spark([H.netIn, H.netOut], ["var(--in)", "var(--out)"])}
        </div>`;

      // Health: problems first; when there are none, say so and show what was checked
      const fs = s.findings || [];
      const issues = fs.filter((f) => f.level !== "ok");
      const fine = fs.filter((f) => f.level === "ok");
      document.getElementById("ovHealth").innerHTML =
        (issues.length ? issues.map(findingHTML).join("") : `<div class="finding ok"><span class="icon ok">✓</span><span class="title">Everything checked is fine</span></div>`)
        + (fine.length ? `<div class="section-title">Fine</div>` + fine.map(findingHTML).join("") : "");

      const ctrs = s.containers || [];
      document.getElementById("ovContainers").innerHTML = ctrs.length ? `<table class="grid"><tbody>${ctrs.map((c) => {
        const [cls, word] = containerState(c);
        return `<tr class="click" onclick="location.hash='#/containers/${short(c.id)}'">
          <td class="cut"><span class="dot ${cls}"></span> <b>${esc(c.name)}</b> <span class="muted">${esc(word)}</span></td>
          <td class="num">${c.state === "running" ? pct1(c.cpu) : ""}</td>
          <td class="num">${c.state === "running" ? bytes(c.memory) : ""}</td></tr>`;
      }).join("")}</tbody></table>` : `<div class="empty">No containers, or Docker is not installed.</div>`;

      const procs = [...s.processes].sort((a, b) => (b.cpu - a.cpu) || (b.rss - a.rss)).slice(0, 12);
      document.getElementById("ovProcs").innerHTML = `<table class="grid"><tbody>${procs.map((p) => `<tr>
        <td class="cut"><b>${esc(p.name)}</b> ${ownerLabel(p)}</td>
        <td class="num">${pct1(p.cpu)}</td><td class="num">${bytes(p.rss)}</td></tr>`).join("")}</tbody></table>`;
    },
  };

  // ---------- Diagnosis ----------
  L.views.diagnosis = {
    title: "Diagnosis",
    mount(el) {
      el.innerHTML = `<div class="panel fill"><div class="panel-head"><h2 id="dxSummary">Checking…</h2><span class="more">checked again every few seconds</span></div><div class="scroll" id="dxList"></div></div>`;
    },
    update(s) {
      const fs = s.findings || [];
      const by = (lv) => fs.filter((f) => f.level === lv);
      const bad = by("bad"), warn = by("warn"), info = by("info"), ok = by("ok");
      const parts = [];
      if (bad.length) parts.push(`${bad.length} problem${bad.length > 1 ? "s" : ""}`);
      if (warn.length) parts.push(`${warn.length} warning${warn.length > 1 ? "s" : ""}`);
      if (info.length) parts.push(`${info.length} note${info.length > 1 ? "s" : ""}`);
      parts.push(`${ok.length} fine`);
      document.getElementById("dxSummary").textContent = parts.join(" · ");
      const section = (title, list) => list.length ? `<div class="section-title">${title}</div>${list.map(findingHTML).join("")}` : "";
      document.getElementById("dxList").innerHTML =
        section("Needs attention", bad.concat(warn)) + section("Good to know", info) + section("Checked and fine", ok);
    },
  };

  // ---------- Services ----------
  let svcState = { name: "", q: "", filter: "running", log: null };

  L.views.services = {
    title: "Services",
    key: () => "services",
    crumbs: (parts) => parts[0] ? [["Services", "#/services"], svc(parts[0])] : ["Services"],
    mount(el, parts) {
      el.innerHTML = `<div class="row2 fill" style="grid-template-columns:minmax(0,1fr) minmax(0,1.6fr)">
        <div class="panel">
          <div class="panel-head toolbar">
            <input class="input" id="svcQ" type="search" placeholder="Find a service" style="flex:1">
            <div class="chips" id="svcChips"><button data-v="running" class="on">Running</button><button data-v="failed">Failed</button><button data-v="all">All</button></div>
          </div>
          <div class="scroll" id="svcList"></div>
        </div>
        <div class="row2" style="grid-template-rows:auto minmax(0,1fr);grid-template-columns:minmax(0,1fr)" id="svcDetail"></div>
      </div>`;
      el.querySelector("#svcQ").addEventListener("input", (e) => { svcState.q = e.target.value.toLowerCase(); this.list(L.S.snap); });
      el.querySelector("#svcChips").addEventListener("click", (e) => {
        const b = e.target.closest("button");
        if (!b) return;
        svcState.filter = b.dataset.v;
        el.querySelectorAll("#svcChips button").forEach((x) => x.classList.toggle("on", x === b));
        this.list(L.S.snap);
      });
      this.route(parts);
    },
    route(parts) {
      const name = parts[0] || "";
      if (name === svcState.name && svcState.log) return;
      svcState.log?.close();
      svcState.log = null;
      svcState.name = name;
      const detail = document.getElementById("svcDetail");
      if (!name) {
        detail.innerHTML = `<div class="panel" style="grid-row:1/3"><div class="empty">Pick a service to see how it runs and its log.</div></div>`;
      } else {
        detail.innerHTML = `<div class="panel"><div class="panel-body head-strip" id="svcHead"></div></div><div class="logview" id="svcLog"></div>`;
        svcState.log = L.LogView(document.getElementById("svcLog"), {
          name: svc(name),
          stream: `api/services/${encodeURIComponent(name)}/logs/stream?tail=500`,
          older: (first) => `api/services/${encodeURIComponent(name)}/logs?tail=500&before=${encodeURIComponent(first.c)}`,
        });
      }
      if (L.S.snap) this.update(L.S.snap);
    },
    unmount() { svcState.log?.close(); svcState.log = null; svcState.name = ""; },
    list(s) {
      if (!s) return;
      let list = s.services;
      if (svcState.filter === "running") list = list.filter((v) => v.sub === "running");
      if (svcState.filter === "failed") list = list.filter((v) => v.active === "failed");
      if (svcState.q) list = list.filter((v) => (v.name + " " + v.description).toLowerCase().includes(svcState.q));
      list = [...list].sort((a, b) => (b.active === "failed") - (a.active === "failed") || (b.cpu - a.cpu) || (b.memory - a.memory) || a.name.localeCompare(b.name));
      document.getElementById("svcList").innerHTML = list.length ? `<table class="grid"><thead><tr><th>Service</th><th class="num">CPU</th><th class="num">Memory</th></tr></thead><tbody>${list.map((v) => {
        const [cls, word] = serviceState(v);
        return `<tr class="click ${v.name === svcState.name ? "on" : ""}" onclick="location.hash='#/services/${esc(v.name)}'">
          <td class="cut" title="${esc(v.description)}"><span class="dot ${cls}"></span> <b>${esc(svc(v.name))}</b> <span class="muted">${v.sub === "running" ? esc(v.description) : esc(word)}</span></td>
          <td class="num">${v.sub === "running" ? pct1(v.cpu) : ""}</td>
          <td class="num">${v.memory ? bytes(v.memory) : ""}</td></tr>`;
      }).join("")}</tbody></table>` : `<div class="empty">${svcState.filter === "failed" ? "No failed services." : "No service matches."}</div>`;
    },
    update(s) {
      this.list(s);
      const head = document.getElementById("svcHead");
      if (!head) return;
      const v = s.services.find((x) => x.name === svcState.name);
      if (!v) { head.innerHTML = `<span class="muted">${esc(svcState.name)} is not loaded.</span>`; return; }
      const [cls, word] = serviceState(v);
      head.innerHTML = `<div class="stat title"><b style="font-size:16px" title="">${esc(svc(v.name))}</b><span style="text-transform:none;letter-spacing:0">${esc(v.description)}</span></div>
        <span class="badge ${cls}">${esc(word)}</span>
        <div class="stat"><span>Running for</span><b>${v.sub === "running" ? since(v.since) || "–" : "–"}</b></div>
        <div class="stat"><span>CPU</span><b>${v.sub === "running" ? pct1(v.cpu) : "–"}</b></div>
        <div class="stat"><span>Memory</span><b>${v.memory ? bytes(v.memory) : "–"}</b></div>
        <div class="stat"><span>Restarts</span><b class="${v.restarts ? "warn" : ""}">${v.restarts}</b></div>
        <div class="stat"><span>PID</span><b class="mono">${v.pid || "–"}</b></div>
        <div class="stat"><span>Starts with server</span><b>${v.enabled === "enabled" ? "yes" : v.enabled === "static" ? "when needed" : v.enabled || "–"}</b></div>`;
    },
  };

  // ---------- Processes ----------
  let procState = { q: "", sort: "cpu" };
  L.views.processes = {
    title: "Processes",
    mount(el) {
      el.innerHTML = `<div class="panel fill">
        <div class="panel-head toolbar">
          <input class="input" id="procQ" type="search" placeholder="Find by name, command, user, container or service" style="width:380px">
          <div class="chips" id="procSort"><button data-v="cpu" class="${procState.sort === "cpu" ? "on" : ""}">Most CPU</button><button data-v="mem" class="${procState.sort === "mem" ? "on" : ""}">Most memory</button></div>
          <span class="grow"></span><span class="muted" id="procSummary"></span>
        </div>
        <div class="scroll" id="procList"></div>
      </div>`;
      el.querySelector("#procQ").value = procState.q;
      el.querySelector("#procQ").addEventListener("input", (e) => { procState.q = e.target.value.toLowerCase(); this.update(L.S.snap); });
      el.querySelector("#procSort").addEventListener("click", (e) => {
        const b = e.target.closest("button");
        if (!b) return;
        procState.sort = b.dataset.v;
        el.querySelectorAll("#procSort button").forEach((x) => x.classList.toggle("on", x === b));
        this.update(L.S.snap);
      });
    },
    update(s) {
      if (!s) return;
      const q = procState.q;
      let list = s.processes.filter((p) => !q || [p.name, p.cmd, p.user, p.ownerName, p.owner?.id, String(p.pid)].some((v) => v && v.toLowerCase().includes(q)));
      list.sort(procState.sort === "mem" ? (a, b) => b.rss - a.rss : (a, b) => (b.cpu - a.cpu) || (b.rss - a.rss));
      document.getElementById("procSummary").textContent = `the ${s.processes.length} busiest of ${s.load.tasks} · CPU is % of one core`;
      document.getElementById("procList").innerHTML = `<table class="grid"><thead><tr>
        <th class="num">PID</th><th>Program</th><th>Belongs to</th><th>User</th><th class="num">CPU</th><th class="num">Memory</th><th class="num">Threads</th><th>Command</th>
      </tr></thead><tbody>${list.length ? list.map((p) => `<tr>
        <td class="num mono">${p.pid}</td>
        <td><b>${esc(p.name)}</b>${p.state === "Z" ? ` <span class="badge bad">zombie</span>` : p.state === "D" ? ` <span class="badge warn" title="Waiting on the disk">waiting</span>` : ""}</td>
        <td>${ownerLabel(p)}</td>
        <td>${esc(p.user)}</td>
        <td class="num">${pct1(p.cpu)}</td>
        <td class="num">${bytes(p.rss)}</td>
        <td class="num">${p.threads}</td>
        <td class="cut mono muted" title="${esc(p.cmd)}">${esc(p.cmd || `[${p.name}]`)}</td>
      </tr>`).join("") : `<tr><td colspan="8" class="empty">No process matches.</td></tr>`}</tbody></table>`;
    },
  };
})();
