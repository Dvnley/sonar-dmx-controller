"use strict";

// Show tab: one calm screen for running the night. Pick a light at the top
// (or Everything), and the panel below only shows what that light can do, so
// it is always clear which light a control changes.

const SHOW_COLORS = [
  ["Red", "#ff3b3b", [255, 0, 0]], ["Orange", "#ff8a2a", [255, 80, 0]], ["Yellow", "#ffd93a", [255, 180, 0]],
  ["Green", "#36d65a", [0, 255, 0]], ["Cyan", "#36d9f5", [0, 255, 255]], ["Blue", "#3f63ff", [0, 0, 255]],
  ["Purple", "#9147ff", [120, 0, 255]], ["Pink", "#ff5cc6", [255, 0, 160]], ["White", "#ffffff", [255, 255, 255]],
];
const SHOW_SIZES = [["Small", 30], ["Medium", 60], ["Big", 100]];
const COLOR_TYPES = ["red", "green", "blue", "amber", "white", "uv"];

let showPrograms = [], showPalettes = [];
let showVar = loadPref("showvar", 0);
let showSize = loadPref("showsize", 60);
let shSel = loadPref("showsel", "all");
let showSig = "";
let rigArmed = false;
let speedTouched = 0;

fetch("/api/programs").then(r => r.json()).then(d => { showPrograms = d.programs; showPalettes = d.palettes; showSig = ""; if (state) renderShow(); });

// ---------- light groups ----------
// Lights of the same type are one group, like the scanner buttons on a
// hardware desk.
function showGroups() {
  const byProfile = new Map();
  for (const f of state.fixtures) {
    if (!byProfile.has(f.profile)) byProfile.set(f.profile, []);
    byProfile.get(f.profile).push(f);
  }
  const out = [];
  for (const [profile, list] of byProfile) {
    const f0 = list[0];
    const name = list.length > 1 ? `${f0.name.replace(/\s*\d+$/, "")}s` : f0.name;
    const lo = Math.min(...list.map(f => f.address)), hi = Math.max(...list.map(f => f.end));
    out.push({
      key: profile, name, list, idx: list.map(f => f.index),
      color: list.some(f => f.channels.some(c => COLOR_TYPES.includes(c.type))),
      moving: list.some(f => f.channels.some(c => c.type === "pan" || c.type === "tilt")),
      on: list.some(f => f.enabled),
      where: list.length > 1 ? `DMX ${lo}–${hi}` : `DMX ${lo}`,
    });
  }
  return out.sort((a, b) => (b.on - a.on) || (a.list[0].address - b.list[0].address));
}

function showRunFor(g, list) { return list.find(r => r.fixtures.some(i => g.idx.includes(i))); }
function progName(id) { return (showPrograms.find(p => p.id === id) || { name: id }).name; }
function moveName(id) { return (state.show.movements.find(m => m.id === id) || { name: id }).name; }

// glow is the light's color right now; lights with no color mixing glow white
// as bright as they are.
function glow(i) {
  const f = state.fixtures[i];
  if (!f || !f.enabled || state.blackout) return "#1a1d24";
  if (f.channels.some(c => COLOR_TYPES.includes(c.type))) return (state.perform.live || [])[i] || "#1a1d24";
  const d = f.channels.findIndex(c => c.type === "dimmer");
  const lvl = (d >= 0 ? f.values[d] / 255 : 1) * f.brightness / 100 * state.master / 100;
  return `rgba(255,255,255,${Math.max(0.1, lvl).toFixed(2)})`;
}

function speedWord(s) {
  return s < 15 ? "Very slow" : s < 40 ? "Slow" : s < 62 ? "Medium" : s < 85 ? "Fast" : "Very fast";
}

// what a light is doing, in a few words
function doing(g) {
  if (!g.on) return "Off";
  const run = showRunFor(g, state.perform.runs), mv = showRunFor(g, state.show.moves);
  const parts = [];
  if (run) parts.push(progName(run.program));
  if (mv) parts.push(moveName(mv.pattern));
  return parts.join(" · ") || "Still";
}

