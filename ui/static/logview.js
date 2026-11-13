// The log viewer used for containers and services: live lines over Server-Sent
// Events, search, problems only, follow the newest line, and reading further back.
"use strict";

LinCos.LogView = function (host, opt) {
  const { esc } = LinCos;
  const MAX = 20000; // lines kept in the page
  const problem = /\b(fatal|panic|error|errors|exception|fail|failed|failure|crit|critical|denied|refused|unhandled)\b|^\s*fail:/i;
  const warning = /\b(warn|warning|deprecated|retry|retrying|timeout|timed out)\b|^\s*warn:/i;

  let lines = [];
  let es = null;
  let ended = false;
  const st = { q: "", problems: false, follow: true, wrap: false };

  host.innerHTML = `<div class="panel logview">
    <div class="panel-head toolbar">
      <input class="input q" type="search" placeholder="Search the log" style="flex:0 1 260px;min-width:110px">
      <label class="check"><input type="checkbox" class="problems"> Problems only</label>
      <label class="check" title="Keep showing the newest line"><input type="checkbox" class="follow" checked> Follow</label>
      <label class="check"><input type="checkbox" class="wrapit"> Wrap long lines</label>
      <span class="grow"></span>
      <span class="muted count"></span>${opt.extra || ""}
      <button class="btn small dl" type="button" title="Save the lines shown as a file">Download</button>
    </div>
    <div class="scroll loglines">
      <div class="logmore"><button class="btn small older" type="button">Load older lines</button> <span class="muted status"></span></div>
      <div class="lines"></div>
    </div>
  </div>`;
  const root = host.querySelector(".logview");
  const box = host.querySelector(".loglines");
  const out = host.querySelector(".lines");
  const status = host.querySelector(".status");

  const level = (l) => (l.s === "err" || problem.test(l.m) ? "err" : l.s === "warn" || warning.test(l.m) ? "warn" : "");
  const shown = (l) => (!st.problems || l.lv) && (!st.q || l.m.toLowerCase().includes(st.q));

  function mark(text) {
    if (!st.q) return esc(text);
    const low = text.toLowerCase();
    let i = 0, html = "";
    for (;;) {
      const j = low.indexOf(st.q, i);
      if (j < 0) break;
      html += esc(text.slice(i, j)) + "<mark>" + esc(text.slice(j, j + st.q.length)) + "</mark>";
      i = j + st.q.length;
    }
    return html + esc(text.slice(i));
  }

  const dayOf = (t) => (t ? new Date(t).toDateString() : "");
  function lineHTML(l, prevDay) {
    const d = l.t ? new Date(l.t) : null;
    const day = d ? d.toDateString() : "";
    let html = "";
    if (day && day !== prevDay) {
      html += `<div class="ll day">${d.toLocaleDateString([], { weekday: "long", day: "numeric", month: "long", year: "numeric" })}</div>`;
    }
    const time = d ? d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" }) : "";
    return html + `<div class="ll ${l.lv}"><span class="t">${time}</span><span class="m" title="">${mark(l.m)}</span></div>`;
  }

  function render() {
    const keep = box.scrollHeight - box.scrollTop;
    let html = "", prev = "", n = 0;
    for (const l of lines) {
      if (!shown(l)) continue;
      html += lineHTML(l, prev);
      prev = dayOf(l.t);
      n++;
    }
    out.innerHTML = html || `<div class="empty">${lines.length ? "No line matches." : ended ? "The log is empty." : "Waiting for lines…"}</div>`;
    count(n);
    if (st.follow) box.scrollTop = box.scrollHeight;
    else box.scrollTop = box.scrollHeight - keep;
  }

  function count(n) {
    host.querySelector(".count").textContent = st.q || st.problems ? `${n.toLocaleString()} of ${lines.length.toLocaleString()} lines` : `${lines.length.toLocaleString()} lines`;
  }

  // Stray line breaks at either end would show a line as two rows
  const clean = (l) => { l.m = String(l.m ?? "").replace(/^[\r\n]+|[\r\n]+$/g, ""); l.lv = level(l); return l; };

  function append(batch) {
    let prev = lines.length ? dayOf(lines[lines.length - 1].t) : "";
    let html = "";
    for (const l of batch) {
      clean(l);
      lines.push(l);
      if (shown(l)) {
        html += lineHTML(l, prev);
      }
      prev = dayOf(l.t);
    }
    if (lines.length > MAX) {
      lines = lines.slice(-MAX);
      return render();
    }
    if (out.querySelector(".empty")) out.innerHTML = "";
    out.insertAdjacentHTML("beforeend", html);
    count(lines.filter(shown).length);
    if (st.follow) box.scrollTop = box.scrollHeight;
  }

  function connect() {
    es = new EventSource(opt.stream);
    es.onopen = () => { status.textContent = ""; };
    es.onmessage = (e) => append(JSON.parse(e.data));
    es.addEventListener("failed", (e) => { status.textContent = "The log could not be read: " + JSON.parse(e.data); });
    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) status.textContent = "Disconnected.";
    };
    setTimeout(() => { ended = true; if (!lines.length) render(); }, 1500);
  }

  async function older() {
    if (!lines.length) return;
    const btn = host.querySelector(".older");
    btn.disabled = true;
    try {
      const batch = await LinCos.api(opt.older(lines[0]));
      // the line at the boundary can come back once more
      const first = lines[0];
      const fresh = batch.filter((l) => !(l.t === first.t && l.m === first.m));
      if (!fresh.length) {
        btn.textContent = "No older lines";
        return;
      }
      fresh.forEach(clean);
      // Reading back: stay where the reader is instead of jumping to the newest line
      st.follow = false;
      host.querySelector(".follow").checked = false;
      lines = fresh.concat(lines).slice(0, MAX);
      render();
    } catch (err) {
      LinCos.toast(err.message, true);
    } finally {
      btn.disabled = false;
    }
  }

  function download() {
    const text = lines.filter(shown).map((l) => `${l.t || ""} ${l.m}`).join("\n");
    const a = document.createElement("a");
    a.href = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
    a.download = `${opt.name || "log"}.log`;
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  }

  // controls
  let timer;
  host.querySelector(".q").addEventListener("input", (e) => {
    clearTimeout(timer);
    timer = setTimeout(() => { st.q = e.target.value.trim().toLowerCase(); render(); }, 150);
  });
  host.querySelector(".problems").addEventListener("change", (e) => { st.problems = e.target.checked; render(); });
  host.querySelector(".follow").addEventListener("change", (e) => { st.follow = e.target.checked; if (st.follow) box.scrollTop = box.scrollHeight; });
  host.querySelector(".wrapit").addEventListener("change", (e) => { st.wrap = e.target.checked; root.classList.toggle("wrap", st.wrap); if (st.follow) box.scrollTop = box.scrollHeight; });
  host.querySelector(".older").addEventListener("click", older);
  host.querySelector(".dl").addEventListener("click", download);
  // Scrolling up to read stops following; scrolling back to the end follows again
  box.addEventListener("scroll", () => {
    const atEnd = box.scrollHeight - box.scrollTop - box.clientHeight < 40;
    if (atEnd !== st.follow) {
      st.follow = atEnd;
      host.querySelector(".follow").checked = atEnd;
    }
  });

  render();
  connect();
  return { close: () => es?.close() };
};
