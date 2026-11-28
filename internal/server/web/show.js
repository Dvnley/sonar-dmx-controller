"use strict";

// Show tab: one screen for running the night. Each light type gets its own
// box, so it is always clear which light a control changes.

const SHOW_COLORS = [
  ["Red", "#ff2a2a", [255, 0, 0]], ["Orange", "#ff7a1a", [255, 80, 0]], ["Yellow", "#ffe03a", [255, 180, 0]],
  ["Green", "#2bd94a", [0, 255, 0]], ["Cyan", "#2be0ff", [0, 255, 255]], ["Blue", "#2f5bff", [0, 0, 255]],
  ["Purple", "#8a2bff", [120, 0, 255]], ["Pink", "#ff4fc0", [255, 0, 160]], ["White", "#ffffff", [255, 255, 255]],
];
const SHOW_STROBES = [["Off", 0], ["Slow", 3], ["Fast", 10]];
const SHOW_SIZES = [["A little", 30], ["Medium", 60], ["A lot", 100]];
const COLOR_TYPES = ["red", "green", "blue", "amber", "white", "uv"];

let showPrograms = [], showPalettes = [];
let showVar = loadPref("showvar", 0);
let showSize = loadPref("showsize", 60);
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
    const name = list.length > 1 ? `${f0.name.replace(/\s*\d+$/, "")}s ×${list.length}` : f0.name;
    const lo = Math.min(...list.map(f => f.address)), hi = Math.max(...list.map(f => f.end));
    out.push({
      key: profile, name, list, idx: list.map(f => f.index), profileName: f0.profile_name,
      color: list.some(f => f.channels.some(c => COLOR_TYPES.includes(c.type))),
      moving: list.some(f => f.channels.some(c => c.type === "pan" || c.type === "tilt")),
      on: list.some(f => f.enabled),
      where: list.length > 1 ? `DMX ${lo}–${hi}` : `DMX ${lo}`, chans: f0.channels.length,
    });
  }
  // lights that are switched on come first
  return out.sort((a, b) => (b.on - a.on) || (a.list[0].address - b.list[0].address));
}

function showRunFor(g, list) { return list.find(r => r.fixtures.some(i => g.idx.includes(i))); }

// glow is the light's color right now; lights with no color mixing glow white
// as bright as they are.
function glow(i) {
  const f = state.fixtures[i];
  if (!f || !f.enabled || state.blackout) return "#111";
  if (f.channels.some(c => COLOR_TYPES.includes(c.type))) return (state.perform.live || [])[i] || "#111";
  const d = f.channels.findIndex(c => c.type === "dimmer");
  const lvl = (d >= 0 ? f.values[d] / 255 : 1) * f.brightness / 100 * state.master / 100;
  return `rgba(255,255,255,${Math.max(0.08, lvl).toFixed(2)})`;
}

