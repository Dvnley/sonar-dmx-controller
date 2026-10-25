"use strict";

const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const esc = s => String(s).replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));

let state = null;
let diag = null;
let profiles = [];
let tab = "live";
let cardSig = "";
let patchDraft = null;
let patchDirty = false;
let openMaps = new Set();
const selected = new Set();
const mix = { r: 0, g: 0, b: 0, a: 0, w: 0, uv: 0 };
const PALETTE = ["#3b6fd8", "#27ae60", "#b9770e", "#8e44ad", "#16a085", "#2c3e8f", "#7f8c1d", "#c0399b", "#5d6d7e", "#0e7c9b"];

// ---------- API ----------
async function api(path, body) {
  const opts = body === undefined ? {} : { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) };
  let res, data;
  try {
    res = await fetch(path, opts);
    data = await res.json();
  } catch (e) {
    showError("The controller is not responding. Is the app still running?");
    throw e;
  }
  if (!res.ok) {
    showError(data.error || res.statusText);
    throw new Error(data.error);
  }
  showError("");
  if (data && Array.isArray(data.fixtures) && "conflicts" in data) {
    state = data;
    render();
  }
  return data;
}

function showError(msg) {
  const el = $("#error");
  el.textContent = msg;
  el.classList.toggle("hidden", !msg);
}

// Coalesce fast slider input to at most one request per 50 ms per key.
const pending = new Map();
function throttled(key, fn) {
  const p = pending.get(key);
  if (p) { p.fn = fn; return; }
  const entry = { fn };
  pending.set(key, entry);
  entry.fn();
  entry.fn = null;
  setTimeout(function flush() {
    if (entry.fn) { const f = entry.fn; entry.fn = null; f(); setTimeout(flush, 50); }
    else pending.delete(key);
  }, 50);
}

let dragging = 0;
document.addEventListener("pointerdown", e => { if (e.target.matches("input[type=range], .xy")) dragging++; });
document.addEventListener("pointerup", () => { dragging = 0; });
document.addEventListener("pointercancel", () => { dragging = 0; });

// ---------- tabs ----------
$$("nav button").forEach(b => b.onclick = () => {
  tab = b.dataset.tab;
  $$("nav button").forEach(x => x.classList.toggle("active", x === b));
  $$(".tab").forEach(s => s.classList.toggle("hidden", s.id !== "tab-" + tab));
  if (tab === "patch" && !patchDirty) resetDraft();
  render();
});

// ---------- render ----------
function render() {
  if (!state) return;
  renderBanners();
  renderLive();
  if (tab === "patch") { if (!patchDraft) resetDraft(); renderPatchWarnings(); }
  renderSweep();
  renderSetup();
}

function renderBanners() {
  const c = $("#conflicts");
  if (state.conflicts.length) {
    c.innerHTML = "<b>Address overlap:</b> fixtures share DMX channels, so they will fight. Re-patch one group in the Patch tab or move it to another universe."
      + "<ul>" + state.conflicts.map(x => `<li>${esc(x.message)}</li>`).join("") + "</ul>";
  }
  c.classList.toggle("hidden", !state.conflicts.length);
  const s = $("#sweepbanner");
  s.innerHTML = state.sweep ? `Channel sweep running on universe ${state.sweep.universe}: <b>${esc(state.sweep.label)}</b>. Normal output is paused. <button id="banner-stop">Stop sweep</button>` : "";
  s.classList.toggle("hidden", !state.sweep);
  if (state.sweep) $("#banner-stop").onclick = () => api("/api/sweep/stop", {});
}

// ---------- live ----------
const MIX = [["r", "Red"], ["g", "Green"], ["b", "Blue"], ["a", "Amber"], ["w", "White"], ["uv", "UV"]];

function fader(label, value, attrs) {
  return `<label class="fader"><span title="${esc(label)}">${esc(label)}</span><input type="range" min="0" max="${attrs.max ?? 255}" value="${value}" ${attrs.data}><span>${value}</span></label>`;
}

