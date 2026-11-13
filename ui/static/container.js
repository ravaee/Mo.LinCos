// Containers: the list of all, and each container's page with its tabs
// Overview, Logs, Files (its app data) and Database (when it runs one).
"use strict";

(() => {
  const L = LinCos;
  const { esc, bytes, pct1, since, ago, when, short, bar, levelColor, containerState, api, toast, confirmDialog } = L;

  let C = null; // the open container page: { id, tab, info, log, files, db }

  const findLive = (s, id) => (s?.containers || []).find((c) => c.id.startsWith(id));

  L.views.containers = {
    title: "Containers",
    key: (parts) => parts[0] || "",
    crumbs(parts, s) {
      if (!parts[0]) return ["Containers"];
      const c = findLive(s, parts[0]);
      return [["Containers", "#/containers"], c ? c.name : parts[0]];
    },
    mount(el, parts) {
      if (!parts[0]) {
        C = null;
        el.innerHTML = `<div class="scroll" id="cList" style="display:flex;flex-direction:column;gap:18px"></div>`;
        return;
      }
      C = { id: parts[0], tab: "", info: null, log: null, files: { mount: 0, path: ".", entries: [], preview: null }, db: null };
      el.innerHTML = `<div class="panel"><div class="panel-body head-strip" id="cHead"></div></div>
        <nav class="tabs" id="cTabs"></nav>
        <div id="cTab" style="flex:1;min-height:0;display:flex;flex-direction:column;gap:14px"></div>`;
      api(`api/containers/${encodeURIComponent(parts[0])}`).then((info) => {
        if (!C || C.id !== parts[0]) return;
        C.info = info;
        this.route(parts);
      }).catch((err) => {
        document.getElementById("cTab").innerHTML = `<div class="panel"><div class="empty">This container is gone: ${esc(err.message)}</div></div>`;
      });
    },
    route(parts) {
      if (!C || !C.info) return;
      const tabs = ["overview", "logs", "files"].concat(C.info.database ? ["database"] : []);
      const tab = tabs.includes(parts[1]) ? parts[1] : "overview";
      const labels = { overview: "Overview", logs: "Logs", files: "Files", database: "Database" };
      document.getElementById("cTabs").innerHTML = tabs.map((t) =>
        `<a href="#/containers/${C.id}/${t}" class="${t === tab ? "on" : ""}">${labels[t]}${t === "database" ? ` <span class="muted">${esc(engineName(C.info.database.engine))}</span>` : ""}</a>`).join("");
      if (tab === C.tab) return;
      C.log?.close();
      C.log = null;
      C.tab = tab;
      const el = document.getElementById("cTab");
      if (tab === "overview") overviewTab(el);
      if (tab === "logs") {
        el.innerHTML = `<div class="logview" id="cLog"></div>`;
        C.log = L.LogView(document.getElementById("cLog"), {
          name: C.info.name,
          stream: `api/containers/${C.id}/logs/stream?tail=500`,
          older: (first) => `api/containers/${C.id}/logs?tail=500&until=${encodeURIComponent(first.t)}`,
        });
      }
      if (tab === "files") filesTab(el);
      if (tab === "database") dbTab(el);
    },
    unmount() { C?.log?.close(); C = null; },
    update(s) {
      if (!C) return listCards(s);
      const c = findLive(s, C.id);
      const head = document.getElementById("cHead");
      if (!head) return;
      if (!c) { head.innerHTML = `<span class="muted">This container is gone.</span>`; return; }
      const [cls, word] = containerState(c);
      const memPct = c.memLimit ? c.memory / c.memLimit * 100 : 0;
      head.innerHTML = `<div class="stat title"><b style="font-size:16px" title="">${esc(c.name)}</b><span style="text-transform:none;letter-spacing:0" class="mono">${esc(c.image)}</span></div>
        <span class="badge ${cls}">${esc(word)}</span>
        <div class="stat"><span>${c.state === "running" ? "Running for" : "State"}</span><b>${c.state === "running" ? since(c.startedAt) : esc(c.status)}</b></div>
        <div class="stat"><span>CPU</span><b>${c.state === "running" ? pct1(c.cpu) : "–"}</b></div>
        <div class="stat" style="min-width:150px"><span>Memory</span><b>${c.state === "running" ? `${bytes(c.memory)}${c.memLimit ? ` <span class="muted" style="font-weight:400">of ${bytes(c.memLimit, 0)}</span>` : ""}` : "–"}</b>${c.memLimit && c.state === "running" ? bar([[memPct, levelColor(memPct, 80, 90)]]) : ""}</div>
        <div class="stat"><span>Restarts</span><b class="${c.restarts ? "warn" : ""}">${c.restarts}</b></div>
        <div class="stat"><span>Programs</span><b>${c.state === "running" ? c.pids : "–"}</b></div>
        <div class="stat"><span>ID</span><b class="mono" style="cursor:copy" title="Click to copy the full ID" onclick="navigator.clipboard?.writeText('${c.id}');LinCos.toast('ID copied')">${short(c.id)}</b></div>`;
    },
  };

  const engineName = (e) => ({ sqlserver: "SQL Server", postgres: "PostgreSQL", mysql: "MySQL" })[e] || e;

  // ---------- the list ----------
  function listCards(s) {
    const el = document.getElementById("cList");
    if (!el) return;
    const ctrs = s.containers || [];
    if (!ctrs.length) { el.innerHTML = `<div class="panel"><div class="empty">No containers, or Docker is not installed.</div></div>`; return; }
    const groups = new Map();
    for (const c of ctrs) {
      const k = c.project || "";
      if (!groups.has(k)) groups.set(k, []);
      groups.get(k).push(c);
    }
    el.innerHTML = [...groups].sort(([a], [b]) => (a === "") - (b === "") || a.localeCompare(b)).map(([project, cs]) => `<div class="stack">
      <div class="stack-title">${esc(project || "Not in a compose stack")}</div>
      <div class="cards">${cs.map((c) => {
        const [cls, word] = containerState(c);
        const memPct = c.memLimit ? c.memory / c.memLimit * 100 : 0;
        const up = c.state === "running";
        return `<div class="ccard" onclick="if(!event.target.closest('a'))location.hash='#/containers/${short(c.id)}'">
          <div class="name"><span class="dot ${cls}"></span><span>${esc(c.name)}</span><span class="badge ${cls}" style="margin-left:auto">${esc(word)}</span></div>
          <div class="line"><span>Image</span><b class="mono">${esc(c.image)}</b></div>
          <div class="line"><span>${up ? "Running for" : "State"}</span><b>${up ? since(c.startedAt) : esc(c.status)}</b></div>
          <div class="line"><span>CPU · Memory</span><b>${up ? `${pct1(c.cpu)} · ${bytes(c.memory)}${c.memLimit ? ` of ${bytes(c.memLimit, 0)}` : ""}` : "–"}</b></div>
          ${up && c.memLimit ? bar([[memPct, levelColor(memPct, 80, 90)]]) : ""}
          <div class="links">
            <a class="btn small" href="#/containers/${short(c.id)}/logs">Logs</a>
            <a class="btn small" href="#/containers/${short(c.id)}/files">Files${c.mounts.length ? ` (${c.mounts.length})` : ""}</a>
            ${isDB(c.image) ? `<a class="btn small" href="#/containers/${short(c.id)}/database">Database</a>` : ""}
          </div>
        </div>`;
      }).join("")}</div></div>`).join("");
  }
  const isDB = (img) => /mssql|sqlserver|postgres|postgis|timescale|mysql|mariadb|percona/i.test(img || "");

  // ---------- Overview tab ----------
  function overviewTab(el) {
    const i = C.info;
    const kv = (rows) => `<dl class="kv">${rows.filter((r) => r && r[1] !== "" && r[1] !== undefined && r[1] !== null).map(([k, v]) => `<dt>${k}</dt><dd>${v}</dd>`).join("")}</dl>`;
    const restartWords = { "unless-stopped": "always, unless you stop it", always: "always", "on-failure": "only after a crash", no: "never", "": "never" };
    el.innerHTML = `<div class="facts">
      <div class="panel"><div class="panel-head"><h2>How it runs</h2></div><div class="scroll panel-body">${kv([
        ["State", esc(i.status) + (i.exitCode && i.status !== "running" ? ` · error code ${i.exitCode}` : "")],
        i.status === "running" ? ["Started", `${esc(when(i.startedAt))} <span class="muted">(${esc(ago(i.startedAt))})</span>`] : ["Stopped", esc(when(i.finishedAt))],
        ["Health", i.health ? `<span class="badge ${i.health === "healthy" ? "good" : i.health === "unhealthy" ? "bad" : "warn"}">${esc(i.health)}</span>` : `<span class="muted">no health check</span>`],
        i.healthLog ? ["Last check said", `<pre class="out">${esc(i.healthLog)}</pre>`] : null,
        ["Restarts", `${i.restarts} <span class="muted">· Docker restarts it ${esc(restartWords[i.restartPolicy] ?? i.restartPolicy)}</span>`],
        i.oomKilled ? ["Memory", `<span class="bad">It was stopped for using too much memory</span>`] : null,
        ["Memory limit", i.memLimit ? bytes(i.memLimit) : "none"],
        i.error ? ["Error", `<span class="bad">${esc(i.error)}</span>`] : null,
      ])}</div></div>
      <div class="panel"><div class="panel-head"><h2>What it is</h2></div><div class="scroll panel-body">${kv([
        ["Image", `<span class="mono">${esc(i.image)}</span>`],
        ["Command", `<span class="mono">${esc(i.command)}</span>`],
        ["Working folder", i.workingDir ? `<span class="mono">${esc(i.workingDir)}</span>` : ""],
        ["Runs as", esc(i.user || "root")],
        ["Created", esc(when(i.created))],
        i.project ? ["Compose stack", `${esc(i.project)} <span class="muted">· service ${esc(i.service)}</span>`] : null,
        i.composeDir ? ["Compose folder", `<span class="mono">${esc(i.composeDir)}</span>`] : null,
        ["Settings", `<div class="chipset">${i.envNames.map((n) => `<span>${esc(n)}</span>`).join("")}</div><div class="muted" style="margin-top:4px">Only the names: values can hold passwords.</div>`],
      ])}</div></div>
      <div class="panel"><div class="panel-head"><h2>Network and storage</h2></div><div class="scroll panel-body">${kv([
        ["Address", i.ip ? `<span class="mono">${esc(i.ip)}</span>` : "none"],
        ["Networks", esc(i.networks.join(", ")) || "none"],
        ["Ports", i.ports.length ? i.ports.map((p) => `<div class="mono">${esc(p)}</div>`).join("") : `<span class="muted">none</span>`],
        ["Folders", i.mounts.length ? i.mounts.map((m, n) => `<div style="margin-bottom:6px"><a href="#/containers/${C.id}/files" onclick="LinCos.openMount(${n})"><b class="mono">${esc(m.Destination)}</b></a> ${m.RW ? "" : `<span class="badge">read-only</span>`}<div class="mono muted">${esc(m.Source)}</div></div>`).join("") : `<span class="muted">none</span>`],
        ["Log", `${esc(i.logDriver)}${i.logPath ? `<div class="mono muted">${esc(i.logPath)}</div>` : ""}`],
      ])}</div></div>
    </div>`;
  }
  L.openMount = (n) => { if (C) { C.files.mount = n; C.files.path = "."; C.files.preview = null; } };

  // ---------- Files tab ----------
  function filesTab(el) {
    const i = C.info;
    if (!i.mounts.length) {
      el.innerHTML = `<div class="panel"><div class="empty">This container has no folders or volumes bound to it, so its data lives only inside it and is lost when it is removed.</div></div>`;
      return;
    }
    el.innerHTML = `<div class="files" id="fGrid">
      <div class="panel"><div class="panel-head"><h2>Folders</h2></div><div class="scroll" id="fMounts"></div></div>
      <div class="panel" id="fPanel">
        <div class="panel-head toolbar"><div class="pathbar" id="fPath"></div>
          <span class="muted" id="fCount"></span>
          <label class="btn small" title="Or drop files onto the list">Upload<input type="file" multiple hidden id="fUpload"></label>
        </div>
        <div class="scroll" id="fList"></div>
      </div>
      <div class="panel" id="fPreview" hidden></div>
    </div>`;
    document.getElementById("fUpload").addEventListener("change", (e) => { upload([...e.target.files]); e.target.value = ""; });
    const panel = document.getElementById("fPanel");
    panel.addEventListener("dragover", (e) => { e.preventDefault(); panel.classList.add("dropping"); });
    panel.addEventListener("dragleave", () => panel.classList.remove("dropping"));
    panel.addEventListener("drop", (e) => { e.preventDefault(); panel.classList.remove("dropping"); upload([...e.dataTransfer.files]); });
    renderMounts();
    listFolder();
  }

  function renderMounts() {
    document.getElementById("fMounts").innerHTML = C.info.mounts.map((m, n) => `<div class="mount ${n === C.files.mount ? "on" : ""}" data-m="${n}">
      <span class="dest">${esc(m.Destination)} ${m.RW ? "" : `<span class="badge">read-only inside</span>`}</span>
      <span class="src">${esc(m.Source)}</span>
      <span class="muted">${m.Type === "volume" ? `Docker volume ${esc(m.Name || "")}` : "Folder on the server"}</span>
    </div>`).join("");
    document.querySelectorAll("#fMounts .mount").forEach((x) => x.addEventListener("click", () => {
      C.files.mount = +x.dataset.m; C.files.path = "."; C.files.preview = null;
      renderMounts(); listFolder();
    }));
  }

  // The open mount, named by its path inside the container
  const md = () => encodeURIComponent(C.info.mounts[C.files.mount].Destination);
  const fq = (extra = "") => `mount=${md()}&path=${encodeURIComponent(C.files.path)}${extra}`;
  const join = (dir, name) => (dir === "." ? name : `${dir}/${name}`);

  async function listFolder() {
    const f = C.files;
    const m = C.info.mounts[f.mount];
    const parts = f.path === "." ? [] : f.path.split("/");
    document.getElementById("fPath").innerHTML = `<a href="#" data-p=".">${esc(m.Destination)}</a>` +
      parts.map((p, n) => ` / <a href="#" data-p="${esc(parts.slice(0, n + 1).join("/"))}">${esc(p)}</a>`).join("");
    document.querySelectorAll("#fPath a").forEach((a) => a.addEventListener("click", (e) => { e.preventDefault(); f.path = a.dataset.p; f.preview = null; listFolder(); }));
    showPreview();
    const list = document.getElementById("fList");
    try {
      const res = await api(`api/containers/${C.id}/files?${fq()}`);
      f.entries = res.entries;
      document.getElementById("fCount").textContent = `${res.entries.length.toLocaleString()} item${res.entries.length === 1 ? "" : "s"}${res.truncated ? " (first 5,000)" : ""}`;
      list.innerHTML = res.entries.length ? `<table class="grid"><thead><tr><th>Name</th><th class="num">Size</th><th>Changed</th><th></th></tr></thead><tbody>${res.entries.map((e, n) => `<tr class="click ${f.preview === e.name ? "on" : ""}" data-n="${n}">
        <td class="cut"><span class="fname"><span class="ic">${e.dir ? "▸" : "·"}</span><span>${esc(e.name)}${e.link ? ` <span class="muted">(link)</span>` : ""}</span></span></td>
        <td class="num">${e.dir ? "" : bytes(e.size)}</td>
        <td title="${esc(new Date(e.mod).toLocaleString())}">${esc(when(e.mod))}</td>
        <td class="num">${e.dir ? "" : `<a class="btn small" href="api/containers/${C.id}/files/raw?mount=${md()}&path=${encodeURIComponent(join(f.path, e.name))}&download=1" title="Download">↓</a>`}
          <button class="btn small danger" data-del="${n}" title="Delete">Delete</button></td>
      </tr>`).join("")}</tbody></table>` : `<div class="empty">This folder is empty. Drop files here or use Upload.</div>`;
      list.querySelectorAll("tr[data-n]").forEach((tr) => tr.addEventListener("click", (ev) => {
        if (ev.target.closest("a,button")) return;
        const e = f.entries[+tr.dataset.n];
        if (e.dir) { f.path = join(f.path, e.name); f.preview = null; listFolder(); }
        else { f.preview = e.name; list.querySelectorAll("tr").forEach((x) => x.classList.toggle("on", x === tr)); showPreview(); }
      }));
      list.querySelectorAll("[data-del]").forEach((b) => b.addEventListener("click", () => remove(f.entries[+b.dataset.del])));
    } catch (err) {
      list.innerHTML = `<div class="empty">Could not open this folder: ${esc(err.message)}</div>`;
    }
  }

  const textExt = /\.(txt|log|md|json|xml|ya?ml|csv|tsv|html?|css|m?js|ts|cs|go|py|rb|php|java|sh|conf|cfg|ini|env|sql|toml|properties|caddyfile|dockerfile|gitignore)$/i;
  const imgExt = /\.(png|jpe?g|gif|webp|svg|ico|bmp|avif)$/i;

  async function showPreview() {
    const f = C.files;
    const grid = document.getElementById("fGrid");
    const box = document.getElementById("fPreview");
    if (!f.preview) { box.hidden = true; grid.classList.remove("preview"); return; }
    const e = f.entries.find((x) => x.name === f.preview);
    box.hidden = false;
    grid.classList.add("preview");
    const url = `api/containers/${C.id}/files/raw?mount=${md()}&path=${encodeURIComponent(join(f.path, f.preview))}`;
    box.innerHTML = `<div class="panel-head"><h2 style="overflow:hidden;text-overflow:ellipsis">${esc(f.preview)}</h2>
      <span class="muted">${e ? bytes(e.size) : ""}</span><a class="btn small" href="${url}&download=1" style="margin-left:auto">Download</a>
      <button class="btn small" id="fClose" title="Close">✕</button></div><div class="scroll preview-body" id="fBody"><div class="empty">Loading…</div></div>`;
    document.getElementById("fClose").onclick = () => { f.preview = null; document.querySelectorAll("#fList tr.on").forEach((x) => x.classList.remove("on")); showPreview(); };
    const body = document.getElementById("fBody");
    if (imgExt.test(f.preview)) { body.innerHTML = `<img src="${url}" alt="">`; return; }
    if (/\.pdf$/i.test(f.preview)) { body.innerHTML = `<div class="empty"><a class="btn" href="${url}" target="_blank" rel="noopener">Open the PDF</a></div>`; return; }
    if (e && e.size > 2 << 20 && !textExt.test(f.preview)) { body.innerHTML = `<div class="empty">No preview for a file this size. Download it instead.</div>`; return; }
    try {
      const res = await fetch(url, { headers: e && e.size > 1 << 20 ? { Range: "bytes=0-1048575" } : {} });
      const buf = new Uint8Array(await res.arrayBuffer());
      if (buf.slice(0, 8000).includes(0)) { body.innerHTML = `<div class="empty">This is not a text file, so there is no preview. Download it instead.</div>`; return; }
      const text = new TextDecoder().decode(buf);
      body.innerHTML = `<pre></pre>${e && e.size > buf.length ? `<div class="empty">Showing the first ${bytes(buf.length)} of ${bytes(e.size)}.</div>` : ""}`;
      body.querySelector("pre").textContent = text;
    } catch (err) {
      body.innerHTML = `<div class="empty">Could not read it: ${esc(err.message)}</div>`;
    }
  }

  async function remove(e) {
    const f = C.files;
    const m = C.info.mounts[f.mount];
    const rel = join(f.path, e.name);
    let what = e.dir ? "this folder and everything in it" : `this file (${bytes(e.size)})`;
    if (e.dir) {
      try {
        const u = await api(`api/containers/${C.id}/files/usage?mount=${md()}&path=${encodeURIComponent(rel)}`);
        what = `this folder with ${u.more ? "more than " : ""}${u.files.toLocaleString()} file${u.files === 1 ? "" : "s"} (${bytes(u.bytes)})`;
      } catch { /* the confirm still names it */ }
    }
    const ok = await confirmDialog({
      title: `Delete ${e.name}?`,
      text: `This deletes ${esc(what)} from the server for good. It can't be undone.`,
      pre: `${m.Source.replace(/\/$/, "")}/${rel}\n(${m.Destination}/${rel} inside ${C.info.name})`,
      ok: "Delete", danger: true,
    });
    if (!ok) return;
    try {
      await api(`api/containers/${C.id}/files/delete?mount=${md()}&path=${encodeURIComponent(rel)}`, { method: "POST" });
      toast(`Deleted ${e.name}`);
      if (f.preview === e.name) f.preview = null;
      listFolder();
    } catch (err) {
      toast(`Could not delete ${e.name}: ${err.message}`, true);
    }
  }

  async function upload(list) {
    if (!list.length) return;
    const f = C.files;
    const m = C.info.mounts[f.mount];
    const existing = new Set(f.entries.map((e) => e.name));
    const clash = list.filter((x) => existing.has(x.name)).map((x) => x.name);
    const total = list.reduce((a, x) => a + x.size, 0);
    const ok = await confirmDialog({
      title: `Upload ${list.length} file${list.length > 1 ? "s" : ""} (${bytes(total)})?`,
      text: clash.length ? `<span class="warn">${clash.length} already exist${clash.length === 1 ? "s" : ""} and will be replaced: ${esc(clash.join(", "))}</span>` : "",
      pre: `into ${m.Source.replace(/\/$/, "")}/${f.path === "." ? "" : f.path}\n${list.map((x) => x.name).join("\n")}`,
      ok: clash.length ? "Upload and replace" : "Upload",
      danger: clash.length > 0,
    });
    if (!ok) return;
    const form = new FormData();
    list.forEach((x) => form.append("file", x, x.name));
    try {
      const res = await api(`api/containers/${C.id}/files/upload?${fq(clash.length ? "&replace=1" : "")}`, { method: "POST", body: form });
      toast(`Uploaded ${res.saved.length} file${res.saved.length === 1 ? "" : "s"}`);
      listFolder();
    } catch (err) {
      toast(`Upload failed: ${err.message}`, true);
    }
  }

  // ---------- Database tab ----------
  function dbTab(el) {
    const t = C.info.database;
    C.db = C.db || { dbs: [], db: "", tables: [], table: null, offset: 0, filter: "", login: null, sql: "" };
    el.innerHTML = `<div class="db">
      <div class="panel">
        <div class="panel-head toolbar"><select class="input" id="dbSel" style="flex:1"></select></div>
        <div class="panel-head" style="padding:6px 14px"><input class="input" id="dbFilter" type="search" placeholder="Find a table" style="flex:1"></div>
        <div class="scroll tlist" id="dbTables"><div class="empty">Connecting…</div></div>
      </div>
      <div class="panel" id="dbMain">
        <textarea class="sql" id="dbSQL" spellcheck="false" placeholder="Pick a table, or write SQL and press Run (Ctrl+Enter)"></textarea>
        <div class="panel-head toolbar" style="border-top:0">
          <button class="btn primary" id="dbRun">Run</button>
          <span class="muted" id="dbInfo"></span><span class="grow"></span>
          <span id="dbPager"></span>
        </div>
        <div class="results-wrap" id="dbResults"></div>
      </div>
    </div>`;
    const sql = document.getElementById("dbSQL");
    sql.value = C.db.sql;
    sql.addEventListener("input", () => { C.db.sql = sql.value; });
    sql.addEventListener("keydown", (e) => { if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); runSQL(); } });
    document.getElementById("dbRun").addEventListener("click", () => runSQL());
    document.getElementById("dbSel").addEventListener("change", (e) => { C.db.db = e.target.value; C.db.table = null; loadTables(); });
    document.getElementById("dbFilter").addEventListener("input", (e) => { C.db.filter = e.target.value.toLowerCase(); renderTables(); });
    if (t.needsLogin && !C.db.login) return askLogin();
    loadDatabases();
  }

  function askLogin() {
    document.getElementById("dbTables").innerHTML = `<form class="login" id="dbLogin">
      <b>Sign in to ${esc(engineName(C.info.database.engine))}</b>
      <span class="muted">The container's settings hold no password. What you type is used only while this page is open.</span>
      <input class="input" name="user" placeholder="User" value="${esc(C.info.database.user)}">
      <input class="input" name="password" type="password" placeholder="Password" autocomplete="off">
      <button class="btn primary">Sign in</button></form>`;
    document.getElementById("dbLogin").addEventListener("submit", (e) => {
      e.preventDefault();
      const fd = new FormData(e.target);
      C.db.login = { user: fd.get("user"), password: fd.get("password") };
      loadDatabases();
    });
  }

  const body = (extra) => Object.assign({}, C.db.login || {}, { db: C.db.db }, extra);

  async function loadDatabases() {
    try {
      const res = await api(`api/containers/${C.id}/db/databases`, { method: "POST", json: body({}) });
      C.db.dbs = res.databases;
      if (!C.db.db) C.db.db = res.databases.includes(res.default) && res.default !== "master" ? res.default : res.databases[0] || "";
      document.getElementById("dbSel").innerHTML = res.databases.map((d) => `<option ${d === C.db.db ? "selected" : ""}>${esc(d)}</option>`).join("");
      loadTables();
    } catch (err) {
      document.getElementById("dbTables").innerHTML = `<div class="empty">Could not connect: ${esc(err.message)}</div>`;
      if (/login|password|auth/i.test(err.message)) { C.db.login = null; askLogin(); }
    }
  }

  async function loadTables() {
    document.getElementById("dbTables").innerHTML = `<div class="empty">Loading…</div>`;
    try {
      C.db.tables = await api(`api/containers/${C.id}/db/tables`, { method: "POST", json: body({}) });
      renderTables();
      if (C.db.table) browse(C.db.table, C.db.offset);
    } catch (err) {
      document.getElementById("dbTables").innerHTML = `<div class="empty">${esc(err.message)}</div>`;
    }
  }

  function renderTables() {
    const list = C.db.tables.filter((t) => !C.db.filter || (t.schema + "." + t.name).toLowerCase().includes(C.db.filter));
    const box = document.getElementById("dbTables");
    box.innerHTML = list.length ? list.map((t, n) => `<div class="t ${C.db.table && C.db.table.name === t.name && C.db.table.schema === t.schema ? "on" : ""}" data-n="${n}" title="${esc(t.schema)}.${esc(t.name)}">
      <span>${t.schema && !["dbo", "public"].includes(t.schema) && t.schema !== C.db.db ? `<span class="muted">${esc(t.schema)}.</span>` : ""}${esc(t.name)}</span>
      <span class="muted">${t.rows.toLocaleString()}</span></div>`).join("") : `<div class="empty">${C.db.tables.length ? "No table matches." : "No tables in this database."}</div>`;
    box.querySelectorAll(".t").forEach((x) => x.addEventListener("click", () => { browse(list[+x.dataset.n], 0); renderTables(); }));
  }

  function selectFor(t) {
    const e = C.info.database.engine;
    if (e === "sqlserver") return `SELECT TOP 100 * FROM [${t.schema}].[${t.name}]`;
    if (e === "postgres") return `SELECT * FROM "${t.schema}"."${t.name}" LIMIT 100`;
    return `SELECT * FROM \`${t.schema}\`.\`${t.name}\` LIMIT 100`;
  }

  async function browse(t, offset) {
    C.db.table = t;
    C.db.offset = offset;
    C.db.sql = selectFor(t);
    document.getElementById("dbSQL").value = C.db.sql;
    info("Loading…");
    try {
      const res = await api(`api/containers/${C.id}/db/rows`, { method: "POST", json: body({ schema: t.schema, table: t.name, offset, limit: 100 }) });
      showResult(res);
      const last = offset + res.rows.length;
      info(`rows ${res.rows.length ? (offset + 1).toLocaleString() : 0}–${last.toLocaleString()} of about ${t.rows.toLocaleString()} · ${res.ms.toFixed(0)} ms`);
      document.getElementById("dbPager").innerHTML = `<button class="btn small" id="dbPrev" ${offset ? "" : "disabled"}>‹ Previous 100</button> <button class="btn small" id="dbNext" ${res.rows.length < 100 ? "disabled" : ""}>Next 100 ›</button>`;
      document.getElementById("dbPrev").onclick = () => browse(t, Math.max(0, offset - 100));
      document.getElementById("dbNext").onclick = () => browse(t, offset + 100);
    } catch (err) {
      info("");
      showError(err);
    }
  }

  async function runSQL(confirmed = false) {
    const sql = document.getElementById("dbSQL").value.trim();
    if (!sql) return;
    document.getElementById("dbPager").innerHTML = "";
    info("Running…");
    try {
      const res = await api(`api/containers/${C.id}/db/query`, { method: "POST", json: body({ sql, confirm: confirmed }) });
      if (res.changed) {
        info(`${res.affected.toLocaleString()} row${res.affected === 1 ? "" : "s"} changed · ${res.ms.toFixed(0)} ms`);
        document.getElementById("dbResults").innerHTML = `<div class="empty">Done: ${res.affected.toLocaleString()} row${res.affected === 1 ? "" : "s"} changed.</div>`;
        toast(`${res.affected} row${res.affected === 1 ? "" : "s"} changed`);
        return;
      }
      showResult(res);
      info(`${res.rows.length.toLocaleString()} row${res.rows.length === 1 ? "" : "s"}${res.truncated ? " (the first 1,000)" : ""} · ${res.ms.toFixed(0)} ms`);
    } catch (err) {
      if (err.status === 409 && err.data?.needsConfirm) {
        info("");
        const ok = await confirmDialog({
          title: "Run SQL that changes data?",
          text: `This can change or delete data in <b>${esc(C.db.db)}</b> on ${esc(C.info.name)}. It can't be undone.`,
          pre: sql, ok: "Run it", danger: true,
        });
        if (ok) return runSQL(true);
        return;
      }
      info("");
      showError(err);
    }
  }

  const info = (t) => { document.getElementById("dbInfo").textContent = t; };
  const showError = (err) => { document.getElementById("dbResults").innerHTML = `<div class="empty bad">${esc(err.message)}</div>`; };

  function showResult(res) {
    const box = document.getElementById("dbResults");
    if (!res.columns.length) { box.innerHTML = `<div class="empty">No result.</div>`; return; }
    box.innerHTML = `<table class="grid results"><thead><tr>${res.columns.map((c) => `<th>${esc(c)}</th>`).join("")}</tr></thead><tbody>${
      res.rows.length ? res.rows.map((r) => `<tr>${r.map((v) => v === null ? `<td class="null">NULL</td>` : `<td title="${esc(String(v).slice(0, 2000))}">${esc(v)}</td>`).join("")}</tr>`).join("")
        : `<tr><td colspan="${res.columns.length}" class="empty">No rows.</td></tr>`}</tbody></table>`;
    box.scrollTop = 0;
  }
})();