function speedWord(s) {
  return s < 15 ? "Very slow" : s < 40 ? "Slow" : s < 62 ? "Medium" : s < 85 ? "Fast" : "Very fast";
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

$("#sh-blackout").onclick = () => api("/api/blackout", { on: !state.blackout });
$("#sh-master").oninput = e => {
  $("#sh-masterv").textContent = e.target.value;
  throttled("master", () => api("/api/master", { value: +e.target.value }));
};
$("#sh-speed").oninput = e => {
  speedTouched = Date.now();
  $("#sh-speedword").textContent = speedWord(+e.target.value);
  throttled("showspeed", () => api("/api/show/speed", runOpts()));
};
$("#sh-music").onchange = () => { speedTouched = Date.now(); api("/api/show/speed", runOpts()); };
$("#sh-tap").onpointerdown = e => { e.preventDefault(); fetch("/api/beat", { method: "POST", body: '{"source":"tap"}' }); };
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
  if (!dragging || document.activeElement !== m) { m.value = state.master; $("#sh-masterv").textContent = state.master; }
  const sp = $("#sh-speed");
  if (Date.now() - speedTouched > 3000) {
    sp.value = sh.speed;
    $("#sh-music").checked = sh.music;
    $("#sh-speedword").textContent = speedWord(sh.speed);
  }
  const bpmEl = $("#sh-bpm");
  bpmEl.textContent = p.beat.live ? `${Math.round(p.beat.bpm)} BPM from ${p.beat.source}` : (sh.music ? "no beat yet: tap TAP to the music" : "");
  bpmEl.classList.toggle("hidden", !bpmEl.textContent);
  const bo = $("#sh-blackout");
  bo.classList.toggle("on", state.blackout);
  bo.textContent = state.blackout ? "BLACKOUT ON (tap to bring lights back)" : "BLACKOUT";
  $$("#sh-groups .shglow").forEach(el => el.style.setProperty("--lc", glow(+el.dataset.i)));

  const groups = showGroups();
  const sig = JSON.stringify([groups.map(g => [g.key, g.name, g.on, g.list.map(f => [f.brightness, f.values, f.enabled])]),
    p.runs.map(r => [r.program, r.fixtures, r.variation]), sh.moves.map(r => [r.pattern, r.fixtures, r.size]),
    p.strobe_hz, sh.rig_ok, rigArmed, showVar, showSize, showPrograms.length, state.conflicts.length]);
  if (sig === showSig || dragging) return;
  showSig = sig;

  renderRig(sh);
  chips($("#sh-strobe"), SHOW_STROBES, p.strobe_hz, v => api("/api/strobe", { hz: +v }));

  // whole-rig program and movement: lit when every group of that kind runs it
  const colorGroups = groups.filter(g => g.color && g.on), moveGroups = groups.filter(g => g.moving && g.on);
  const allRun = id => colorGroups.length && colorGroups.every(g => (showRunFor(g, p.runs) || {}).program === id);
  const allMove = id => moveGroups.length && moveGroups.every(g => (showRunFor(g, sh.moves) || {}).pattern === id);
  const on = (list, idx) => list.filter(g => g.on).flatMap(g => g.idx);
  $("#sh-progs").innerHTML = showPrograms.map(pr => `<button class="shprog ${allRun(pr.id) ? "on" : ""}" data-p="${pr.id}">${esc(pr.name)}</button>`).join("");
  $$("#sh-progs .shprog").forEach(b => b.onclick = () => startProgram(on(colorGroups), b.dataset.p));
  $("#sh-moves").innerHTML = `<button class="shprog ${moveGroups.length && !moveGroups.some(g => showRunFor(g, sh.moves)) ? "on" : ""}" data-m="">Still</button>` +
    sh.movements.map(mv => `<button class="shprog ${allMove(mv.id) ? "on" : ""}" data-m="${mv.id}">${esc(mv.name)}</button>`).join("");
  $$("#sh-moves .shprog").forEach(b => b.onclick = () => b.dataset.m ? startMove(on(moveGroups), b.dataset.m) : api("/api/movement/stop", { fixtures: "all" }));
  chips($("#sh-colors"), showPalettes.map((pl, i) => [pl.name, i]), showVar, v => {
    showVar = +v; savePref("showvar", showVar);
    // re-start the running programs with the new colors
    p.runs.forEach(r => api("/api/program", { fixtures: r.fixtures, program: r.program, size: r.size, variation: showVar, speed: r.speed, music: r.music }));
    showSig = ""; renderShow();
  });
  chips($("#sh-size"), SHOW_SIZES, showSize, v => {
    showSize = +v; savePref("showsize", showSize);
    sh.moves.forEach(r => api("/api/movement", { fixtures: r.fixtures, pattern: r.pattern, size: showSize, speed: r.speed, music: r.music }));
    showSig = ""; renderShow();
  });

  $("#sh-groups").innerHTML = groups.map((g, gi) => groupCard(g, gi, p, sh)).join("");
  groups.forEach((g, gi) => wireGroup(g, $(`#sh-groups .shgroup[data-g="${gi}"]`), p, sh));
}

