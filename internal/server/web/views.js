"use strict";
// Perform tab layouts (Groups, Desk, Stage), the programs panel and music
// mode. Shares state, api() and helpers with app.js.

let view = loadPref("view", "groups");
let catalog = { programs: [], palettes: [] };
let progTarget = { label: "All lights", fixtures: "all" };
const draft = { speed: 50, size: 60, variation: 0, music: false };
const stageSel = new Set();
let deskKey = "";
const VIEWS = [["Groups", "groups"], ["Desk", "desk"], ["Stage", "stage"], ["Pads", "pads"]];
const MINI = PADS.filter(p => ["Red", "Amber", "Green", "Cyan", "Blue", "UV", "Magenta", "White", "Warm"].includes(p[0]));

fetch("/api/programs").then(r => r.json()).then(c => { catalog = c; viewSig = ""; renderViews(); }).catch(() => {});

// ---------- helpers ----------
function fixtureGroups() {
  const out = [];
  for (const f of state.fixtures) {
    let g = out.find(x => x.key === f.profile);
    if (!g) {
      g = { key: f.profile, fixtures: [], color: hasColor(f), name: "" };
      out.push(g);
    }
    g.fixtures.push(f.index);
  }
  for (const g of out) {
    const f = state.fixtures[g.fixtures[0]];
    g.name = g.fixtures.length > 1 ? `${f.profile_name.replace(/^Chauvet /, "").replace(/^Generic /, "")} ×${g.fixtures.length}` : f.name;
    g.short = g.fixtures.length > 1 ? f.profile_name.replace(/^Chauvet /, "").replace(/^Generic /, "").split(" ")[0] : f.name;
  }
  return out;
}

function colorIdx(list) {
  const all = list === "all" ? state.fixtures.map(f => f.index) : list;
  return all.filter(i => state.fixtures[i] && hasColor(state.fixtures[i]));
}

function runFor(list) {
  const want = colorIdx(list);
  if (!want.length) return null;
  return state.perform.runs.find(r => want.every(i => r.fixtures.includes(i))) || null;
}

function runOf(i) { return state.perform.runs.find(r => r.fixtures.includes(i)) || null; }

function programName(id) { return (catalog.programs.find(p => p.id === id) || { name: id }).name; }

function holdButton(el, fixtures) {
  let held = false;
  const up = () => { if (!held) return; held = false; el.classList.remove("on"); api("/api/flash", { on: false, fixtures }); };
  el.onpointerdown = e => {
    e.preventDefault();
    el.setPointerCapture(e.pointerId);
    if (held) return;
    held = true; el.classList.add("on");
    api("/api/flash", { on: true, fixtures });
  };
  el.onpointerup = el.onpointercancel = el.onlostpointercapture = up;
  el.oncontextmenu = e => e.preventDefault();
}

function setTarget(label, fixtures) {
  progTarget = { label, fixtures };
  const run = runFor(fixtures);
  if (run) Object.assign(draft, { speed: run.speed, size: run.size, variation: run.variation, music: run.music });
  renderPrograms(true);
}

function avgBrightness(list) {
  const fx = list.map(i => state.fixtures[i]);
  return Math.round(fx.reduce((a, f) => a + f.brightness, 0) / Math.max(fx.length, 1));
}

function valueOf(i, type) {
  const f = state.fixtures[i];
  const j = f ? chIndex(f, type) : -1;
  return j < 0 ? null : f.values[j];
}

function setRange(inp, v, label) {
  if (!inp || v == null) return;
  if (document.activeElement === inp && dragging) return;
  inp.value = v;
  if (label) label.textContent = v;
}

function swatchButtons(cls) {
  return MINI.map(([n, css]) => `<button class="${cls}" data-pad="${n}" style="--pc:${css}" aria-label="${n}"><span>${n}</span></button>`).join("");
}

function sendPad(name, fixtures) {
  const [r, g, b, a, w, uv] = PADS.find(p => p[0] === name)[2];
  api("/api/color", { fixtures, r, g, b, a, w, uv, fade: fadeSec });
}

