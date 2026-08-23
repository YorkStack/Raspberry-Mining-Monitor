// Raspberry Mining Monitor dashboard.
//
// The backend pushes one document per second over SSE. This file does nothing
// but format that document into the DOM: no framework, no build step, and
// nothing loaded from outside the binary.
//
// Panels, brackets, glow and icons are drawn by CSS and inline SVG rather than
// baked into a background image, so they can take on a miner's state: dimmed
// when the data is stale, red when a threshold is crossed, and however many
// tiles the fleet actually has.

const EM_DASH = "—";

/* ---------------- formatting ---------------- */

const has = (v) => v !== null && v !== undefined && !Number.isNaN(v);

const fmt = (v, digits = 2) => (has(v) ? v.toFixed(digits) : EM_DASH);
const int = (v) => (has(v) ? Math.round(v).toLocaleString("en-US") : EM_DASH);

// Difficulty and share values span many orders of magnitude, so they get an SI
// suffix rather than an exponent nobody can read at a glance.
function si(v, digits = 2) {
  if (!has(v) || !Number.isFinite(v)) return EM_DASH;
  const units = [[1e18, "E"], [1e15, "P"], [1e12, "T"], [1e9, "G"], [1e6, "M"], [1e3, "K"]];
  for (const [scale, suffix] of units) {
    if (Math.abs(v) >= scale) return (v / scale).toFixed(digits) + " " + suffix;
  }
  return v.toFixed(digits) + " ";
}

function duration(seconds) {
  if (!has(seconds) || !Number.isFinite(seconds)) return EM_DASH;
  const s = Math.max(0, Math.floor(seconds));
  if (s < 60) return s + " s";
  const m = Math.floor(s / 60);
  if (m < 60) return m + "m " + String(s % 60).padStart(2, "0") + "s";
  const h = Math.floor(m / 60);
  if (h < 24) return h + "h " + String(m % 60).padStart(2, "0") + "m";
  return Math.floor(h / 24) + "d " + (h % 24) + "h";
}

// Solo probabilities run to 1e-6 percent and smaller. Three significant digits
// keeps the magnitude visible instead of flattening everything to "< 0.0001".
function percent(p) {
  if (!has(p) || !Number.isFinite(p)) return EM_DASH;
  const pct = p * 100;
  if (pct === 0) return "0 %";
  if (pct < 1) return pct.toPrecision(3) + " %";
  return pct.toFixed(2) + " %";
}

// Whole-euro BTC price with thousands separators, e.g. "65.515 €".
function euro(v) {
  if (!has(v) || !Number.isFinite(v) || v <= 0) return EM_DASH;
  return Math.round(v).toLocaleString("de-DE") + " €";
}

function years(y) {
  if (!has(y) || !Number.isFinite(y)) return EM_DASH;
  return y < 1 ? Math.round(y * 365.25) + " d" : int(y) + " y";
}

const el = (id) => document.getElementById(id);
const setText = (id, text) => { const n = el(id); if (n) n.textContent = text; };

function setClass(id, base, extra) {
  const n = el(id);
  if (n) n.className = extra ? base + " " + extra : base;
}

/* ---------------- tile construction ---------------- */

function badge(id, icon, unit) {
  return `<span class="badge" id="${id}">
    ${icon ? `<svg class="bi"><use href="#${icon}"/></svg>` : ""}
    <span class="bv" id="${id}-v">${EM_DASH}</span><span class="bu">${unit}</span>
  </span>`;
}

// Device-specific mark by miner name. NerdOctaxe gets the chip array, a Mac
// gets the notebook, everything else the single-core reactor.
function getMinerIcon(name) {
  const n = (name || "").toLowerCase();
  if (n.includes("octa") || n.includes("nerd") || n.includes("axe")) return "i-chip-matrix";
  if (n.includes("mac") || n.includes("m2") || n.includes("metal") || n.includes("apple")) return "i-linechart";
  return "i-reactor-core";
}

function minerTile(i, name) {
  const id = "m" + i;
  return `
    <svg class="tile-mark"><use href="#${getMinerIcon(name)}"/></svg>
    <div class="tile-head">
      <span class="tile-name"><span class="tile-idx">MINER ${String(i + 1).padStart(2, "0")} //</span>
        <span id="${id}-name">${EM_DASH}</span></span>
      <span class="tile-state"><i class="dot dot-off" id="${id}-dot"></i><span id="${id}-state"></span></span>
    </div>
    <div class="tile-primary"><span class="v" id="${id}-hash">${EM_DASH}</span><span class="u">TH/s</span></div>
    <div class="badges">
      ${badge(id + "-temp", "i-thermo", "°C")}
      ${badge(id + "-power", "i-bolt", "W")}
      ${badge(id + "-eff", "i-eff", "J/TH")}
      ${badge(id + "-rpm", "i-fan", "RPM")}
    </div>
    <div class="tile-foot" id="${id}-foot"></div>`;
}