// ---------- actions ----------
function showSpeed() { return Date.now() - speedTouched < 3000 ? +$("#sh-speed").value : state.show.speed; }
function runOpts() { return { speed: showSpeed(), music: $("#sh-music").checked }; }

function startProgram(fixtures, program) {
  return api("/api/program", { fixtures, program, size: 60, variation: showVar, ...runOpts() });
}
function startMove(fixtures, pattern) {
  return api("/api/movement", { fixtures, pattern, size: showSize, ...runOpts() });
}
function sendShowColor(fixtures, name) {
  const [, , [r, g, b]] = SHOW_COLORS.find(c => c[0] === name);
  const white = name === "White";
  return api("/api/color", { fixtures, r: white ? 0 : r, g: white ? 0 : g, b: white ? 0 : b, w: white ? 255 : 0, a: 0, uv: 0, fade: 0.3 });
}

$("#sh-blackout").onclick = () => api("/api/blackout", { on: !state.blackout });
$("#sh-master").oninput = e => {
  $("#sh-masterv").textContent = e.target.value + "%";
  throttled("master", () => api("/api/master", { value: +e.target.value }));
};
$("#sh-speed").oninput = e => {
  speedTouched = Date.now();
  $("#sh-speedword").textContent = speedWord(+e.target.value);
  throttled("showspeed", () => api("/api/show/speed", runOpts()));
};
$("#sh-music").onchange = () => {
  speedTouched = Date.now();
  $("#sh-tap").classList.toggle("hidden", !$("#sh-music").checked);
  api("/api/show/speed", runOpts());
};
$("#sh-tap").onpointerdown = e => { e.preventDefault(); fetch("/api/beat", { method: "POST", body: '{"source":"tap"}' }); };
$("#sh-strobe").onclick = () => api("/api/strobe", { hz: state.perform.strobe_hz ? 0 : 8 });
$("#sh-stopall").onclick = async () => {
  await api("/api/program/stop", { fixtures: "all" });
  await api("/api/movement/stop", { fixtures: "all" });
  await api("/api/strobe", { hz: 0 });
};

(function flash() {
  const b = $("#sh-flash");
  let held = false;
  const down = e => { e.preventDefault(); if (held) return; held = true; b.classList.add("on"); api("/api/flash", { on: true }); };
  const up = () => { if (!held) return; held = false; b.classList.remove("on"); api("/api/flash", { on: false }); };
  b.onpointerdown = e => { b.setPointerCapture(e.pointerId); down(e); };
  b.onpointerup = b.onpointercancel = b.onlostpointercapture = up;
  b.oncontextmenu = e => e.preventDefault();
  window.addEventListener("blur", up);
})();

// ---------- render ----------
function renderShow() {
  if (tab !== "show" || !state || !state.show) return;
  const sh = state.show, p = state.perform;

  // things that change often are updated in place
  const m = $("#sh-master");
  if (!dragging || document.activeElement !== m) { m.value = state.master; $("#sh-masterv").textContent = state.master + "%"; }
  if (Date.now() - speedTouched > 3000) {
    $("#sh-speed").value = sh.speed;
    $("#sh-music").checked = sh.music;
    $("#sh-speedword").textContent = speedWord(sh.speed);
    $("#sh-tap").classList.toggle("hidden", !sh.music);
  }
  const bpm = $("#sh-bpm");
  bpm.textContent = sh.music ? (p.beat.live ? `${Math.round(p.beat.bpm)} BPM` : "waiting for a beat") : "";
  const bo = $("#sh-blackout");
  bo.classList.toggle("on", state.blackout);
  bo.textContent = state.blackout ? "Lights are off · tap to bring back" : "Blackout";
  $("#sh-strobe").classList.toggle("on", !!p.strobe_hz);
  $$("#tab-show .sh-glow").forEach(el => el.style.setProperty("--lc", glow(+el.dataset.i)));

  const groups = showGroups();
  if (shSel !== "all" && !groups.some(g => g.key === shSel)) shSel = "all";
  const sig = JSON.stringify([groups.map(g => [g.key, g.name, g.on, g.list.map(f => [f.brightness, f.values, f.enabled])]),
    p.runs.map(r => [r.program, r.fixtures, r.variation]), sh.moves.map(r => [r.pattern, r.fixtures, r.size]),
    sh.rig_ok, rigArmed, showVar, showSize, shSel, showPrograms.length]);
  if (sig === showSig || dragging) return;
  showSig = sig;

  renderRig(sh);
  renderLights(groups);
  const panel = $("#sh-panel");
  if (shSel === "all") renderEverything(panel, groups);
  else renderLight(panel, groups.find(g => g.key === shSel));
}