function buildMixer() {
  $("#mixer").innerHTML = MIX.map(([k, n]) => fader(n, mix[k], { data: `data-mix="${k}"` })).join("");
  $$("#mixer input").forEach(inp => inp.oninput = () => {
    mix[inp.dataset.mix] = +inp.value;
    inp.nextElementSibling.textContent = inp.value;
    throttled("mix", () => api("/api/color", { fixtures: targets(), [inp.dataset.mix]: +inp.value }));
  });
}

function targets() { return selected.size ? [...selected] : "all"; }

$$(".swatches button").forEach(b => b.onclick = () => {
  const [r, g, bl, a, w, uv] = b.dataset.c.split(",").map(Number);
  Object.assign(mix, { r, g, b: bl, a, w, uv });
  buildMixer();
  api("/api/color", { fixtures: targets(), r, g, b: bl, a, w, uv });
});

$("#master").oninput = e => {
  $("#masterv").textContent = e.target.value;
  throttled("master", () => api("/api/master", { value: +e.target.value }));
};
$("#blackout").onclick = () => api("/api/blackout", { on: !state.blackout });
$("#test").onclick = () => api("/api/test", {});

function renderLive() {
  const m = $("#master");
  if (!dragging || document.activeElement !== m) { m.value = state.master; $("#masterv").textContent = state.master; }
  const bo = $("#blackout");
  bo.classList.toggle("on", state.blackout);
  bo.textContent = state.blackout ? "Blackout ON" : "Blackout";
  $("#test").textContent = state.test_running ? `Testing: ${state.test_color}` : "RGBW test";
  for (const i of [...selected]) if (i >= state.fixtures.length) selected.delete(i);
  $("#colortarget").textContent = selected.size ? `${selected.size} selected fixture${selected.size > 1 ? "s" : ""}` : "all color fixtures";

  const sig = JSON.stringify(state.fixtures.map(f => [f.name, f.profile, f.mode, f.channels]));
  if (sig !== cardSig) { cardSig = sig; buildCards(); }
  updateCards();
}

function hasColor(f) { return f.channels.some(c => ["red", "green", "blue", "amber", "white", "uv"].includes(c.type)); }
function chIndex(f, type) { return f.channels.findIndex(c => c.type === type); }

function previewColor(f) {
  const v = t => { const i = chIndex(f, t); return i < 0 ? 0 : f.values[i]; };
  const w = v("white"), a = v("amber"), uv = v("uv");
  const r = Math.min(255, v("red") + w + a + uv * 0.4), g = Math.min(255, v("green") + w + a * 0.6), b = Math.min(255, v("blue") + w + uv);
  return `rgb(${r | 0},${g | 0},${b | 0})`;
}

function buildCards() {
  $("#cards").innerHTML = state.fixtures.map(f => {
    const moving = chIndex(f, "pan") >= 0 || chIndex(f, "tilt") >= 0;
    return `<div class="card" data-i="${f.index}">
      <div class="fhead">
        ${hasColor(f) ? `<input type="checkbox" class="sel" title="Include in color changes">` : ""}
        ${hasColor(f) ? `<span class="swatch"></span>` : ""}
        <b>${esc(f.name)}</b>
        <button class="en"></button>
      </div>
      <div class="meta">${esc(f.profile_name)} · ${esc(f.mode)} · U${f.universe} · <span class="addr"></span></div>
      <div class="chans">${fader("Brightness %", f.brightness, { max: 100, data: 'data-bright' })}</div>
      ${moving ? `<div class="xywrap"><div class="xy" title="Drag to aim (pan / tilt)"><div class="dot"></div></div>
        <div class="xybtns"><button data-act="center">Center</button><button data-act="open">Light on</button><button data-act="dark">Light off</button><button data-act="reset">Reset values</button></div></div>` : ""}
      <div class="chans">${f.channels.map((c, j) => fader(`${j + 1}. ${c.name}`, f.values[j], { data: `data-ch="${j + 1}"` })).join("")}</div>
      ${!moving ? `<div class="row"><button data-act="reset">Reset values</button></div>` : ""}
    </div>`;
  }).join("");

  $$("#cards .card").forEach(card => {
    const i = +card.dataset.i;
    const sel = $(".sel", card);
    if (sel) { sel.checked = selected.has(i); sel.onchange = () => { sel.checked ? selected.add(i) : selected.delete(i); renderLive(); }; }
    $(".en", card).onclick = () => api("/api/fixture", { index: i, enabled: !state.fixtures[i].enabled });
    $("[data-bright]", card).oninput = e => {
      e.target.nextElementSibling.textContent = e.target.value;
      throttled("b" + i, () => api("/api/fixture", { index: i, brightness: +e.target.value }));
    };
    $$("[data-ch]", card).forEach(inp => inp.oninput = () => {
      inp.nextElementSibling.textContent = inp.value;
      throttled(`c${i}.${inp.dataset.ch}`, () => api("/api/fixture", { index: i, values: { [inp.dataset.ch]: +inp.value } }));
    });
    $$("[data-act]", card).forEach(b => b.onclick = () => cardAction(i, b.dataset.act));
    const xy = $(".xy", card);
    if (xy) {
      const move = e => {
        const r = xy.getBoundingClientRect();
        const x = Math.round(Math.max(0, Math.min(1, (e.clientX - r.left) / r.width)) * 255);
        const y = Math.round(Math.max(0, Math.min(1, 1 - (e.clientY - r.top) / r.height)) * 255);
        placeDot(xy, x, y);
        throttled("xy" + i, () => api("/api/fixture", { index: i, values: panTilt(state.fixtures[i], x, y) }));
      };
      xy.onpointerdown = e => { xy.setPointerCapture(e.pointerId); move(e); xy.onpointermove = move; };
      xy.onpointerup = () => { xy.onpointermove = null; };
    }
  });
}