function totalTile() {
  return `
    <div class="eq" aria-hidden="true"><i></i><i></i><i></i><i></i><i></i><i></i><i></i></div>
    <div class="tile-head">
      <span class="tile-name"><span class="tile-idx">FLEET TOTAL //</span> SUMMARY</span>
    </div>
    <div class="tile-primary"><span class="v" id="mt-hash">${EM_DASH}</span><span class="u">TH/s</span></div>
    <div class="badges">
      ${badge("mt-power", "i-bolt", "W")}
      ${badge("mt-eff", "i-eff", "J/TH")}
      ${badge("mt-online", null, "ONLINE")}
    </div>
    <div class="tile-foot" id="mt-foot"></div>`;
}

let builtSig = "";

function buildTiles(miners) {
  const host = el("tiles");
  host.innerHTML = "";
  host.style.setProperty("--cols", String(miners.length + 1));

  for (let i = 0; i < miners.length; i++) {
    const d = document.createElement("div");
    d.className = "tile";
    d.id = "m" + i;
    d.innerHTML = minerTile(i, miners[i].name);
    host.appendChild(d);
  }
  const t = document.createElement("div");
  t.className = "tile tile-total";
  t.id = "mt";
  t.innerHTML = totalTile();
  host.appendChild(t);

  builtSig = miners.map((m) => m.name).join("|");
}

/* ---------------- rendering ---------------- */

function setBadge(id, value, status) {
  setText(id + "-v", value);
  setClass(id, "badge", status && status !== "ok" ? "is-" + status : "");
}

function renderMiner(i, m) {
  const id = "m" + i;
  const tile = el(id);
  tile.classList.toggle("is-stale", !!m.stale);
  tile.classList.toggle("is-offline", !m.online);
  // Tint the animated mark to the ASIC thermal state (only while online).
  tile.classList.toggle("mark-warn", m.online && m.asicTempStatus === "warn");
  tile.classList.toggle("mark-crit", m.online && m.asicTempStatus === "crit");

  setText(id + "-name", m.name.toUpperCase());
  setClass(id + "-dot", "dot", m.online ? "dot-on" : "dot-off");
  setText(id + "-state", m.online ? "" : m.hasData ? duration(m.ageSeconds).toUpperCase() + " AGO" : "NO DATA");

  setText(id + "-hash", m.hasData ? fmt(m.hashrateThs) : EM_DASH);

  setBadge(id + "-temp", has(m.asicTempC) ? fmt(m.asicTempC, 0) : EM_DASH, m.asicTempStatus);
  setBadge(id + "-power", has(m.powerW) ? fmt(m.powerW, 0) : EM_DASH);
  setBadge(id + "-eff", has(m.efficiencyJth) ? fmt(m.efficiencyJth, 1) : EM_DASH);
  setBadge(id + "-rpm", has(m.fanRpm) ? String(Math.round(m.fanRpm)) : EM_DASH);

  let foot;
  if (!m.online && m.err) {
    foot = m.err.toUpperCase();
  } else {
    const bits = [];
    if (m.model) bits.push(m.model);
    if (has(m.vrmTempC)) bits.push("VRM " + fmt(m.vrmTempC, 0) + "°C");
    if (m.uptimeSeconds) bits.push("UP " + duration(m.uptimeSeconds));
    if (m.poolUserMasked) bits.push(m.poolUserMasked);
    foot = bits.join("  |  ");
  }
  setText(id + "-foot", foot);
}

function renderTotals(t) {
  el("mt").classList.toggle("is-offline", t.minersOnline === 0);

  setText("mt-hash", fmt(t.hashrateThs));
  setBadge("mt-power", fmt(t.powerW, 0));
  setBadge("mt-eff", has(t.efficiencyJth) ? fmt(t.efficiencyJth, 1) : EM_DASH);

  setText("mt-online-v", t.minersOnline + " / " + t.minersTotal);
  setClass("mt-online", "badge", t.minersOnline === t.minersTotal ? "is-ok" : "is-crit");

  setText("mt-foot", t.powerComplete ? "" : "PARTIAL: ONE MINER REPORTS NO POWER");
}

