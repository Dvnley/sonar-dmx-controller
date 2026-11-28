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

$("#sh-phone").onclick = () => { shSel = shSel === "phone" ? "all" : "phone"; savePref("showsel", shSel); showSig = ""; loadPhoneInfo(); renderShow(); };
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

  // say plainly when the lights can't be reached
  const conn = $("#sh-conn");
  const lost = diag && !diag.an2_online;
  conn.classList.toggle("hidden", !lost);
  if (lost) {
    conn.className = "fd-conn " + (diag.artnet_ip_ok ? "mid" : "bad");
    conn.innerHTML = diag.artnet_ip_ok
      ? `<b>The AN2 (${esc(diag.target)}) is not answering,</b> so the lights won't get anything. Check it has power and its network cable is in.`
      : `<b>The Mac can't reach the AN2,</b> so the lights won't get anything. Plug in the USB network adapter and set it to <code>2.0.0.2</code> / <code>255.0.0.0</code> in System Settings › Network.`;
  }

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
  if (!["all", "schedule", "phone"].includes(shSel) && !groups.some(g => g.key === shSel)) shSel = "all";
  const sig = JSON.stringify([groups.map(g => [g.key, g.name, g.on, g.list.map(f => [f.brightness, f.values, f.enabled])]),
    p.runs.map(r => [r.program, r.fixtures, r.variation]), sh.moves.map(r => [r.pattern, r.fixtures, r.size]),
    sh.rig_ok, rigArmed, showVar, showSize, shSel, showPrograms.length,
    shSel === "schedule" ? [state.schedule, state.moods.map(m => [m.id, m.name, m.colors]), lookDelArmed, schedDay, schedSel, tlDraft] : 0,
    shSel === "phone" ? phoneInfo : 0]);
  // don't rebuild the schedule while a time or look is being picked
  const editing = shSel === "schedule" && document.activeElement && document.activeElement.closest && document.activeElement.closest("#sh-panel") && /INPUT|SELECT/.test(document.activeElement.tagName);
  if (sig === showSig || dragging || editing) return;
  showSig = sig;

  renderRig(sh);
  renderLights(groups);
  const panel = $("#sh-panel");
  $("#sh-phone").classList.toggle("on", shSel === "phone");
  if (shSel === "phone") renderPhone(panel);
  else if (shSel === "schedule") renderSchedule(panel);
  else if (shSel === "all") renderEverything(panel, groups);
  else renderLight(panel, groups.find(g => g.key === shSel));
}