function panTilt(f, x, y) {
  const v = {};
  if (chIndex(f, "pan") >= 0) v[chIndex(f, "pan") + 1] = x;
  if (chIndex(f, "tilt") >= 0) v[chIndex(f, "tilt") + 1] = y;
  return v;
}

function placeDot(xy, x, y) {
  const dot = $(".dot", xy);
  dot.style.left = (x / 255 * 100) + "%";
  dot.style.top = ((255 - y) / 255 * 100) + "%";
}

function cardAction(i, act) {
  const f = state.fixtures[i];
  const set = (type, val, v = {}) => { const j = chIndex(f, type); if (j >= 0) v[j + 1] = val; return v; };
  if (act === "center") api("/api/fixture", { index: i, values: panTilt(f, 128, 128) });
  if (act === "open") api("/api/fixture", { index: i, values: set("dimmer", 255) });
  if (act === "dark") api("/api/fixture", { index: i, values: set("dimmer", 0) });
  if (act === "reset") api("/api/fixture/reset", { index: i });
}

function updateCards() {
  state.fixtures.forEach(f => {
    const card = $(`#cards .card[data-i="${f.index}"]`);
    if (!card) return;
    card.classList.toggle("off", !f.enabled);
    const en = $(".en", card);
    en.textContent = f.enabled ? "On" : "Off";
    en.classList.toggle("primary", f.enabled);
    $(".addr", card).textContent = f.channels.length > 1 ? `ch ${f.address}-${f.end}` : `ch ${f.address}`;
    const sw = $(".swatch", card);
    if (sw) sw.style.background = previewColor(f);
    const setRange = (inp, v) => { if (document.activeElement !== inp || !dragging) { inp.value = v; inp.nextElementSibling.textContent = v; } };
    setRange($("[data-bright]", card), f.brightness);
    $$("[data-ch]", card).forEach(inp => setRange(inp, f.values[+inp.dataset.ch - 1]));
    const xy = $(".xy", card);
    if (xy && !xy.onpointermove) {
      const p = chIndex(f, "pan"), t = chIndex(f, "tilt");
      placeDot(xy, p >= 0 ? f.values[p] : 128, t >= 0 ? f.values[t] : 128);
    }
  });
}

// ---------- patch ----------
function strip(f) {
  return { name: f.name, profile: f.profile, mode: f.mode, universe: f.universe, address: f.address, enabled: f.enabled, brightness: f.brightness, channels: f.channels.map(c => ({ ...c })), values: [...f.values] };
}
function resetDraft() { patchDraft = state.fixtures.map(strip); patchDirty = false; openMaps.clear(); renderPatch(); }
function dirty() { patchDirty = true; renderPatchWarnings(); }
function profileById(id) { return profiles.find(p => p.id === id); }