function renderRig(sh) {
  const el = $("#sh-rig");
  el.classList.toggle("hidden", sh.rig_ok);
  if (sh.rig_ok) return;
  el.innerHTML = `<div><b>Set the app up for your lights?</b>
    <p>Spots on DMX 1, Chauvet wash on 17, Spider on 34. The uplights get switched off.</p></div>
    <button id="sh-loadrig" class="${rigArmed ? "danger" : "primary"}">${rigArmed ? "Tap again to confirm" : "Set up my lights"}</button>
    ${rigArmed ? `<button id="sh-rigcancel">Cancel</button>` : ""}`;
  $("#sh-loadrig").onclick = async () => {
    if (!rigArmed) { rigArmed = true; showSig = ""; renderShow(); return; }
    rigArmed = false;
    await api("/api/show/setup", {});
    profiles = await (await fetch("/api/profiles")).json();
  };
  if (rigArmed) $("#sh-rigcancel").onclick = () => { rigArmed = false; showSig = ""; renderShow(); };
}

function renderLights(groups) {
  const el = $("#sh-lights");
  const anyOn = groups.filter(g => g.on);
  el.innerHTML = `<button class="sh-light ${shSel === "all" ? "sel" : ""}" data-k="all">
      <span class="sh-dots">${anyOn.slice(0, 3).map(g => `<i class="sh-glow" data-i="${g.list[0].index}" style="--lc:${glow(g.list[0].index)}"></i>`).join("")}</span>
      <span class="sh-lname">Everything</span><span class="sh-ldo">All lights together</span></button>` +
    groups.map(g => `<button class="sh-light ${shSel === g.key ? "sel" : ""} ${g.on ? "" : "off"}" data-k="${esc(g.key)}">
      <i class="sh-glow" data-i="${g.list[0].index}" style="--lc:${glow(g.list[0].index)}"></i>
      <span class="sh-lname">${esc(g.name)}</span><span class="sh-ldo">${esc(doing(g))}</span></button>`).join("");
  $$(".sh-light", el).forEach(b => b.onclick = () => { shSel = b.dataset.k; savePref("showsel", shSel); showSig = ""; renderShow(); });
}

// a row of choices where one is lit
function choices(name, items, current) {
  return `<div class="sh-choices" data-name="${name}">${items.map(([label, v]) =>
    `<button class="${String(v) === String(current) ? "on" : ""}" data-v="${esc(v)}">${esc(label)}</button>`).join("")}</div>`;
}
function onChoice(el, name, fn) { $$(`.sh-choices[data-name="${name}"] button`, el).forEach(b => b.onclick = () => fn(b.dataset.v)); }

function swatches() {
  return `<div class="sh-swatches">${SHOW_COLORS.map(([n, css]) => `<button class="sh-sw" data-color="${n}" style="--pc:${css}" title="${n}" aria-label="${n}"></button>`).join("")}</div>`;
}

function section(title, body, note) {
  return `<section class="sh-sec"><h3>${title}${note ? ` <small>${note}</small>` : ""}</h3>${body}</section>`;
}