function renderPool(p, prob) {
  el("panel-solo").classList.toggle("is-stale", !!p.stale);
  setText("pool-provider", p.provider ? p.provider.toUpperCase() : "NO POOL");
  setText("pool-age", p.hasData ? duration(p.ageSeconds) + " ago" : "no data");

  setText("pool-best", si(p.bestDifficulty));
  setText("pool-bestever", si(p.bestEver));
  setText("pool-workers", p.workersCount ? String(p.workersCount) : EM_DASH);
  setText("pool-lastshare", has(p.lastShareSeconds) ? duration(p.lastShareSeconds) + " ago" : EM_DASH);

  setText("pool-shares", int(p.sharesAccepted) + " / " + int(p.sharesRejected));
  setClass("pool-shares", "val", p.sharesRejected > 0 ? "is-crit" : "is-ok");

  // The pool cannot report rejects, so those come from the miners. Say so
  // rather than passing them off as pool-side numbers.
  setText("pool-rej-note", p.rejectedFromMiners ? "miners" : "");
  setText("pool-share-note", p.lastShareInferred ? "inferred" : "");

  if (!prob) {
    ["prob-day", "prob-week", "prob-month", "prob-year", "prob-median"].forEach((id) => setText(id, EM_DASH));
    return;
  }
  setText("prob-day", percent(prob.day));
  setText("prob-week", percent(prob.week));
  setText("prob-month", percent(prob.month));
  setText("prob-year", percent(prob.year));
  setText("prob-median", years(prob.medianYears));
}

function renderNetwork(n, prob) {
  el("panel-net").classList.toggle("is-stale", !!n.stale);
  setText("net-age", n.hasData ? duration(n.ageSeconds) + " ago" : "no data");

  setText("net-diff", n.hasData ? si(n.difficulty) : EM_DASH);
  setText("net-hash", n.hasData ? si(n.networkHashrateHs) + "H/s" : EM_DASH);
  setText("net-lastblock", n.hasData ? duration(n.secondsSinceBlock) + " ago" : EM_DASH);
  setText("net-price", n.hasData && n.priceEur > 0 ? euro(n.priceEur) : EM_DASH);
  setText("net-subsidy", n.hasData ? fmt(n.subsidyBtc, 3) + " BTC" : EM_DASH);

  if (n.hasData && n.nextRetargetEtaSeconds) {
    const sign = n.nextRetargetChangePct >= 0 ? "+" : "";
    setText("net-retarget", sign + fmt(n.nextRetargetChangePct, 2) + " % / " + duration(n.nextRetargetEtaSeconds));
  } else {
    setText("net-retarget", EM_DASH);
  }

  setText("net-share", prob ? percent(prob.shareOfNetwork) : EM_DASH);
}

function renderHeader(v, connected) {
  setText("hdr-block", v.network.hasData ? int(v.network.height) : EM_DASH);
  setText("hdr-source", v.network.sourceLabel || EM_DASH);

  const anyStale = v.miners.some((m) => m.stale) || v.pool.stale || v.network.stale;
  if (!connected) {
    setClass("hdr-dot", "dot", "dot-off");
    setClass("hdr-state", "hchip hchip-state", "is-down");
    setText("hdr-status", "RECONNECTING");
  } else if (anyStale) {
    setClass("hdr-dot", "dot", "dot-warn");
    setClass("hdr-state", "hchip hchip-state", "is-warn");
    setText("hdr-status", "DEGRADED");
  } else {
    setClass("hdr-dot", "dot", "dot-on");
    setClass("hdr-state", "hchip hchip-state", "");
    setText("hdr-status", "ONLINE");
  }
}

function renderFooter(v) {
  const parts = v.miners.map(
    (m) => "NODE " + m.name.split(" ")[0].toUpperCase() + ": " + (m.online ? "OK" : "DOWN")
  );
  parts.push("POOL: " + (v.pool.online ? "CONNECTED" : "DOWN"));
  parts.push("NET: " + (v.network.online ? "SYNCED" : "DOWN"));
  parts.push("REFRESH: 1.0s");
  setText("foot-ages", "SYS: " + parts.join("  |  "));
}

/* ---------------- wiring ---------------- */

let connected = false;

let lastView = null;

function render(v) {
  lastView = v;
  updateSaverConfig(v.screensaverSeconds);
  // Rebuild tiles when the fleet's names change, so each device keeps its mark.
  const sig = v.miners.map((m) => m.name).join("|");
  if (sig !== builtSig) buildTiles(v.miners);
  v.miners.forEach((m, i) => renderMiner(i, m));
  renderTotals(v.totals);
  renderPool(v.pool, v.probability);
  renderNetwork(v.network, v.probability);
  renderHeader(v, connected);
  renderFooter(v);
}

function connect() {
  const es = new EventSource("/api/v1/stream");

  es.addEventListener("open", () => { connected = true; });
  es.addEventListener("snapshot", (e) => {
    connected = true;
    try {
      render(JSON.parse(e.data));
    } catch (err) {
      console.error("bad snapshot", err);
    }
  });
  es.addEventListener("error", () => {
    // EventSource reconnects on its own. Reflect the gap in the header so a
    // frozen dashboard is never mistaken for a calm one.
    connected = false;
    setClass("hdr-dot", "dot", "dot-off");
    setClass("hdr-state", "hchip hchip-state", "is-down");
    setText("hdr-status", "RECONNECTING");
  });
}