function overlaps(list) {
  const out = [];
  list.forEach((a, i) => list.forEach((b, j) => {
    if (j <= i || !a.enabled || !b.enabled || a.universe !== b.universe || !a.channels.length || !b.channels.length) return;
    const from = Math.max(a.address, b.address), to = Math.min(a.address + a.channels.length - 1, b.address + b.channels.length - 1);
    if (from <= to) out.push({ a: i, b: j, text: `${a.name} and ${b.name} share ${from === to ? "channel " + from : `channels ${from}-${to}`} on universe ${a.universe}` });
  }));
  return out;
}

function freeAddress(i) {
  const f = patchDraft[i], n = f.channels.length;
  for (let a = 1; a + n - 1 <= 512; a++) {
    const clash = patchDraft.some((o, j) => j !== i && o.enabled && o.universe === f.universe && a <= o.address + o.channels.length - 1 && o.address <= a + n - 1);
    if (!clash) return a;
  }
  return null;
}

function renderPatch() {
  if (!patchDraft) return;
  $("#addprofile").innerHTML = profiles.map(p => `<option value="${p.id}">${esc(p.name)}</option>`).join("");
  const types = state.channel_types;
  $("#patchtable tbody").innerHTML = patchDraft.map((f, i) => {
    const p = profileById(f.profile);
    const profOpts = profiles.map(x => `<option value="${x.id}" ${x.id === f.profile ? "selected" : ""}>${esc(x.name)}</option>`).join("")
      + (p ? "" : `<option value="${esc(f.profile)}" selected>${esc(f.profile || "custom")}</option>`);
    const modeOpts = p ? p.modes.map(m => `<option ${m.name === f.mode ? "selected" : ""}>${esc(m.name)}</option>`).join("") : `<option>${esc(f.mode || "custom")}</option>`;
    const row = `<tr data-i="${i}">
      <td><input class="name" data-k="name" value="${esc(f.name)}"></td>
      <td><select data-k="profile">${profOpts}</select></td>
      <td><select data-k="mode">${modeOpts}</select></td>
      <td><input type="number" min="0" data-k="universe" value="${f.universe}"></td>
      <td><div class="row" style="margin:0;flex-wrap:nowrap"><input type="number" min="1" max="512" data-k="address" value="${f.address}"><button data-a="free" title="Move to the first free address on this universe">Free</button></div></td>
      <td><button data-a="map" title="Edit channel map">${f.channels.length} ch ▾</button></td>
      <td><input type="checkbox" data-k="enabled" ${f.enabled ? "checked" : ""}></td>
      <td><button data-a="del" title="Remove">✕</button></td>
    </tr>`;
    const map = !openMaps.has(i) ? "" : `<tr class="maprow" data-i="${i}"><td colspan="8">
      <table class="chmap"><tr><th>#</th><th>DMX</th><th>Name</th><th>Type</th><th>Default</th><th></th></tr>
      ${f.channels.map((c, j) => `<tr data-j="${j}"><td>${j + 1}</td><td class="meta">${f.address + j}</td>
        <td><input data-c="name" value="${esc(c.name)}"></td>
        <td><select data-c="type">${types.map(t => `<option ${t === c.type ? "selected" : ""}>${t}</option>`).join("")}</select></td>
        <td><input type="number" min="0" max="255" data-c="default" value="${c.default}"></td>
        <td><button data-c-a="up" title="Move up">↑</button><button data-c-a="del" title="Remove channel">✕</button></td></tr>`).join("")}
      </table>
      <div class="row" style="margin:0 0 6px"><button data-a="addch">Add channel</button>${p ? `<button data-a="frommode">Reset to profile</button>` : ""}<span class="hint">${esc(p?.notes || "")}</span></div>
    </td></tr>`;
    return row + map;
  }).join("");

  $$("#patchtable tbody tr[data-i]:not(.maprow)").forEach(tr => {
    const i = +tr.dataset.i, f = patchDraft[i];
    $$("[data-k]", tr).forEach(inp => inp[inp.tagName === "SELECT" || inp.type === "checkbox" ? "onchange" : "oninput"] = () => {
      const k = inp.dataset.k;
      if (k === "enabled") f.enabled = inp.checked;
      else if (k === "universe" || k === "address") f[k] = Math.max(k === "address" ? 1 : 0, +inp.value || 0);
      else if (k === "profile" || k === "mode") {
        const p = profileById(k === "profile" ? inp.value : f.profile);
        if (p) {
          const m = k === "profile" ? p.modes[0] : (p.modes.find(m => m.name === inp.value) || p.modes[0]);
          f.profile = p.id; f.mode = m.name; f.channels = m.channels.map(c => ({ ...c })); f.values = m.channels.map(c => c.default);
        }
        dirty(); renderPatch(); return;
      } else f[k] = inp.value;
      dirty();
    });
    $$("[data-a]", tr).forEach(b => b.onclick = () => patchAction(i, b.dataset.a));
  });
  $$("#patchtable tbody tr.maprow").forEach(tr => {
    const i = +tr.dataset.i, f = patchDraft[i];
    $$("[data-a]", tr).forEach(b => b.onclick = () => patchAction(i, b.dataset.a));
    $$("tr[data-j]", tr).forEach(row => {
      const j = +row.dataset.j;
      $$("[data-c]", row).forEach(inp => inp[inp.tagName === "SELECT" ? "onchange" : "oninput"] = () => {
        const k = inp.dataset.c;
        f.channels[j][k] = k === "default" ? Math.max(0, Math.min(255, +inp.value || 0)) : inp.value;
        dirty();
      });
      $$("[data-c-a]", row).forEach(b => b.onclick = () => {
        if (b.dataset.cA === "del") { f.channels.splice(j, 1); f.values.splice(j, 1); }
        if (b.dataset.cA === "up" && j > 0) {
          [f.channels[j - 1], f.channels[j]] = [f.channels[j], f.channels[j - 1]];
          [f.values[j - 1], f.values[j]] = [f.values[j], f.values[j - 1]];
        }
        dirty(); renderPatch();
      });
    });
  });
  renderPatchWarnings();
}