function renderRig(sh) {
  const el = $("#sh-rig");
  el.classList.toggle("hidden", sh.rig_ok);
  if (sh.rig_ok) return;
  el.innerHTML = `<div class="fhead"><b>Set the app up for your lights?</b></div>
    <p>This loads your rig: <b>Spots at DMX 1</b>, <b>Chauvet wash at DMX 17</b> and <b>Spider at DMX 34</b>, and switches the uplights off so they don't clash. It replaces what is in the Patch tab now.</p>
    <button id="sh-loadrig" class="big ${rigArmed ? "danger" : "primary"}">${rigArmed ? "Tap again to replace the patch" : "Load my rig"}</button>
    ${rigArmed ? `<button id="sh-rigcancel">Cancel</button>` : ""}`;
  $("#sh-loadrig").onclick = async () => {
    if (!rigArmed) { rigArmed = true; showSig = ""; renderShow(); return; }
    rigArmed = false;
    await api("/api/show/setup", {});
    profiles = await (await fetch("/api/profiles")).json();
  };
  if (rigArmed) $("#sh-rigcancel").onclick = () => { rigArmed = false; showSig = ""; renderShow(); };
}

function chanSlider(g, ch, i, label) {
  const v = g.list[0].values[i];
  return `<label class="shslider"><span>${esc(label || ch.name)} <em>ch ${i + 1}</em></span>
    <input type="range" min="0" max="255" value="${v}" data-ch="${i}"><b>${v}</b></label>`;
}

function groupCard(g, gi, p, sh) {
  const f0 = g.list[0];
  const run = showRunFor(g, p.runs), move = showRunFor(g, sh.moves);
  const level = f0.brightness;
  const chans = f0.channels;
  let body = "";
  if (g.on) {
    body += `<label class="shslider shlevel"><span>Brightness</span><input type="range" min="0" max="100" value="${level}" data-level><b>${level}%</b></label>`;
    if (g.color) {
      body += `<div class="shsub">Color</div><div class="shswatches">${SHOW_COLORS.map(([n, css]) => `<button class="shsw" data-color="${n}" style="--pc:${css}" title="${n}"><span>${n}</span></button>`).join("")}</div>`;
      body += `<div class="shsub">Effect</div><div class="chips shfx"><button class="chip ${run ? "" : "on"}" data-fx="">Still</button>${showPrograms.map(pr => `<button class="chip ${run && run.program === pr.id ? "on" : ""}" data-fx="${pr.id}">${esc(pr.name)}</button>`).join("")}</div>`;
    }
    chans.forEach((ch, i) => {
      if (ch.type === "color" || ch.type === "gobo") {
        body += `<div class="shsub">${ch.type === "color" ? "Color wheel" : "Gobo (pattern)"}</div>`;
        if (ch.stops && ch.stops.length) body += `<div class="chips">${ch.stops.map(st => `<button class="chip ${f0.values[i] === st.value ? "on" : ""}" data-ch="${i}" data-v="${st.value}">${esc(st.name)}</button>`).join("")}</div>`;
        body += chanSlider(g, ch, i, "Position");
      }
    });
    if (g.moving) {
      body += `<div class="shsub">Movement</div><div class="chips shmv"><button class="chip ${move ? "" : "on"}" data-mv="">Still</button>${sh.movements.map(mv => `<button class="chip ${move && move.pattern === mv.id ? "on" : ""}" data-mv="${mv.id}">${esc(mv.name)}</button>`).join("")}</div>`;
      body += `<div class="shsub">Aim${move ? " <small>(it moves around this point)</small>" : ""}</div>`;
      chans.forEach((ch, i) => { if (ch.type === "pan" || ch.type === "tilt") body += chanSlider(g, ch, i); });
      body += `<button class="shcenter" data-center>Aim at the middle</button>`;
    }
    body += `<details class="shall"><summary>Every channel of this light (${chans.length})</summary>${chans.map((ch, i) => chanSlider(g, ch, i)).join("")}</details>`;
    if (f0.profile === "spider-10ch") body += `<p class="hint">The spider's channels are a best guess for 10-channel spiders. If something does the wrong thing, check it in Channel sweep and tell me what each channel does.</p>`;
    if (f0.profile === "gobo-spot-11ch") body += `<p class="hint">Checked: ch 1 up/down, ch 3 left/right, ch 6 brightness. The color wheel, gobo and strobe channels are the usual layout for these spots, not checked yet.</p>`;
  } else {
    const clash = state.conflicts.length && g.key === "freedom-flex-h4-ip" ? " Their addresses overlap your other lights, so they are off." : "";
    body = `<p class="hint">Switched off.${clash}</p>`;
  }
  return `<div class="shgroup ${g.on ? "" : "off"}" data-g="${gi}">
    <div class="shhead">
      <i class="shglow" data-i="${f0.index}" style="--lc:${glow(f0.index)}"></i>
      <div class="shname"><b>${esc(g.name)}</b><span>${esc(g.where)} · ${g.chans} channels · ${esc(g.profileName)}</span></div>
      <button class="shonoff ${g.on ? "on" : ""}" data-onoff>${g.on ? "ON" : "OFF"}</button>
    </div>${body}</div>`;
}