function aim(list, x, y) {
  for (const i of list) {
    const f = state.fixtures[i];
    const v = {};
    if (chIndex(f, "pan") >= 0) v[chIndex(f, "pan") + 1] = x;
    if (chIndex(f, "tilt") >= 0) v[chIndex(f, "tilt") + 1] = y;
    throttled("aim" + i, () => api("/api/fixture", { index: i, values: v }));
  }
}

function setType(list, type, val) {
  for (const i of list) {
    const f = state.fixtures[i];
    const j = chIndex(f, type);
    if (j >= 0) throttled(`t${i}${type}`, () => api("/api/fixture", { index: i, values: { [j + 1]: val } }));
  }
}

function xyPad(el, list) {
  const move = e => {
    const r = el.getBoundingClientRect();
    const x = Math.round(Math.max(0, Math.min(1, (e.clientX - r.left) / r.width)) * 255);
    const y = Math.round(Math.max(0, Math.min(1, 1 - (e.clientY - r.top) / r.height)) * 255);
    placeDot(el, x, y);
    aim(list, x, y);
  };
  el.onpointerdown = e => { el.setPointerCapture(e.pointerId); move(e); el.onpointermove = move; };
  el.onpointerup = el.onpointercancel = () => { el.onpointermove = null; };
}

// ---------- view switch ----------
let viewSig = "";
function renderViews() {
  if (!state) return;
  const groups = fixtureGroups();
  const sig = JSON.stringify([view, groups.map(g => [g.key, g.fixtures]), catalog.programs.length]);
  if (sig !== viewSig) {
    viewSig = sig;
    chips($("#views"), VIEWS, view, v => { view = v; savePref("view", v); viewSig = ""; renderViews(); });
    $$(".pview").forEach(el => el.classList.toggle("hidden", el.id !== "view-" + view));
    if (view === "groups") buildGroups(groups);
    if (view === "desk") buildDesk(groups);
    if (view === "stage") buildStage(groups);
    renderPrograms(true);
  }
  if (view === "groups") updateGroups(groups);
  if (view === "desk") updateDesk(groups);
  if (view === "stage") updateStage();
  renderPrograms(false);
}

// ---------- layout 3: a column per fixture group ----------
function buildGroups(groups) {
  $("#gcols").innerHTML = groups.map(g => `<div class="gcol" data-key="${esc(g.key)}">
    <div class="ghead"><b>${esc(g.name)}</b><button class="en"></button></div>
    ${g.color ? `<div class="gpads">${swatchButtons("gpad")}</div>`
      : `<div class="xy gxy" title="Drag to aim"><div class="dot"></div></div>
         <div class="row" style="margin:0"><button data-act="center">Center</button><button data-act="on">Light on</button><button data-act="off">Off</button></div>
         <label class="pmaster">Dimmer <input class="gdim" type="range" min="0" max="255"><span></span></label>`}
    <label class="pmaster">Level <input class="glevel" type="range" min="0" max="100"><span></span>%</label>
    <div class="row" style="margin:0">
      ${g.color ? `<button class="gprog">Programs</button>` : ""}
      <button class="hit gflash">FLASH</button>
    </div>
    <div class="meta gstatus"></div>
  </div>`).join("");
  for (const g of groups) {
    const col = $(`.gcol[data-key="${CSS.escape(g.key)}"]`);
    $(".en", col).onclick = () => api("/api/group", { fixtures: g.fixtures, enabled: !g.fixtures.some(i => state.fixtures[i].enabled) });
    $$(".gpad", col).forEach(b => b.onclick = () => sendPad(b.dataset.pad, g.fixtures));
    const lvl = $(".glevel", col);
    lvl.oninput = () => { lvl.nextElementSibling.textContent = lvl.value; throttled("gl" + g.key, () => api("/api/group", { fixtures: g.fixtures, brightness: +lvl.value })); };
    const pb = $(".gprog", col);
    if (pb) pb.onclick = () => { setTarget(g.name, g.fixtures); $("#progcard").scrollIntoView({ behavior: "smooth", block: "nearest" }); };
    holdButton($(".gflash", col), g.fixtures);
    const xy = $(".gxy", col);
    if (xy) {
      xyPad(xy, g.fixtures);
      $("[data-act=center]", col).onclick = () => aim(g.fixtures, 128, 128);
      $("[data-act=on]", col).onclick = () => setType(g.fixtures, "dimmer", 255);
      $("[data-act=off]", col).onclick = () => setType(g.fixtures, "dimmer", 0);
      const dim = $(".gdim", col);
      dim.oninput = () => { dim.nextElementSibling.textContent = dim.value; setType(g.fixtures, "dimmer", +dim.value); };
    }
  }
}