function renderRig(sh) {
  const el = $("#sh-rig");
  el.classList.toggle("hidden", sh.rig_ok);
  if (sh.rig_ok) return;
  el.innerHTML = `<div><b>Set the app up for your lights?</b>
    <p>Spots on DMX 1, Chauvet wash on 17, Spider on 34.</p></div>
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
      <span class="sh-lname">Everything</span><span class="sh-ldo">All lights</span></button>` +
    groups.map(g => `<button class="sh-light ${shSel === g.key ? "sel" : ""} ${g.on ? "" : "off"}" data-k="${esc(g.key)}">
      <i class="sh-glow" data-i="${g.list[0].index}" style="--lc:${glow(g.list[0].index)}"></i>
      <span class="sh-lname">${esc(g.name)}</span><span class="sh-ldo">${esc(doing(g))}</span></button>`).join("") +
    `<button class="sh-light sh-schedbtn ${shSel === "schedule" ? "sel" : ""}" data-k="schedule">
      <i class="sh-clock">◷</i><span class="sh-lname">Schedule</span>
      <span class="sh-ldo">${state.schedule.enabled ? (state.schedule.next ? "Next " + esc(state.schedule.next.clock.replace(/^\w+ /, "")) : "On") : "Off"}</span></button>`;
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
  const guess = f0.profile === "spider-10ch" ? "Checked on the rig: 2 red, 3 green, 4 blue, 5 white, 6 amber, 7 UV, 8 strobe, 9 party mode. Channels 1 and 10 are not known yet."
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

// ---------- schedule: a timeline of the night ----------
// The night runs from noon to noon, so a 1 AM "lights off" sits after the
// 9 PM party instead of at the start of the day.
const DAY_SETS = [["Every day", []], ["Mon–Fri", [1, 2, 3, 4, 5]], ["Fri & Sat", [5, 6]], ["Sat & Sun", [0, 6]], ["Only this day", null]];
const DAY_SHORT = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const NIGHT_START = 12 * 60; // noon
let lookDelArmed = "";
let addingRow = false; // kept for the render signature
let schedDay = new Date().getDay();
let tlDraft = null;   // entries being edited, saved a moment after the last change
let schedSaveT = 0;
let schedSel = -1;       // index into tlDraft of the line picked on the timeline

const toMin = t => { const [h, m] = t.split(":").map(Number); return h * 60 + m; };
const fromMin = m => { m = ((m % 1440) + 1440) % 1440; return `${String(Math.floor(m / 60)).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`; };
const nightPos = t => (toMin(t) - NIGHT_START + 1440) % 1440; // minutes since noon
function clockLabel(t) {
  const [h, m] = t.split(":").map(Number);
  return `${((h + 11) % 12) + 1}:${String(m).padStart(2, "0")} ${h < 12 ? "AM" : "PM"}`;
}
function daysLabel(days) {
  const d = (days || []).slice().sort().join(",");
  const hit = DAY_SETS.find(([, v]) => v && v.slice().sort().join(",") === d);
  return hit ? hit[0] : (days || []).map(x => DAY_SHORT[x]).join(", ");
}
// an entry plays on a night that starts on day d: times after midnight
// belong to the night before
function onNight(e, d) {
  const day = toMin(e.time) < NIGHT_START ? (d + 1) % 7 : d;
  return !e.days || !e.days.length || e.days.includes(day);
}
function moodColor2(id) {
  if (id === "off") return "#000";
  const m = (state.moods || []).find(x => x.id === id);
  const cs = (m && m.colors && m.colors.length ? m.colors : ["#555"]).slice(0, 4);
  return cs.length > 1 ? `linear-gradient(90deg, ${cs.join(",")})` : cs[0];
}
function moodLabel(id) { return id === "off" ? "Lights off" : ((state.moods || []).find(x => x.id === id) || { name: "?" }).name; }

function scheduleDraft() {
  if (!tlDraft) tlDraft = state.schedule.entries.map(e => ({ time: e.time, mood: e.mood, days: e.days ? [...e.days] : [], fade: e.fade }));
  return tlDraft;
}
function queueSave(now) {
  clearTimeout(schedSaveT);
  const go = () => {
    const sc = state.schedule;
    let tzName = "";
    try { tzName = Intl.DateTimeFormat().resolvedOptions().timeZone || ""; } catch (_) { /* ignore */ }
    const entries = tlDraft;
    api("/api/schedule", { enabled: sc.enabled, time_zone: sc.time_zone || tzName, entries })
      .then(() => { tlDraft = null; showSig = ""; renderShow(); })
      .catch(() => { tlDraft = null; });
  };
  if (now) go(); else schedSaveT = setTimeout(go, 600);
  showSig = ""; renderShow();
}

function renderSchedule(panel) {
  const sc = state.schedule, moods = state.moods || [];
  const draft = scheduleDraft();
  const tonight = draft.map((e, i) => ({ e, i })).filter(x => onNight(x.e, schedDay)).sort((a, b) => nightPos(a.e.time) - nightPos(b.e.time));
  const status = !sc.enabled ? "Off. The lights only change when you change them."
    : sc.next ? `Next: <b>${esc(sc.next.name)}</b> at ${esc(sc.next.clock)}` : "On. Add a look to the night below.";

  // timeline blocks: each look runs until the next one that night
  let blocks = "";
  tonight.forEach(({ e, i }, k) => {
    const a = nightPos(e.time), b = k + 1 < tonight.length ? nightPos(tonight[k + 1].e.time) : 1440;
    const left = a / 1440 * 100, width = Math.max(0.6, (b - a) / 1440 * 100);
    blocks += `<div class="tl-block ${i === schedSel ? "sel" : ""} ${e.mood === "off" ? "off" : ""}" data-i="${i}" style="left:${left}%;width:${width}%;--bg:${moodColor2(e.mood)}">
      <span>${esc(moodLabel(e.mood))}</span></div>
      <div class="tl-handle ${i === schedSel ? "sel" : ""}" data-i="${i}" style="left:${left}%" title="Drag to change the time"><b>${esc(clockLabel(e.time).replace(":00", ""))}</b></div>`;
  });
  let ticks = "";
  for (let h = 0; h <= 24; h += 2) {
    const t = fromMin(NIGHT_START + h * 60);
    ticks += `<span style="left:${h / 24 * 100}%">${h === 24 ? "" : clockLabel(t).replace(":00", "")}</span>`;
  }
  const nowD = new Date();
  const nowPos = ((nowD.getHours() * 60 + nowD.getMinutes()) - NIGHT_START + 1440) % 1440;
  const isTonight = schedDay === (nowD.getHours() * 60 + nowD.getMinutes() < NIGHT_START ? (nowD.getDay() + 6) % 7 : nowD.getDay());

  const lookOpts = id => `<option value="off" ${id === "off" ? "selected" : ""}>Lights off</option>` +
    moods.map(m => `<option value="${esc(m.id)}" ${m.id === id ? "selected" : ""}>${esc(m.name)}</option>`).join("");
  const rows = tonight.map(({ e, i }) => `<div class="sh-srow ${i === schedSel ? "sel" : ""}" data-i="${i}">
      <div class="tl-time"><button data-step="-15" title="15 minutes earlier">−</button>
        <label><input type="time" value="${esc(e.time)}" data-time><b>${esc(clockLabel(e.time))}</b></label>
        <button data-step="15" title="15 minutes later">+</button></div>
      <span class="tl-dot" style="background:${moodColor2(e.mood)}"></span>
      <select data-mood>${lookOpts(e.mood)}</select>
      <select data-days>${DAY_SETS.map(([n, v]) => `<option value="${n}" ${v && daysLabel(e.days) === n ? "selected" : ""}>${n === "Only this day" ? "Only " + DAY_SHORT[schedDay] + "s" : n}</option>`).join("")}
        ${DAY_SETS.some(([n, v]) => v && daysLabel(e.days) === n) ? "" : `<option selected value="custom">${esc(daysLabel(e.days))}</option>`}</select>
      <button class="sh-x" data-del title="Remove">✕</button>
    </div>`).join("");

  panel.innerHTML = `<div class="sh-phead"><div><h2>Schedule</h2><p>${status}</p></div>
      <button class="sh-switch ${sc.enabled ? "on" : ""}" data-sched><span></span>${sc.enabled ? "On" : "Off"}</button></div>
    <div class="tl-days">${DAY_SHORT.map((d, k) => `<button class="${k === schedDay ? "on" : ""}" data-day="${k}">${d}${k === nowD.getDay() ? " ·" : ""}</button>`).join("")}</div>
    <div class="tl">
      <div class="tl-track" id="tl-track">${blocks || `<p class="tl-empty">Nothing planned for ${DAY_SHORT[schedDay]} night. Add a look below.</p>`}
        ${isTonight ? `<i class="tl-now" style="left:${nowPos / 1440 * 100}%" title="Now"></i>` : ""}</div>
      <div class="tl-ticks">${ticks}</div>
    </div>
    <p class="hint sh-tight">Drag a time on the bar to move it, or use − and + below. Each look fades in over 30 seconds and plays until the next one.</p>
    <div class="sh-rows">${rows}</div>
    <div class="sh-addline">
      <select id="sh-addmood">${lookOpts(moods.length ? moods[0].id : "off")}</select>
      <button id="sh-addtime" class="primary">Add to ${DAY_SHORT[schedDay]} night</button>
    </div>
    ${section("Looks", `<p class="hint sh-tight">Set the lights how you like (colors, effects, movement, speed) and save it as a look. Tap a look to see it now.</p>
      <div class="sh-saveline"><input id="sh-lookname" placeholder="Name, e.g. Chill blue" maxlength="40"><button id="sh-savelook" class="primary">Save look</button></div>
      <div class="sh-looks">${moods.map(m => `<span class="sh-look"><i style="background:${moodColor2(m.id)}"></i>
        <button data-play="${esc(m.id)}" title="Show this look now">${esc(m.name)}</button>
        <button class="sh-x" data-forget="${esc(m.id)}" title="Delete this look">${lookDelArmed === m.id ? "Delete?" : "✕"}</button></span>`).join("")}</div>
      <button class="sh-link" id="sh-starters">+ Add ready-made looks (Ocean floor, Thunderstorm, Disco fever and more)</button>`)}
    <p class="hint sh-tight">The schedule runs while this app is open and the Mac is awake.</p>`;

  $("[data-sched]", panel).onclick = () => {
    let tzName = "";
    try { tzName = Intl.DateTimeFormat().resolvedOptions().timeZone || ""; } catch (_) { /* ignore */ }
    clearTimeout(schedSaveT);
    api("/api/schedule", { enabled: !sc.enabled, time_zone: sc.time_zone || tzName, entries: scheduleDraft() }).then(() => { tlDraft = null; });
  };
  $$("[data-day]", panel).forEach(b => b.onclick = () => { schedDay = +b.dataset.day; schedSel = -1; showSig = ""; renderShow(); });
  $$(".tl-block", panel).forEach(b => b.onclick = () => { schedSel = +b.dataset.i; showSig = ""; renderShow(); });

  // drag a handle along the bar to move that time, in 15 minute steps
  const track = $("#tl-track", panel);
  $$(".tl-handle", panel).forEach(h => {
    h.onpointerdown = ev => {
      ev.preventDefault();
      h.setPointerCapture(ev.pointerId);
      const i = +h.dataset.i;
      schedSel = i;
      dragging++;
      const move = e2 => {
        const r = track.getBoundingClientRect();
        const pos = Math.round(Math.min(1, Math.max(0, (e2.clientX - r.left) / r.width)) * 1440 / 15) * 15;
        const t = fromMin(NIGHT_START + Math.min(pos, 1440 - 15));
        if (t === draft[i].time) return;
        draft[i].time = t;
        h.style.left = (pos / 1440 * 100) + "%";
        $("b", h).textContent = clockLabel(t).replace(":00", "");
      };
      h.onpointermove = move;
      h.onpointerup = h.onpointercancel = () => { h.onpointermove = null; dragging = 0; queueSave(true); };
    };
  });

  $$(".sh-srow", panel).forEach(r => {
    const i = +r.dataset.i, e = draft[i];
    r.onclick = ev => { if (ev.target === r) { schedSel = i; showSig = ""; renderShow(); } };
    $$("[data-step]", r).forEach(b => b.onclick = () => { e.time = fromMin(toMin(e.time) + +b.dataset.step); schedSel = i; queueSave(); });
    $("[data-time]", r).onchange = ev => { if (ev.target.value) { e.time = ev.target.value; queueSave(true); } };
    $("[data-mood]", r).onchange = ev => { e.mood = ev.target.value; e.fade = e.mood === "off" ? 60 : 30; queueSave(true); };
    $("[data-days]", r).onchange = ev => {
      const hit = DAY_SETS.find(d => d[0] === ev.target.value);
      if (!hit) return;
      e.days = hit[1] ? [...hit[1]] : [toMin(e.time) < NIGHT_START ? (schedDay + 1) % 7 : schedDay];
      queueSave(true);
    };
    $("[data-del]", r).onclick = () => { draft.splice(i, 1); schedSel = -1; queueSave(true); };
  });

  $("#sh-addtime", panel).onclick = () => {
    const last = tonight.length ? tonight[tonight.length - 1].e.time : "19:00";
    const time = tonight.length ? fromMin(toMin(last) + 60) : "20:00";
    const mood = $("#sh-addmood", panel).value;
    draft.push({ time, mood, days: [], fade: mood === "off" ? 60 : 30 });
    schedSel = draft.length - 1;
    queueSave(true);
  };

  $("#sh-savelook", panel).onclick = async () => {
    const name = $("#sh-lookname", panel).value.trim();
    if (!name) { $("#sh-lookname", panel).focus(); return; }
    await api("/api/mood/save", { name });
    showSig = ""; renderShow();
  };
  $("#sh-lookname", panel).onkeydown = e => { if (e.key === "Enter") $("#sh-savelook", panel).click(); };
  $("#sh-starters", panel).onclick = () => api("/api/mood/starters", {});
  $$("[data-play]", panel).forEach(b => b.onclick = () => api("/api/mood/play", { id: b.dataset.play, fade: 2 }));
  $$("[data-forget]", panel).forEach(b => b.onclick = async () => {
    if (lookDelArmed !== b.dataset.forget) { lookDelArmed = b.dataset.forget; showSig = ""; renderShow(); return; }
    lookDelArmed = "";
    await api("/api/mood/delete", { id: b.dataset.forget });
  });
}

// ---------- phone ----------
let phoneInfo = null;
async function loadPhoneInfo() {
  try { phoneInfo = await (await fetch("/api/remote")).json(); } catch (_) { phoneInfo = null; }
  if (shSel === "phone") { showSig = ""; renderShow(); }
}

function renderPhone(panel) {
  const r = phoneInfo;
  let body;
  if (!r) body = `<p class="sh-empty">Checking…</p>`;
  else if (!r.pin && r.enabled) body = `<p class="sh-empty">You're on the phone remote now. Tip: tap Share, then <b>Add to Home Screen</b> to open it like an app.</p>`;
  else if (!r.enabled) body = `
    <ol class="ph-steps">
      <li>Quit the app (close its Terminal window).</li>
      <li>Start it with <b>START-WITH-PHONE.command</b> in the same folder instead.</li>
      <li>Come back here: the address and PIN for your phone will show up.</li>
    </ol>`;
  else body = `
    <ol class="ph-steps">
      <li>Put your phone on the <b>same Wi-Fi</b> as this Mac.<br><span class="hint">No Wi-Fi? Turn on your phone's <b>Personal Hotspot</b> and join it from the Wi-Fi menu on the Mac. The lights keep working through the cable.</span></li>
      <li>On the phone, open Safari and type:<div class="ph-urls">${(r.urls || []).map(u => `<code>${esc(u.replace(/^http:\/\//, ""))}</code>`).join("") || `<span class="hint">The Mac has no Wi-Fi address yet. Join a network or the hotspot, then check again.</span>`}</div></li>
      <li>Enter the PIN <b class="ph-pin">${esc(r.pin)}</b></li>
      <li>Tap Share, then <b>Add to Home Screen</b> to open it like an app next time.</li>
    </ol>
    <button class="sh-link" id="ph-again">Check the address again</button>`;
  panel.innerHTML = `<div class="sh-phead"><div><h2>Use your phone</h2><p>Control the lights from your phone or iPad while this Mac stays plugged into the AN2.</p></div></div>${body}`;
  const again = $("#ph-again", panel);
  if (again) again.onclick = () => { phoneInfo = null; renderPhone(panel); loadPhoneInfo(); };
}