function renderEverything(panel, groups) {
  const p = state.perform, sh = state.show;
  const colorGroups = groups.filter(g => g.color && g.on), moveGroups = groups.filter(g => g.moving && g.on);
  const colorIdx = colorGroups.flatMap(g => g.idx), moveIdx = moveGroups.flatMap(g => g.idx);
  const progs = colorGroups.map(g => (showRunFor(g, p.runs) || {}).program || "");
  const moves = moveGroups.map(g => (showRunFor(g, sh.moves) || {}).pattern || "");
  const same = list => list.length && list.every(x => x === list[0]) ? list[0] : "?";
  const curProg = same(progs), curMove = same(moves);

  let html = `<div class="sh-phead"><div><h2>Everything</h2><p>These buttons change all your lights at once.</p></div></div>`;
  if (colorIdx.length) {
    html += section("Color", swatches());
    html += section("Effect", choices("fx", [["None", ""], ...showPrograms.map(x => [x.name, x.id])], curProg));
    if (curProg && curProg !== "?") html += section("Effect colors", choices("pal", showPalettes.map((pl, i) => [pl.name, i]), showVar));
  }
  if (moveIdx.length) {
    html += section("Movement", choices("mv", [["None", ""], ...sh.movements.map(x => [x.name, x.id])], curMove));
    if (curMove && curMove !== "?") html += section("How far they move", choices("size", SHOW_SIZES, showSize));
  }
  if (!colorIdx.length && !moveIdx.length) html += `<p class="sh-empty">All lights are switched off. Pick a light above to turn it on.</p>`;
  panel.innerHTML = html;

  $$("[data-color]", panel).forEach(b => b.onclick = () => sendShowColor(colorIdx, b.dataset.color));
  onChoice(panel, "fx", v => v ? startProgram(colorIdx, v) : api("/api/program/stop", { fixtures: "all" }));
  onChoice(panel, "mv", v => v ? startMove(moveIdx, v) : api("/api/movement/stop", { fixtures: "all" }));
  wirePalSize(panel);
}

function wirePalSize(panel) {
  const p = state.perform, sh = state.show;
  onChoice(panel, "pal", v => {
    showVar = +v; savePref("showvar", showVar);
    p.runs.forEach(r => api("/api/program", { fixtures: r.fixtures, program: r.program, size: r.size, variation: showVar, speed: r.speed, music: r.music }));
    showSig = ""; renderShow();
  });
  onChoice(panel, "size", v => {
    showSize = +v; savePref("showsize", showSize);
    sh.moves.forEach(r => api("/api/movement", { fixtures: r.fixtures, pattern: r.pattern, size: showSize, speed: r.speed, music: r.music }));
    showSig = ""; renderShow();
  });
}

function chanSlider(g, ch, i, label) {
  const v = g.list[0].values[i];
  return `<label class="sh-slider"><span>${esc(label || ch.name)}<em>ch ${i + 1}</em></span>
    <input type="range" min="0" max="255" value="${v}" data-ch="${i}"><b>${v}</b></label>`;
}