function updateGroups(groups) {
  for (const g of groups) {
    const col = $(`.gcol[data-key="${CSS.escape(g.key)}"]`);
    if (!col) continue;
    const on = g.fixtures.some(i => state.fixtures[i].enabled);
    const en = $(".en", col);
    en.textContent = on ? "On" : "Off";
    en.classList.toggle("primary", on);
    col.classList.toggle("off", !on);
    col.classList.toggle("target", JSON.stringify(progTarget.fixtures) === JSON.stringify(g.fixtures));
    const lvl = $(".glevel", col);
    setRange(lvl, avgBrightness(g.fixtures), lvl.nextElementSibling);
    const run = g.color ? runFor(g.fixtures) : null;
    $(".gstatus", col).textContent = run ? `Running: ${programName(run.program)}${run.music ? " (music)" : ""}` : (g.color ? "" : "Moving heads");
    const xy = $(".gxy", col);
    if (xy && !xy.onpointermove) placeDot(xy, valueOf(g.fixtures[0], "pan") ?? 128, valueOf(g.fixtures[0], "tilt") ?? 128);
    const dim = $(".gdim", col);
    if (dim) setRange(dim, valueOf(g.fixtures[0], "dimmer"), dim.nextElementSibling);
  }
}

// ---------- layout 4: lighting desk ----------
const DESK_COLORS = [["red", "R"], ["green", "G"], ["blue", "B"], ["amber", "A"], ["white", "W"], ["uv", "UV"]];

function vfader(label, cls, max, extra = "") {
  return `<div class="strip ${cls}"><span class="slab">${label}</span>
    <input type="range" class="vr" min="0" max="${max}" aria-label="${esc(label)}"><span class="sval"></span>${extra}</div>`;
}

function buildDesk(groups) {
  const colorGroups = groups.filter(g => g.color);
  if (!colorGroups.some(g => g.key === deskKey)) deskKey = colorGroups[0] ? colorGroups[0].key : "";
  $("#desk").innerHTML =
    groups.map(g => vfader(g.short, "dgroup", 100, `${g.color ? `<button class="dsel" data-key="${esc(g.key)}">Sel</button>` : `<span class="dsel"></span>`}<button class="hit dflash" data-key="${esc(g.key)}">Flash</button>`).replace('class="strip dgroup"', `class="strip dgroup" data-key="${esc(g.key)}"`)).join("")
    + `<div class="ddiv"></div>`
    + DESK_COLORS.map(([t, l]) => vfader(l, "dcolor", 255).replace('class="strip dcolor"', `class="strip dcolor" data-type="${t}"`)).join("")
    + vfader("Speed", "dspeed", 100)
    + `<div class="ddiv"></div>`
    + vfader("Master", "dmaster", 100, `<button class="hit dbo">B/O</button>`);
  for (const g of groups) {
    const s = $(`.dgroup[data-key="${CSS.escape(g.key)}"]`);
    const inp = $(".vr", s);
    inp.oninput = () => { $(".sval", s).textContent = inp.value; throttled("dg" + g.key, () => api("/api/group", { fixtures: g.fixtures, brightness: +inp.value })); };
    holdButton($(".dflash", s), g.fixtures);
    const sel = $("button.dsel", s);
    if (sel) sel.onclick = () => { deskKey = g.key; setTarget(g.name, g.fixtures); updateDesk(fixtureGroups()); };
  }
  $$("#desk .dcolor").forEach(s => {
    const inp = $(".vr", s);
    const key = { red: "r", green: "g", blue: "b", amber: "a", white: "w", uv: "uv" }[s.dataset.type];
    inp.oninput = () => {
      $(".sval", s).textContent = inp.value;
      const g = fixtureGroups().find(x => x.key === deskKey);
      if (g) throttled("dc" + key, () => api("/api/color", { fixtures: g.fixtures, [key]: +inp.value }));
    };
  });
  const sp = $("#desk .dspeed .vr");
  sp.oninput = () => { draft.speed = +sp.value; $("#pspeed").value = sp.value; resendProgram(); renderPrograms(false); };
  const m = $("#desk .dmaster .vr");
  m.oninput = () => { $("#desk .dmaster .sval").textContent = m.value; throttled("master", () => api("/api/master", { value: +m.value })); };
  $("#desk .dbo").onclick = () => api("/api/blackout", { on: !state.blackout });
  const g = groups.find(x => x.key === deskKey);
  if (g) setTarget(g.name, g.fixtures);
}