function patchAction(i, a) {
  const f = patchDraft[i];
  if (a === "del") { patchDraft.splice(i, 1); openMaps = new Set([...openMaps].filter(x => x !== i).map(x => x > i ? x - 1 : x)); }
  if (a === "map") openMaps.has(i) ? openMaps.delete(i) : openMaps.add(i);
  if (a === "free") { const addr = freeAddress(i); if (addr) f.address = addr; else showError(`No free run of ${f.channels.length} channels on universe ${f.universe}`); }
  if (a === "addch") { f.channels.push({ name: `Ch ${f.channels.length + 1}`, type: "generic", default: 0 }); f.values.push(0); }
  if (a === "frommode") { const p = profileById(f.profile); const m = p.modes.find(m => m.name === f.mode) || p.modes[0]; f.channels = m.channels.map(c => ({ ...c })); f.values = m.channels.map(c => c.default); }
  if (a !== "map") dirty();
  renderPatch();
}

$("#addfixture").onclick = () => {
  const p = profileById($("#addprofile").value);
  if (!p) return;
  const m = p.modes[0];
  const f = { name: `${p.name.split(" ").pop()} ${patchDraft.filter(x => x.profile === p.id).length + 1}`, profile: p.id, mode: m.name, universe: 0, address: 1, enabled: true, brightness: 100, channels: m.channels.map(c => ({ ...c })), values: m.channels.map(c => c.default) };
  patchDraft.push(f);
  f.address = freeAddress(patchDraft.length - 1) || 1;
  dirty(); renderPatch();
};
$("#revertpatch").onclick = () => resetDraft();
$("#savepatch").onclick = async () => {
  await api("/api/patch", { fixtures: patchDraft });
  resetDraft();
};

function renderPatchWarnings() {
  if (!patchDraft) return;
  const ov = overlaps(patchDraft);
  const bad = new Set(ov.flatMap(o => [o.a, o.b]));
  $$("#patchtable tbody tr[data-i]:not(.maprow)").forEach(tr => tr.classList.toggle("conflict", bad.has(+tr.dataset.i)));
  const msgs = ov.map(o => esc(o.text));
  patchDraft.forEach(f => { if (f.address + f.channels.length - 1 > 512) msgs.push(`${esc(f.name)} runs past channel 512`); });
  $("#patchwarn").innerHTML = (patchDirty ? `<p class="hint"><b>Unsaved changes.</b></p>` : "")
    + (msgs.length ? `<div class="banner warn"><b>Overlaps in this patch:</b><ul>${msgs.map(m => `<li>${m}</li>`).join("")}</ul></div>` : `<p class="hint">No overlaps.</p>`);
  renderMap();
}