function renderLight(panel, g) {
  const p = state.perform, sh = state.show, f0 = g.list[0], chans = f0.channels;
  const run = showRunFor(g, p.runs), move = showRunFor(g, sh.moves);
  let html = `<div class="sh-phead">
      <i class="sh-glow big" data-i="${f0.index}" style="--lc:${glow(f0.index)}"></i>
      <div><h2>${esc(g.name)}</h2><p>${esc(g.where)}${g.list.length > 1 ? ` · ${g.list.length} lights` : ""}</p></div>
      <button class="sh-switch ${g.on ? "on" : ""}" data-onoff><span></span>${g.on ? "On" : "Off"}</button>
    </div>`;
  if (!g.on) {
    html += `<p class="sh-empty">This light is switched off.${g.key === "freedom-flex-h4-ip" ? " Its addresses overlap your other lights, so leave it off unless you re-patch it." : ""}</p>`;
    panel.innerHTML = html;
    $("[data-onoff]", panel).onclick = () => api("/api/show/group", { fixtures: g.idx, on: true });
    return;
  }
  html += `<label class="sh-slider sh-level"><span>Brightness</span><input type="range" min="0" max="100" value="${f0.brightness}" data-level><b>${f0.brightness}%</b></label>`;
  if (g.color) {
    html += section("Color", swatches());
    html += section("Effect", choices("fx", [["None", ""], ...showPrograms.map(x => [x.name, x.id])], run ? run.program : ""));
    if (run) html += section("Effect colors", choices("pal", showPalettes.map((pl, i) => [pl.name, i]), showVar));
  }
  const wheel = chans.findIndex(c => c.type === "color"), gobo = chans.findIndex(c => c.type === "gobo");
  if (wheel >= 0 || gobo >= 0) {
    html += section("Color and pattern", (wheel >= 0 ? chanSlider(g, chans[wheel], wheel, "Color wheel") : "") + (gobo >= 0 ? chanSlider(g, chans[gobo], gobo, "Pattern (gobo)") : ""), "slide until you like it");
  }
  if (g.moving) {
    html += section("Movement", choices("mv", [["None", ""], ...sh.movements.map(x => [x.name, x.id])], move ? move.pattern : ""));
    if (move) html += section("How far it moves", choices("size", SHOW_SIZES, showSize));
    let aim = "";
    chans.forEach((ch, i) => { if (ch.type === "pan" || ch.type === "tilt") aim += chanSlider(g, ch, i); });
    html += section("Where it points", aim + `<button class="sh-link" data-center>Point at the middle</button>`, move ? "it moves around this spot" : "");
  }
  const guess = f0.profile === "spider-10ch" ? "These channels are a best guess for 10-channel spiders. If one does the wrong thing, tell me what it does."
    : f0.profile === "gobo-spot-11ch" ? "Channels 1, 3 and 6 are checked. The others are the usual layout for these spots." : "";
  html += `<details class="sh-more"><summary>Every channel (${chans.length})</summary>${guess ? `<p class="hint">${guess}</p>` : ""}${chans.map((ch, i) => chanSlider(g, ch, i)).join("")}</details>`;
  panel.innerHTML = html;

  $("[data-onoff]", panel).onclick = () => api("/api/show/group", { fixtures: g.idx, on: false });
  const lv = $("[data-level]", panel);
  lv.oninput = () => { lv.nextElementSibling.textContent = lv.value + "%"; throttled("lvl" + g.key, () => api("/api/show/group", { fixtures: g.idx, level: +lv.value })); };
  $$("[data-color]", panel).forEach(b => b.onclick = () => sendShowColor(g.idx, b.dataset.color));
  onChoice(panel, "fx", v => v ? startProgram(g.idx, v) : api("/api/program/stop", { fixtures: g.idx }));
  onChoice(panel, "mv", v => v ? startMove(g.idx, v) : api("/api/movement/stop", { fixtures: g.idx }));
  wirePalSize(panel);
  const setCh = (i, v) => Promise.all(g.idx.map(fi => api("/api/fixture", { index: fi, values: { [i + 1]: v } })));
  $$("input[data-ch]", panel).forEach(inp => inp.oninput = () => {
    $$(`input[data-ch="${inp.dataset.ch}"]`, panel).forEach(o => { o.value = inp.value; o.nextElementSibling.textContent = inp.value; });
    throttled(`ch${g.key}:${inp.dataset.ch}`, () => setCh(+inp.dataset.ch, +inp.value));
  });
  const c = $("[data-center]", panel);
  if (c) c.onclick = () => {
    const vals = {};
    chans.forEach((ch, i) => { if (ch.type === "pan" || ch.type === "tilt") vals[i + 1] = 128; });
    Promise.all(g.idx.map(fi => api("/api/fixture", { index: fi, values: vals })));
  };
}