function updateDesk(groups) {
  for (const g of groups) {
    const s = $(`.dgroup[data-key="${CSS.escape(g.key)}"]`);
    if (!s) continue;
    setRange($(".vr", s), avgBrightness(g.fixtures), $(".sval", s));
    const sel = $("button.dsel", s);
    if (sel) sel.classList.toggle("primary", g.key === deskKey);
    s.classList.toggle("target", g.key === deskKey);
  }
  const g = groups.find(x => x.key === deskKey);
  $$("#desk .dcolor").forEach(s => {
    const v = g ? valueOf(g.fixtures[0], s.dataset.type) : null;
    s.classList.toggle("hidden", v == null);
    setRange($(".vr", s), v, $(".sval", s));
  });
  setRange($("#desk .dspeed .vr"), draft.speed, $("#desk .dspeed .sval"));
  setRange($("#desk .dmaster .vr"), state.master, $("#desk .dmaster .sval"));
  const bo = $("#desk .dbo");
  if (bo) bo.classList.toggle("on", state.blackout);
}

// ---------- layout 7: stage map ----------
function defaultPos(f, groups) {
  const g = groups.find(x => x.fixtures.includes(f.index));
  const k = g.fixtures.indexOf(f.index), n = g.fixtures.length;
  if (g.color && n > 1) {
    const left = k < Math.ceil(n / 2);
    const row = left ? k : k - Math.ceil(n / 2);
    const rows = left ? Math.ceil(n / 2) : Math.floor(n / 2);
    return { x: left ? 0.07 : 0.93, y: 0.12 + 0.6 * (rows > 1 ? row / (rows - 1) : 0.5) };
  }
  const singles = state.fixtures.filter(x => { const gg = groups.find(y => y.fixtures.includes(x.index)); return !(gg.color && gg.fixtures.length > 1); });
  const j = singles.findIndex(x => x.index === f.index);
  return { x: 0.5 + (j - (singles.length - 1) / 2) * 0.12, y: 0.72 };
}

function shortName(f) {
  const m = f.name.match(/^(\S)\S*(?:\s+(\d+))?/);
  return m ? (m[1] + (m[2] || "")).toUpperCase() : "?";
}