function renderMap() {
  const unis = [...new Set(patchDraft.map(f => f.universe))].sort((a, b) => a - b);
  $("#dmxmap").innerHTML = unis.map(u => {
    const fx = patchDraft.map((f, i) => ({ f, i })).filter(x => x.f.universe === u && x.f.enabled);
    const last = Math.max(16, ...fx.map(x => x.f.address + x.f.channels.length - 1));
    const n = Math.min(512, Math.ceil((last + 4) / 16) * 16);
    const cells = [];
    for (let ch = 1; ch <= n; ch++) {
      const who = fx.filter(x => ch >= x.f.address && ch < x.f.address + x.f.channels.length);
      const title = who.map(x => `${x.f.name}: ${x.f.channels[ch - x.f.address].name}`).join("\n");
      const cls = who.length > 1 ? "cell clash" : who.length ? "cell used" : "cell";
      const bg = who.length === 1 ? `style="background:${PALETTE[who[0].i % PALETTE.length]}"` : "";
      cells.push(`<div class="${cls}" ${bg} title="${esc(title || "free")}">${ch}</div>`);
    }
    return `<div class="dmxrow"><h3>Universe ${u}</h3><div class="cells">${cells.join("")}</div>
      <div class="legend">${fx.map(x => `<span><i style="background:${PALETTE[x.i % PALETTE.length]}"></i>${esc(x.f.name)}</span>`).join("")}<span><i class="clash"></i>overlap</span></div></div>`;
  }).join("") || `<p class="hint">No fixtures patched.</p>`;
}

// ---------- sweep ----------
const PRESETS = {
  spot1: { from: 1, to: 9, val: 255, hold: 4, gap: 2, base: "", manual: true },
  spot2: { from: 3, to: 9, val: 255, hold: 4, gap: 2, base: "1=128,2=128,5=255,6=255", manual: true },
};
$$(".presets button").forEach(b => b.onclick = () => {
  if (b.dataset.preset === "wash") {
    $("#sw-script").value = "5:17=255;5:18=255;5:19=255;5:17=255,18=255,19=255;2:";
    $("details").open = true;
    return;
  }
  const p = PRESETS[b.dataset.preset];
  $("#sw-from").value = p.from; $("#sw-to").value = p.to; $("#sw-val").value = p.val;
  $("#sw-hold").value = p.hold; $("#sw-gap").value = p.gap; $("#sw-base").value = p.base; $("#sw-manual").checked = p.manual;
});
$("#sw-start").onclick = () => api("/api/sweep", {
  universe: +$("#sw-uni").value,
  plan: { from: +$("#sw-from").value, to: +$("#sw-to").value, value: +$("#sw-val").value, hold: +$("#sw-hold").value, gap: +$("#sw-gap").value, base: $("#sw-base").value, manual: $("#sw-manual").checked },
});
$("#sw-runscript").onclick = () => api("/api/sweep", { universe: +$("#sw-uni").value, script: $("#sw-script").value, manual: $("#sw-script-manual").checked });
$("#sw-stop").onclick = () => api("/api/sweep/stop", {});
$("#sw-prev").onclick = () => api("/api/sweep/step", { delta: -1 });
$("#sw-nextb").onclick = () => api("/api/sweep/step", { delta: 1 });

const NOTES_KEY = "sonar-sweep-notes";
try { $("#sw-log").value = localStorage.getItem(NOTES_KEY) || ""; } catch (_) { /* storage blocked */ }
const saveNotes = () => { try { localStorage.setItem(NOTES_KEY, $("#sw-log").value); } catch (_) { /* ignore */ } };
$("#sw-log").oninput = saveNotes;
function addNote() {
  const note = $("#sw-note").value.trim();
  if (!note) return;
  const where = state && state.sweep ? state.sweep.label : "no sweep";
  const log = $("#sw-log");
  log.value += (log.value && !log.value.endsWith("\n") ? "\n" : "") + `${where}: ${note}\n`;
  $("#sw-note").value = "";
  saveNotes();
}
$("#sw-addnote").onclick = addNote;
$("#sw-note").onkeydown = e => { if (e.key === "Enter") addNote(); };
$("#sw-copy").onclick = () => { $("#sw-log").select(); navigator.clipboard?.writeText($("#sw-log").value); };
$("#sw-clear").onclick = () => { $("#sw-log").value = ""; saveNotes(); };

