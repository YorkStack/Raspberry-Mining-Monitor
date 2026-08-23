// Threshold settings page.
//
// Reachable from this machine only. It writes display thresholds and nothing
// else: there is no path from here to a miner.

const el = (id) => document.getElementById(id);

const FIELDS = [
  ["asicWarnC", "ASIC amber at"],
  ["asicCritC", "ASIC red at"],
  ["vrmWarnC", "VRM amber at"],
  ["vrmCritC", "VRM red at"],
];

let state = { default: {}, miners: [], overrides: {} };

function message(text, kind) {
  const n = el("cfg-msg");
  n.textContent = text;
  n.className = "cfg-msg" + (kind ? " is-" + kind : "");
  if (kind === "ok") setTimeout(() => { if (n.textContent === text) n.textContent = ""; }, 4000);
}

function cardMarkup(name, values, overridden) {
  const id = encodeURIComponent(name);
  const rows = FIELDS.map(([key, label]) => `
    <label class="cfg-field">
      <span>${label}</span>
      <input type="number" min="0" max="150" step="1" inputmode="numeric"
             data-miner="${id}" data-key="${key}" value="${values[key]}">
      <em>°C</em>
    </label>`).join("");

  return `
    <div class="cfg-card" data-card="${id}">
      <div class="cfg-card-head">
        <h2>${name}</h2>
        <span class="cfg-tag">${overridden ? "CUSTOM" : "FLEET DEFAULT"}</span>
      </div>
      <div class="cfg-fields">${rows}</div>
      <div class="cfg-actions">
        <button class="cfg-btn cfg-save" data-miner="${id}">SAVE</button>
        <button class="cfg-btn cfg-reset" data-miner="${id}" ${overridden ? "" : "disabled"}>RESET TO DEFAULT</button>
      </div>
    </div>`;
}

function render() {
  renderMonitoring();
  el("cfg-list").innerHTML = state.miners
    .map((name) => {
      const overridden = Object.prototype.hasOwnProperty.call(state.overrides, name);
      return cardMarkup(name, overridden ? state.overrides[name] : state.default, overridden);
    })
    .join("");
}

function renderMonitoring() {
  const disabled = state.disabled || {};
  el("cfg-monitoring").innerHTML = state.miners
    .map((name) => {
      const on = !disabled[name];
      const id = encodeURIComponent(name);
      return `
        <div class="cfg-toggle-row">
          <span class="cfg-toggle-name">${name}</span>
          <label class="switch">
            <input type="checkbox" data-miner="${id}" ${on ? "checked" : ""}>
            <span class="slider"></span>
          </label>
          <span class="cfg-toggle-state">${on ? "MONITORED" : "OFF"}</span>
        </div>`;
    })
    .join("");
}

function readCard(encodedName) {
  const out = {};
  for (const [key] of FIELDS) {
    const input = document.querySelector(`input[data-miner="${encodedName}"][data-key="${key}"]`);
    const v = Number(input.value);
    if (!Number.isFinite(v)) return null;
    out[key] = v;
  }
  return out;
}

async function apply(method, encodedName, body) {
  const res = await fetch("/api/v1/settings/" + encodedName, {
    method,
    headers: body ? { "Content-Type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  if (!res.ok) throw new Error(text.trim() || res.statusText);
  return JSON.parse(text);
}

document.addEventListener("change", async (e) => {
  const cb = e.target.closest('input[type="checkbox"][data-miner]');
  if (!cb) return;
  const encodedName = cb.dataset.miner;
  try {
    const res = await fetch("/api/v1/settings/" + encodedName + "/enabled", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ enabled: cb.checked }),
    });
    const text = await res.text();
    if (!res.ok) throw new Error(text.trim() || res.statusText);
    state = JSON.parse(text);
    render();
    message(cb.checked ? "Miner switched on." : "Miner switched off.", "ok");
  } catch (err) {
    message(String(err.message || err), "bad");
    cb.checked = !cb.checked;
  }
});

document.addEventListener("click", async (e) => {
  const save = e.target.closest(".cfg-save");
  const reset = e.target.closest(".cfg-reset");
  if (!save && !reset) return;

  const encodedName = (save || reset).dataset.miner;
  try {
    if (save) {
      const body = readCard(encodedName);
      if (!body) { message("Every field needs a number.", "bad"); return; }
      state = await apply("PUT", encodedName, body);
      message("Saved. The dashboard picks it up on the next frame.", "ok");
    } else {
      state = await apply("DELETE", encodedName, null);
      message("Reset to the fleet default.", "ok");
    }
    render();
  } catch (err) {
    message(String(err.message || err), "bad");
  }
});