function buildStage(groups) {
  const st = $("#stage");
  st.innerHTML = `<div class="stagelabel">STAGE</div>` + state.fixtures.map(f => {
    const p = f.pos || defaultPos(f, groups);
    return `<button class="sdot" data-i="${f.index}" style="left:${p.x * 100}%;top:${p.y * 100}%" aria-label="${esc(f.name)}"><span>${esc(shortName(f))}</span><i class="badge"></i></button>`;
  }).join("");
  chips($("#stagesel"), [["All", "all"], ["None", "none"], ...groups.map(g => [g.short, g.key])], "", v => {
    stageSel.clear();
    if (v === "all") state.fixtures.forEach(f => stageSel.add(f.index));
    else if (v !== "none") groups.find(g => g.key === v).fixtures.forEach(i => stageSel.add(i));
    stageChanged();
  });
  $$("#stage .sdot").forEach(d => {
    const i = +d.dataset.i;
    let moved = false;
    d.onpointerdown = e => {
      if (!$("#stagemove").checked) return;
      e.preventDefault();
      d.setPointerCapture(e.pointerId);
      moved = false;
      d.onpointermove = ev => {
        const r = st.getBoundingClientRect();
        const x = Math.max(0, Math.min(1, (ev.clientX - r.left) / r.width)), y = Math.max(0, Math.min(1, (ev.clientY - r.top) / r.height));
        d.style.left = x * 100 + "%"; d.style.top = y * 100 + "%";
        d.dataset.x = x; d.dataset.y = y; moved = true;
      };
    };
    d.onpointerup = () => {
      if (d.onpointermove) {
        d.onpointermove = null;
        if (moved) api("/api/position", { index: i, x: +d.dataset.x, y: +d.dataset.y });
        return;
      }
    };
    d.onclick = () => {
      if ($("#stagemove").checked) return;
      stageSel.has(i) ? stageSel.delete(i) : stageSel.add(i);
      stageChanged();
    };
  });
  $("#tswatches").innerHTML = swatchButtons("gpad");
  $$("#tswatches .gpad").forEach(b => b.onclick = () => { if (stageSel.size) sendPad(b.dataset.pad, [...stageSel]); });
  const lvl = $("#traylevel");
  lvl.oninput = () => { $("#traylevelv").textContent = lvl.value; if (stageSel.size) throttled("tl", () => api("/api/group", { fixtures: [...stageSel], brightness: +lvl.value })); };
  stageChanged();
}

function stageChanged() {
  const list = [...stageSel].sort((a, b) => a - b);
  $("#traytitle").textContent = list.length ? (list.length === 1 ? state.fixtures[list[0]].name : `${list.length} selected`) : "Tap lights to select them";
  if (list.length) setTarget(list.length === 1 ? state.fixtures[list[0]].name : `${list.length} selected`, list);
  updateStage();
}

function updateStage() {
  for (const f of state.fixtures) {
    const d = $(`#stage .sdot[data-i="${f.index}"]`);
    if (!d) continue;
    d.classList.toggle("sel", stageSel.has(f.index));
    d.classList.toggle("off", !f.enabled);
    d.style.background = hasColor(f) ? ((state.perform.live || [])[f.index] || previewColor(f)) : "#8b92a3";
    const run = runOf(f.index);
    $(".badge", d).textContent = run ? (run.music ? "♪" : "∿") : "";
    if (f.pos && !d.onpointermove) { d.style.left = f.pos.x * 100 + "%"; d.style.top = f.pos.y * 100 + "%"; }
  }
  const list = [...stageSel];
  if (list.length) setRange($("#traylevel"), avgBrightness(list), $("#traylevelv"));
  $$("#tswatches .gpad").forEach(b => b.disabled = !list.length);
}

// ---------- programs panel ----------
function speedLabel() {
  if (draft.music) return ["¼ beat", "½ beat", "1 per beat", "2 per beat", "4 per beat"][Math.round(draft.speed / 25)];
  return Math.round(30 * Math.pow(8, draft.speed / 100)) + " BPM";
}

function resendProgram() {
  const run = runFor(progTarget.fixtures);
  if (!run) return;
  throttled("prog", () => api("/api/program", { fixtures: progTarget.fixtures, program: run.program, ...draft }));
}

let progSig = "";
function renderPrograms(force) {
  if (!state || !catalog.programs.length) return;
  const run = runFor(progTarget.fixtures);
  if (run && !dragging) Object.assign(draft, { speed: run.speed, size: run.size, variation: run.variation, music: run.music });
  const sig = JSON.stringify([progTarget, run && run.program, draft, catalog.palettes.length]);
  if (force || sig !== progSig) {
    progSig = sig;
    $("#progtarget").textContent = "for " + progTarget.label;
    $("#proggrid").innerHTML = catalog.programs.map(p => `<button class="prog ${run && run.program === p.id ? "on" : ""}" data-p="${p.id}">${esc(p.name)}</button>`).join("");
    $$("#proggrid .prog").forEach(b => b.onclick = () => api("/api/program", { fixtures: progTarget.fixtures, program: b.dataset.p, ...draft }));
    const pal = $("#palettes");
    const label = $(".chiplabel", pal).outerHTML;
    pal.innerHTML = label + catalog.palettes.map((p, i) => `<button class="chip ${i === draft.variation ? "on" : ""}" data-v="${i}">
      ${p.colors.slice(0, 4).map(c => `<i class="pdot" style="background:rgb(${c.join(",")})"></i>`).join("")} ${esc(p.name)}</button>`).join("");
    $$("#palettes .chip").forEach(b => b.onclick = () => { draft.variation = +b.dataset.v; resendProgram(); renderPrograms(true); });
    $("#progstop").disabled = !run;
  }
  setRange($("#pspeed"), draft.speed);
  setRange($("#psize"), draft.size);
  $("#pspeedv").textContent = speedLabel();
  $("#psizev").textContent = draft.size < 10 ? "together" : draft.size > 90 ? "spread" : Math.round(draft.size) + "%";
  $("#pmusic").checked = draft.music;
  const b = state.perform.beat;
  const bpm = $("#bpm");
  bpm.textContent = b.bpm ? `${Math.round(b.bpm)} BPM · ${b.source}` : "no beat yet";
  bpm.classList.toggle("live", b.live);
  $("#progstopall").disabled = !state.perform.runs.length;
}