function renderSweep() {
  const s = state.sweep;
  $("#sw-now").textContent = s ? s.label : "nothing (normal output)";
  $("#sw-next").textContent = s ? `Step ${s.index + 1} of ${s.count}` + (s.manual ? "" : ` · ${s.remaining_s.toFixed(1)} s left`) + (s.next ? ` · next: ${s.next}` : "") : "";
  $("#sw-prev").disabled = $("#sw-nextb").disabled = !s || !s.manual;
  $("#sw-stop").disabled = !s;
}

// ---------- setup ----------
function renderSetup() {
  for (const [id, v] of [["#target", state.target_ip], ["#fps", state.fps]]) {
    const el = $(id);
    if (document.activeElement !== el && !el.dataset.edited) el.value = v;
  }
}
["#target", "#fps"].forEach(id => $(id).oninput = () => { $(id).dataset.edited = "1"; });
$("#savecfg").onclick = async () => {
  await api("/api/config", { target_ip: $("#target").value, fps: +$("#fps").value });
  delete $("#target").dataset.edited; delete $("#fps").dataset.edited;
  refreshDiag();
};
$("#pollnow").onclick = async () => { await api("/api/poll", {}); setTimeout(refreshDiag, 800); };

function renderDiag() {
  const pill = $("#status");
  if (!diag) { pill.className = "pill bad"; pill.textContent = "app not responding"; return; }
  if (diag.an2_online) { pill.className = "pill ok"; pill.textContent = `AN2 online (${diag.target})`; }
  else if (diag.artnet_ip_ok) { pill.className = "pill mid"; pill.textContent = `On ${diag.interface_network}, ${diag.target} not answering`; }
  else { pill.className = "pill bad"; pill.textContent = `No network for ${diag.target}: set USB Ethernet to 2.0.0.2 / 255.0.0.0`; }
  const rows = [
    ["Node online (ArtPoll)", diag.an2_online ? "yes" : "no"],
    ["Mac on node's subnet", diag.artnet_ip_ok ? `yes (${diag.interface_network})` : "no"],
    ["Local IPs", diag.local_ips.join(", ") || "none"],
    ["Target", diag.target],
    ["Listening on UDP 6454", diag.bound_port_6454 ? "yes" : "no (another Art-Net app is open?)"],
    ["Last send error", diag.send_error || "none"],
    ["Config file", state ? state.config_path : ""],
  ];
  $("#diag").innerHTML = rows.map(([k, v]) => `<dt>${esc(k)}</dt><dd>${esc(v)}</dd>`).join("");
  $("#nodes").innerHTML = diag.nodes.length ? diag.nodes.map(n => `<div class="row"><b>${esc(n.ip)}</b> ${esc(n.short_name)} <span class="meta">${esc(n.long_name)}</span>
    ${n.ip !== diag.target ? `<button data-ip="${esc(n.ip)}">Use this node</button>` : `<span class="meta">(current target)</span>`}</div>`).join("") : `<p class="hint">None yet.</p>`;
  $$("#nodes button[data-ip]").forEach(b => b.onclick = async () => { await api("/api/config", { target_ip: b.dataset.ip }); refreshDiag(); });
}

async function refreshDiag() {
  try { diag = await (await fetch("/api/diag")).json(); } catch (_) { diag = null; }
  renderDiag();
}

// ---------- boot ----------
async function refreshState() {
  try { await api("/api/state"); } catch (_) { /* banner shown */ }
  setTimeout(refreshState, state && (state.sweep || state.test_running) ? 250 : 1000);
}

(async function boot() {
  profiles = await (await fetch("/api/profiles")).json();
  buildMixer();
  await refreshState();
  refreshDiag();
  setInterval(refreshDiag, 3000);
})();