// The settings route answers on loopback only, so the link is offered only
// where it would actually work.
// The admin surface answers to the local network, so the entry points are shown
// everywhere the dashboard is reached from a trusted LAN. On a public host the
// routes 404, which the settings page surfaces as an error.
{
  const link = el("ftr-cfg");
  if (link) link.classList.remove("is-hidden");
}

// Paint once from a plain GET so the first frame does not wait on the stream.
// ?static=1 renders a single frame without opening the stream, which lets a
// headless screenshot capture populated data deterministically.
const staticMode = new URLSearchParams(location.search).has("static");
if (staticMode) connected = true; // single-frame capture shows a live status
fetch("/api/v1/snapshot")
  .then((r) => r.json())
  .then(render)
  .catch(() => {})
  .finally(() => { if (!staticMode) connect(); });


/* ---------------- burn-in screensaver ----------------
   After the configured idle time a single panel drifts across a black screen so
   no pixel stays lit. Any pointer or key input dismisses it. */

const saver = {
  el: el("saver"),
  box: el("saver-box"),
  timeoutMs: 15 * 60 * 1000,
  idleTimer: null,
  raf: 0,
  x: 40, y: 40, vx: 1.1, vy: 0.9,
  active: false,
};

function updateSaverConfig(seconds) {
  // 0 disables the screensaver entirely.
  saver.timeoutMs = (seconds && seconds > 0 ? seconds : 0) * 1000;
  if (saver.timeoutMs === 0) {
    stopSaver();
    if (saver.idleTimer) { clearTimeout(saver.idleTimer); saver.idleTimer = null; }
  } else if (!saver.active && !saver.idleTimer) {
    armIdle();
  }
}

function armIdle() {
  if (saver.timeoutMs === 0) return;
  if (saver.idleTimer) clearTimeout(saver.idleTimer);
  saver.idleTimer = setTimeout(startSaver, saver.timeoutMs);
}

function fillSaver() {
  const v = lastView;
  if (!v) return;
  const t = v.totals, n = v.network;
  setText("sv-hash", fmt(t.hashrateThs));
  setText("sv-power", fmt(t.powerW, 0) + " W");
  setText("sv-eff", t.efficiencyJth != null ? fmt(t.efficiencyJth, 1) + " J/TH" : EM_DASH);
  setText("sv-miners", t.minersOnline + " / " + t.minersTotal);
  setText("sv-price", n.hasData && n.priceEur > 0 ? euro(n.priceEur) : EM_DASH);
  setText("sv-block", n.hasData ? int(n.height) : EM_DASH);
  setText("sv-diff", n.hasData ? si(n.difficulty) : EM_DASH);
  setText("sv-nethash", n.hasData ? si(n.networkHashrateHs) + "H/s" : EM_DASH);
}

function startSaver() {
  if (saver.active || saver.timeoutMs === 0) return;
  saver.active = true;
  fillSaver();
  saver.el.classList.add("on");
  // Refresh the figures every few seconds while the saver runs.
  saver.fillTimer = setInterval(fillSaver, 5000);
  bounce();
}

function stopSaver() {
  if (!saver.active) return;
  saver.active = false;
  saver.el.classList.remove("on");
  cancelAnimationFrame(saver.raf);
  if (saver.fillTimer) { clearInterval(saver.fillTimer); saver.fillTimer = null; }
}

function bounce() {
  const w = saver.box.offsetWidth, h = saver.box.offsetHeight;
  const maxX = Math.max(0, window.innerWidth - w);
  const maxY = Math.max(0, window.innerHeight - h);
  saver.x += saver.vx;
  saver.y += saver.vy;
  if (saver.x <= 0)    { saver.x = 0;    saver.vx = Math.abs(saver.vx); }
  if (saver.x >= maxX) { saver.x = maxX; saver.vx = -Math.abs(saver.vx); }
  if (saver.y <= 0)    { saver.y = 0;    saver.vy = Math.abs(saver.vy); }
  if (saver.y >= maxY) { saver.y = maxY; saver.vy = -Math.abs(saver.vy); }
  saver.box.style.transform = `translate(${saver.x}px, ${saver.y}px)`;
  saver.raf = requestAnimationFrame(bounce);
}

function onActivity() {
  if (saver.active) stopSaver();
  armIdle();
}

["mousemove", "mousedown", "touchstart", "keydown", "wheel"].forEach((ev) =>
  window.addEventListener(ev, onActivity, { passive: true })
);