function wireGroup(g, el, p, sh) {
  $("[data-onoff]", el).onclick = () => api("/api/show/group", { fixtures: g.idx, on: !g.on });
  if (!g.on) return;
  const lv = $("[data-level]", el);
  lv.oninput = () => { lv.nextElementSibling.textContent = lv.value + "%"; throttled("lvl" + g.key, () => api("/api/show/group", { fixtures: g.idx, level: +lv.value })); };
  $$("[data-color]", el).forEach(b => b.onclick = () => {
    const [, , [r, gg, bb]] = SHOW_COLORS.find(c => c[0] === b.dataset.color);
    const white = b.dataset.color === "White";
    api("/api/color", { fixtures: g.idx, r: white ? 0 : r, g: white ? 0 : gg, b: white ? 0 : bb, w: white ? 255 : 0, a: 0, uv: 0, fade: 0.3 });
  });
  $$("[data-fx]", el).forEach(b => b.onclick = () => b.dataset.fx ? startProgram(g.idx, b.dataset.fx) : api("/api/program/stop", { fixtures: g.idx }));
  $$("[data-mv]", el).forEach(b => b.onclick = () => b.dataset.mv ? startMove(g.idx, b.dataset.mv) : api("/api/movement/stop", { fixtures: g.idx }));
  const setCh = (i, v) => Promise.all(g.idx.map(fi => api("/api/fixture", { index: fi, values: { [i + 1]: v } })));
  $$("input[data-ch]", el).forEach(inp => inp.oninput = () => {
    inp.nextElementSibling.textContent = inp.value;
    // keep the same channel's other sliders (in "every channel") in step
    $$(`input[data-ch="${inp.dataset.ch}"]`, el).forEach(o => { if (o !== inp) { o.value = inp.value; o.nextElementSibling.textContent = inp.value; } });
    throttled(`ch${g.key}:${inp.dataset.ch}`, () => setCh(+inp.dataset.ch, +inp.value));
  });
  $$("button[data-ch]", el).forEach(b => b.onclick = () => setCh(+b.dataset.ch, +b.dataset.v));
  const c = $("[data-center]", el);
  if (c) c.onclick = () => {
    const vals = {};
    g.list[0].channels.forEach((ch, i) => { if (ch.type === "pan" || ch.type === "tilt") vals[i + 1] = 128; });
    Promise.all(g.idx.map(fi => api("/api/fixture", { index: fi, values: vals })));
  };
}