function loadSettings() {
  return fetch("/api/v1/settings")
    .then((r) => r.json())
    .then((s) => { state = s; render(); })
    .catch((err) => message("Could not load settings: " + err, "bad"));
}
loadSettings();


/* ---- miners + providers editor ---- */

let minersState = { miners: [], providers: {} };

function minerRow(m, i) {
  return `
    <div class="miner-row" data-i="${i}">
      <input class="m-name" placeholder="Name" value="${(m.name || "").replace(/"/g, "&quot;")}">
      <select class="m-type">
        <option value="axeos"${m.type !== "demo" ? " selected" : ""}>axeos</option>
        <option value="demo"${m.type === "demo" ? " selected" : ""}>demo</option>
      </select>
      <input class="m-host" placeholder="IP / hostname" value="${(m.host || "").replace(/"/g, "&quot;")}">
      <input class="m-addr" placeholder="payout address" value="${(m.payoutAddress || "").replace(/"/g, "&quot;")}">
      <input class="m-int" type="number" min="1" max="60" title="poll interval (s)" value="${m.intervalSeconds || 2}">
      <button class="cfg-btn m-del" title="remove">✕</button>
    </div>`;
}

function renderMiners() {
  el("cfg-miners").innerHTML =
    `<div class="miner-row miner-head">
       <span>Name</span><span>Type</span><span>Host</span><span>Payout address</span><span>Poll s</span><span></span>
     </div>` + minersState.miners.map(minerRow).join("");
  el("prov-btc").value = minersState.providers.bitcoinBaseUrl || "";
  el("prov-pool").value = minersState.providers.poolBaseUrl || "";
}

function collectMiners() {
  return [...document.querySelectorAll("#cfg-miners .miner-row:not(.miner-head)")].map((row) => ({
    name: row.querySelector(".m-name").value.trim(),
    type: row.querySelector(".m-type").value,
    host: row.querySelector(".m-host").value.trim(),
    payoutAddress: row.querySelector(".m-addr").value.trim(),
    intervalSeconds: Number(row.querySelector(".m-int").value) || 2,
  }));
}

async function loadMiners() {
  try {
    minersState = await fetch("/api/v1/miners").then((r) => {
      if (!r.ok) throw new Error("miners API unavailable");
      return r.json();
    });
    renderMiners();
  } catch (err) {
    const host = el("cfg-miners");
    if (host) host.innerHTML = `<p class="cfg-subnote">Miner editing is only available from the local network.</p>`;
  }
}

document.addEventListener("click", async (e) => {
  if (e.target.closest("#miner-add")) {
    minersState.miners.push({ name: "", type: "axeos", host: "", payoutAddress: "", intervalSeconds: 2 });
    renderMiners();
    return;
  }
  if (e.target.closest(".m-del")) {
    const row = e.target.closest(".miner-row");
    const i = Number(row.dataset.i);
    minersState.miners = collectMiners().filter((_, idx) => idx !== i);
    renderMiners();
    return;
  }
  if (e.target.closest("#miner-save")) {
    try {
      const res = await fetch("/api/v1/miners", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ miners: collectMiners() }),
      });
      const text = await res.text();
      if (!res.ok) throw new Error(text.trim() || res.statusText);
      minersState = JSON.parse(text);
      renderMiners();
      // Reload the whole settings view so monitoring/threshold sections match.
      await loadSettings();
      message("Miners saved and applied.", "ok");
    } catch (err) {
      message(String(err.message || err), "bad");
    }
    return;
  }
  if (e.target.closest("#prov-save")) {
    try {
      const res = await fetch("/api/v1/providers", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          bitcoinBaseUrl: el("prov-btc").value.trim(),
          poolBaseUrl: el("prov-pool").value.trim(),
        }),
      });
      const text = await res.text();
      if (!res.ok) throw new Error(text.trim() || res.statusText);
      minersState = JSON.parse(text);
      renderMiners();
      message("Providers saved and applied.", "ok");
    } catch (err) {
      message(String(err.message || err), "bad");
    }
    return;
  }
});

loadMiners();