$("#pspeed").oninput = e => { draft.speed = +e.target.value; renderPrograms(false); resendProgram(); };
$("#psize").oninput = e => { draft.size = +e.target.value; renderPrograms(false); resendProgram(); };
$("#pmusic").onchange = e => { draft.music = e.target.checked; renderPrograms(false); resendProgram(); };
$("#progstop").onclick = () => api("/api/program/stop", { fixtures: progTarget.fixtures });
$("#progstopall").onclick = () => api("/api/program/stop", {});
$("#tap").onpointerdown = e => { e.preventDefault(); fetch("/api/beat", { method: "POST", body: '{"source":"tap"}' }); };

// ---------- music: beat detection from the Mac's mic ----------
// Browsers only allow the mic on secure pages, which includes
// http://127.0.0.1 on the Mac but not the iPad remote over Wi-Fi.
let mic = null;
const micOK = window.isSecureContext && navigator.mediaDevices && navigator.mediaDevices.getUserMedia;
if (!micOK) {
  $("#mic").disabled = true;
  $("#michint").textContent = "Mic listening only works in the browser on the Mac itself. Use TAP here.";
}
$("#mic").textContent = `Listen with this ${here}'s mic`;
$("#mic").onclick = async () => {
  if (mic) { mic.stop(); mic = null; $("#mic").textContent = `Listen with this ${here}'s mic`; $("#mic").classList.remove("primary"); return; }
  try {
    mic = await startMic();
    $("#mic").textContent = "Stop listening";
    $("#mic").classList.add("primary");
    $("#michint").textContent = "Listening for the kick drum. Keep this page open.";
  } catch (e) {
    $("#michint").textContent = "Could not open the mic: " + e.message;
  }
};

async function startMic() {
  const stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: false, noiseSuppression: false, autoGainControl: false } });
  const ctx = new (window.AudioContext || window.webkitAudioContext)();
  const src = ctx.createMediaStreamSource(stream);
  const lp = ctx.createBiquadFilter();
  lp.type = "lowpass"; lp.frequency.value = 150;
  // ScriptProcessor keeps running when the tab is in the background, unlike timers.
  const proc = ctx.createScriptProcessor(1024, 1, 1);
  const hist = [];
  let last = 0;
  proc.onaudioprocess = ev => {
    const d = ev.inputBuffer.getChannelData(0);
    let e = 0;
    for (let i = 0; i < d.length; i++) e += d[i] * d[i];
    e /= d.length;
    const avg = hist.length ? hist.reduce((a, b) => a + b, 0) / hist.length : e;
    hist.push(e);
    if (hist.length > 43) hist.shift();
    const now = performance.now();
    if (hist.length > 20 && e > avg * 1.6 && e > 1e-5 && now - last > 280) {
      last = now;
      fetch("/api/beat", { method: "POST", body: '{"source":"mic"}' }).catch(() => {});
    }
  };
  src.connect(lp); lp.connect(proc); proc.connect(ctx.destination);
  return { stop() { proc.disconnect(); src.disconnect(); stream.getTracks().forEach(t => t.stop()); ctx.close(); } };
}
