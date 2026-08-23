// Fleet history charts. Fetches the rolling record and draws simple line charts
// on canvas — no chart library, so nothing loads from outside the binary.

const el = (id) => document.getElementById(id);
let range = "1h";

const cssVar = (n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim();

function fmt(v, d = 2) {
  if (v == null || !Number.isFinite(v)) return "—";
  return v.toFixed(d);
}
function euro(v) {
  if (!v) return "—";
  return Math.round(v).toLocaleString("de-DE") + " €";
}

// Draw a single series onto a canvas with a grid, min/max labels and glow.
function drawChart(canvas, points, accessor, color, fmtVal) {
  const ctx = canvas.getContext("2d");
  const W = canvas.width, H = canvas.height;
  ctx.clearRect(0, 0, W, H);

  const padL = 90, padR = 20, padT = 14, padB = 24;
  const plotW = W - padL - padR, plotH = H - padT - padB;

  // background grid
  ctx.strokeStyle = "rgba(0,180,255,0.10)";
  ctx.lineWidth = 1;
  for (let i = 0; i <= 4; i++) {
    const y = padT + (plotH * i) / 4;
    ctx.beginPath(); ctx.moveTo(padL, y); ctx.lineTo(W - padR, y); ctx.stroke();
  }

  const vals = points.map(accessor).filter((v) => Number.isFinite(v));
  if (points.length < 2 || vals.length < 2) {
    ctx.fillStyle = cssVar("--label");
    ctx.font = "14px 'Share Tech Mono', monospace";
    ctx.fillText("collecting data…", padL, padT + plotH / 2);
    return;
  }

  let min = Math.min(...vals), max = Math.max(...vals);
  if (min === max) { min -= 1; max += 1; }
  const pad = (max - min) * 0.1; min -= pad; max += pad;

  const t0 = points[0].t, t1 = points[points.length - 1].t;
  const span = Math.max(1, t1 - t0);
  const x = (t) => padL + (plotW * (t - t0)) / span;
  const y = (v) => padT + plotH * (1 - (v - min) / (max - min));

  // y-axis labels (min / mid / max)
  ctx.fillStyle = cssVar("--label");
  ctx.font = "12px 'Share Tech Mono', monospace";
  ctx.textAlign = "right";
  [max, (max + min) / 2, min].forEach((v, i) => {
    ctx.fillText(fmtVal(v), padL - 8, padT + (plotH * i) / 2 + 4);
  });
  ctx.textAlign = "left";

  // area fill
  ctx.beginPath();
  points.forEach((p, i) => { const px = x(p.t), py = y(accessor(p)); i ? ctx.lineTo(px, py) : ctx.moveTo(px, py); });
  ctx.lineTo(x(t1), padT + plotH);
  ctx.lineTo(x(t0), padT + plotH);
  ctx.closePath();
  ctx.fillStyle = color.replace("COLOR", "0.10");
  ctx.fill();

  // line with glow
  ctx.beginPath();
  points.forEach((p, i) => { const px = x(p.t), py = y(accessor(p)); i ? ctx.lineTo(px, py) : ctx.moveTo(px, py); });
  ctx.strokeStyle = color.replace("COLOR", "1");
  ctx.lineWidth = 2;
  ctx.shadowColor = color.replace("COLOR", "0.6");
  ctx.shadowBlur = 6;
  ctx.stroke();
  ctx.shadowBlur = 0;
}

let miner = ""; // "" = fleet total

// Keep the miner dropdown in sync with the miners that have history.
function populateMiners(names) {
  const sel = el("miner-sel");
  const want = ["", ...(names || [])];
  const have = [...sel.options].map((o) => o.value);
  if (have.join("|") !== want.join("|")) {
    sel.innerHTML = "";
    want.forEach((v) => {
      const o = document.createElement("option");
      o.value = v;
      o.textContent = v === "" ? "FLEET TOTAL" : v.toUpperCase();
      sel.appendChild(o);
    });
  }
  // The state is the source of truth; reflect it in the control.
  sel.value = want.includes(miner) ? miner : "";
}

async function load() {
  try {
    const q = "/api/v1/history?range=" + encodeURIComponent(range) + (miner ? "&miner=" + encodeURIComponent(miner) : "");
    const data = await fetch(q).then((r) => r.json());
    const pts = data.points || [];
    populateMiners(data.miners);
    el("hist-msg").textContent = pts.length < 2 ? "Not enough history yet — charts fill in as data is collected." : "";

    drawChart(el("c-hash"), pts, (p) => p.hashrate, "rgba(0,229,255,COLOR)", (v) => fmt(v, 2));
    drawChart(el("c-power"), pts, (p) => p.power, "rgba(255,213,79,COLOR)", (v) => fmt(v, 0));

    const last = pts[pts.length - 1] || null;
    el("last-hash").textContent = last ? fmt(last.hashrate, 2) + " TH/s" : "";
    el("last-power").textContent = last ? fmt(last.power, 0) + " W" : "";

    if (miner) {
      // Per-miner view: the third chart is the miner's ASIC temperature.
      el("hd-third").innerHTML = 'ASIC TEMP <span class="chart-unit">°C</span> <span class="chart-last" id="last-third"></span>';
      drawChart(el("c-third"), pts, (p) => p.temp, "rgba(255,82,82,COLOR)", (v) => fmt(v, 0));
      el("last-third").textContent = last ? fmt(last.temp, 0) + " °C" : "";
    } else {
      // Fleet view: the third chart is the BTC price.
      el("hd-third").innerHTML = 'BTC PRICE <span class="chart-unit">EUR</span> <span class="chart-last" id="last-third"></span>';
      drawChart(el("c-third"), pts, (p) => p.price, "rgba(0,230,118,COLOR)", (v) => Math.round(v).toLocaleString("de-DE"));
      el("last-third").textContent = last ? euro(last.price) : "";
    }
  } catch (err) {
    el("hist-msg").textContent = "Could not load history: " + err;
  }
}

el("range-tabs").addEventListener("click", (e) => {
  const b = e.target.closest(".range-tab");
  if (!b) return;
  range = b.dataset.range;
  document.querySelectorAll(".range-tab").forEach((t) => t.classList.toggle("is-on", t === b));
  load();
});

el("miner-sel").addEventListener("change", (e) => {
  miner = e.target.value;
  load();
});

// Deep link: /history?miner=Name opens straight to that miner's charts.
const initialMiner = new URLSearchParams(location.search).get("miner");
if (initialMiner) miner = initialMiner;

load();
setInterval(load, 30000);
