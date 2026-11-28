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
let strobeHz = loadPref("strobehz", 8);
$("#sh-strobe").onclick = () => api("/api/strobe", { hz: state.perform.strobe_hz ? 0 : strobeHz });
$$("#sh-strobespeed button").forEach(b => b.onclick = () => {
  strobeHz = +b.dataset.hz; savePref("strobehz", strobeHz); paintSegs();
  if (state.perform.strobe_hz) api("/api/strobe", { hz: strobeHz });
});
let lookFade = loadPref("lookfade", 1.5);
$$("#sh-fade button").forEach(b => b.onclick = () => { lookFade = +b.dataset.fade; savePref("lookfade", lookFade); paintSegs(); });
function paintSegs() {
  $$("#sh-strobespeed button").forEach(b => b.classList.toggle("on", +b.dataset.hz === strobeHz));
  $$("#sh-fade button").forEach(b => b.classList.toggle("on", +b.dataset.fade === lookFade));
}
paintSegs();

// hold a color button: every light goes that color until you let go
$$("[data-hit]").forEach(b => {
  let held = false;
  const down = e => { e.preventDefault(); if (held) return; held = true; b.classList.add("on"); api("/api/hit", { on: true, color: b.dataset.hit }); };
  const up = () => { if (!held) return; held = false; b.classList.remove("on"); api("/api/hit", { on: false, color: b.dataset.hit }); };
  b.onpointerdown = e => { b.setPointerCapture(e.pointerId); down(e); };
  b.onpointerup = b.onpointercancel = b.onlostpointercapture = up;
  b.oncontextmenu = e => e.preventDefault();
  window.addEventListener("blur", up);
});

$("#sh-undo").onclick = () => api("/api/undo", {});
function saveNowForm() {
  const w = $("#sh-savewrap");
  w.innerHTML = `<input id="sh-savename" placeholder="Name this look" maxlength="40"><button id="sh-saveok" class="primary">Save</button><button id="sh-savecancel">Cancel</button>`;
  const inp = $("#sh-savename");
  inp.focus();
  const done = () => { w.innerHTML = `<button id="sh-savenow" class="sh-small">+ Save what's on</button>`; $("#sh-savenow").onclick = saveNowForm; };
  const save = async () => { const name = inp.value.trim(); if (!name) { inp.focus(); return; } await api("/api/mood/save", { name }); done(); };
  $("#sh-saveok").onclick = save;
  $("#sh-savecancel").onclick = done;
  inp.onkeydown = e => { if (e.key === "Enter") save(); if (e.key === "Escape") done(); };
}
$("#sh-savenow").onclick = saveNowForm;

$("#sh-checkbtn").onclick = () => api("/api/check", {});
function renderCheck() {
  const ck = state.tools && state.tools.check, el = $("#sh-check");
  el.classList.toggle("hidden", !(ck && ck.running));
  $("#sh-checkbtn").classList.toggle("on", !!(ck && ck.running));
  if (!ck || !ck.running) return;
  const what = { red: "red", green: "green", blue: "blue", white: "white", center: "pointing at the middle", left: "pointing left", right: "pointing right", up: "tilting one way", down: "tilting the other way" }[ck.doing] || ck.doing;
  const txt = `<div><b>Checking your lights</b> · ${esc(ck.light)} should be <b>${esc(what)}</b> now. Everything else is off. <small>(${ck.step} of ${ck.steps})</small></div><button id="sh-checkstop">Stop</button>`;
  if (el.dataset.t !== txt) { el.dataset.t = txt; el.innerHTML = txt; $("#sh-checkstop").onclick = () => api("/api/check", { stop: true }); }
}
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

// ---------- auto: the mic runs the show ----------
// The browser on the Mac listens. It measures how loud the room is, finds the
// kick and snare hits (spectral flux with a moving threshold), works out the
// tempo from the gaps between hits, and keeps its own beat clock that only
// sends steady beats to the controller. The controller turns loudness and
// beats into slow or fast, calm or busy looks. The meter draws what it hears.
let autoMic = null, autoSensTouched = 0;
const micView = { level: 0, bands: new Array(24).fill(0), bpm: 0, conf: 0, beatAt: 0, hitAt: 0, quietFor: 0, heard: 0 };

async function autoMicStart() {
  const stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: false, noiseSuppression: false, autoGainControl: false } });
  const ctx = new (window.AudioContext || window.webkitAudioContext)();
  if (ctx.state === "suspended") await ctx.resume();
  const src = ctx.createMediaStreamSource(stream);
  const an = ctx.createAnalyser();
  an.fftSize = 1024; an.smoothingTimeConstant = 0;
  const proc = ctx.createScriptProcessor(512, 1, 1);
  const sr = ctx.sampleRate, binHz = sr / an.fftSize;
  const spec = new Float32Array(an.frequencyBinCount), prev = new Float32Array(an.frequencyBinCount);
  const lo = Math.max(1, Math.round(40 / binHz)), hi = Math.round(4000 / binHz);
  // 24 log-spaced bands, 40 Hz - 12 kHz, for the picture
  const edges = [];
  for (let i = 0; i <= 24; i++) edges.push(Math.max(1, Math.round(40 * Math.pow(300, i / 24) / binHz)));
  const fluxHist = [], onsets = [];
  let sum = 0, n = 0, sent = 0, lastOnset = 0, lastFlux = 0, rising = false;
  let period = 0, nextBeat = 0;
  const dt = 512 / sr * 1000;
  proc.onaudioprocess = ev => {
    const now = performance.now();
    const d = ev.inputBuffer.getChannelData(0);
    let e = 0;
    for (let i = 0; i < d.length; i++) e += d[i] * d[i];
    sum += e; n += d.length;
    const rms = Math.sqrt(e / d.length);
    micView.level = Math.max(rms, micView.level * 0.9);
    // spectrum and flux
    an.getFloatFrequencyData(spec);
    let flux = 0;
    for (let i = lo; i < hi; i++) {
      const m = Math.max(spec[i], -100);
      const diff = m - prev[i];
      if (diff > 0) flux += diff * (i < 10 ? 2 : 1); // weigh the kick drum
      prev[i] = m;
    }
    for (let b = 0; b < 24; b++) {
      let mx = -100;
      for (let i = edges[b]; i < Math.max(edges[b + 1], edges[b] + 1) && i < spec.length; i++) mx = Math.max(mx, spec[i]);
      const v = Math.min(1, Math.max(0, (mx + 90) / 70));
      micView.bands[b] = Math.max(v, micView.bands[b] * 0.85);
    }
    fluxHist.push(flux);
    if (fluxHist.length > 150) fluxHist.shift(); // about 1.5 s
    const sorted = fluxHist.slice().sort((a, b) => a - b);
    const med = sorted[sorted.length >> 1] || 0;
    const thr = med * 1.6 + 30;
    // a hit is a peak in the flux above the moving threshold
    if (flux > lastFlux) rising = true;
    else if (rising) {
      rising = false;
      if (lastFlux > thr && now - lastOnset > 220 && rms > 0.002) {
        lastOnset = now; micView.hitAt = now;
        onsets.push(now);
        while (onsets.length && now - onsets[0] > 10000) onsets.shift();
        tempo();
        // pull the beat clock towards the hit when it lands near a beat
        if (period && Math.abs(now - nextBeat) < period * 0.2) nextBeat = nextBeat + (now - nextBeat) * 0.5;
        else if (period && Math.abs(now - (nextBeat - period)) < period * 0.2) nextBeat = now + period;
      }
    }
    lastFlux = flux;
    // the beat clock: only steady beats go to the controller
    if (period && now >= nextBeat) {
      if (micView.conf >= 0.35 && now - lastOnset < 3000) {
        micView.beatAt = now;
        fetch("/api/beat", { method: "POST", body: '{"source":"mic"}' }).catch(() => {});
      }
      nextBeat += period;
      if (nextBeat < now) nextBeat = now + period;
    }
    micView.quietFor = rms < 0.0015 ? micView.quietFor + dt : 0;
    if (now - sent >= 250) {
      sent = now;
      const level = Math.sqrt(sum / Math.max(n, 1));
      sum = 0; n = 0;
      micView.heard = now;
      fetch("/api/auto/level", { method: "POST", body: JSON.stringify({ level }) }).catch(() => {});
    }
  };
  // tempo: score the gaps between nearby hits, folded into 80-160 BPM by
  // halving (never by thirds, which mistakes 124 BPM for 83)
  function tempo() {
    const score = new Float32Array(81); // 80..160 BPM
    let total = 0;
    for (let i = 0; i < onsets.length; i++) {
      for (let j = i + 1; j < onsets.length && j <= i + 2; j++) {
        const gap = onsets[j] - onsets[i];
        for (const k of [0.5, 1, 2, 4]) {
          const g = gap / k;
          if (g < 375 || g > 750) continue;
          const bpm = 60000 / g;
          const at = bpm - 80;
          for (let b = Math.max(0, Math.floor(at) - 2); b <= Math.min(80, Math.ceil(at) + 2); b++) score[b] += Math.exp(-((b - at) ** 2) / 2) * (k === 1 ? 1 : 0.5);
          total += k === 1 ? 1 : 0.5;
          break;
        }
      }
    }
    let best = 0;
    for (let b = 1; b <= 80; b++) if (score[b] > score[best]) best = b;
    if (!total) { micView.conf = 0; return; }
    micView.conf = Math.min(1, score[best] / total * 2.5);
    // keep the current tempo unless another one clearly wins
    if (micView.bpm && Math.abs(best + 80 - micView.bpm) > 2) {
      const cur = Math.round(micView.bpm - 80);
      if (cur >= 0 && cur <= 80 && score[cur] > score[best] * 0.75) best = cur;
    }
    const bpm = 80 + best;
    if (micView.conf >= 0.35) {
      const p = 60000 / bpm;
      if (!period || Math.abs(p - period) > period * 0.04) { period = p; nextBeat = performance.now() + p; }
      micView.bpm = bpm;
    }
  }
  src.connect(an); src.connect(proc); proc.connect(ctx.destination);
  return { stop() { proc.disconnect(); an.disconnect(); src.disconnect(); stream.getTracks().forEach(t => t.stop()); ctx.close(); micView.bpm = 0; micView.conf = 0; } };
}

// the picture of what the mic hears
(function drawMic() {
  requestAnimationFrame(drawMic);
  const cv = $("#sh-viz");
  if (!cv || tab !== "show") return;
  const on = !!autoMic;
  cv.parentElement.classList.toggle("hidden", !on);
  if (!on) return;
  const w = cv.clientWidth, h = cv.clientHeight, r = window.devicePixelRatio || 1;
  if (cv.width !== Math.round(w * r)) { cv.width = Math.round(w * r); cv.height = Math.round(h * r); }
  const g = cv.getContext("2d");
  g.setTransform(r, 0, 0, r, 0, 0);
  g.clearRect(0, 0, w, h);
  const now = performance.now();
  const beat = Math.max(0, 1 - (now - micView.beatAt) / 250);
  const hit = Math.max(0, 1 - (now - micView.hitAt) / 150);
  // beat light
  const cx = 26, cy = h / 2;
  g.beginPath(); g.arc(cx, cy, 14 + beat * 6, 0, 7);
  g.fillStyle = beat > 0 ? `rgba(241,7,163,${0.35 + beat * 0.65})` : "#2a2f3a"; g.fill();
  g.beginPath(); g.arc(cx, cy, 5, 0, 7); g.fillStyle = hit > 0 ? "#fff" : "#4a5060"; g.fill();
  // spectrum
  const x0 = 56, bw = (w - x0 - 8) / 24;
  for (let b = 0; b < 24; b++) {
    const v = micView.bands[b];
    const bh = Math.max(2, v * (h - 10));
    g.fillStyle = `hsl(${260 - b * 9}, 85%, ${45 + v * 20}%)`;
    g.fillRect(x0 + b * bw + 1, h - 5 - bh, bw - 2, bh);
  }
  // loudness line
  const lv = Math.min(1, Math.max(0, (20 * Math.log10(Math.max(micView.level, 1e-5)) + 60) / 60));
  g.fillStyle = "rgba(255,255,255,.85)";
  g.fillRect(x0, 2, (w - x0 - 8) * lv, 3);
  // words
  const t = $("#sh-vizt");
  const say = micView.quietFor > 4000 ? "Can't hear anything. Turn the music up, move the Mac nearer, or raise the mic sensitivity."
    : micView.bpm && micView.conf >= 0.35 ? `Hearing the beat · ${micView.bpm} BPM`
    : micView.level > 0.003 ? "Hearing music, finding the beat…" : "Listening…";
  if (t.textContent !== say) t.textContent = say;
})();

$("#sh-autobtn").onclick = async () => {
  const on = !(state && state.auto && state.auto.on);
  if (on && micOK && !autoMic) {
    try { autoMic = await autoMicStart(); } catch (e) { autoMic = null; showError("Could not open the mic: " + e.message + ". In Safari, allow the microphone for this page (Safari › Settings for This Website)."); }
  }
  if (!on && autoMic) { autoMic.stop(); autoMic = null; }
  await api("/api/auto", { on });
};
$("#sh-autosens").oninput = e => {
  autoSensTouched = Date.now();
  $("#sh-autosensv").textContent = sensWord(+e.target.value);
  throttled("autosens", () => api("/api/auto", { sensitivity: +e.target.value }));
};
function sensWord(v) { return v < 25 ? "Low" : v < 45 ? "Lower" : v <= 55 ? "Normal" : v <= 75 ? "Higher" : "High"; }

function renderAuto() {
  const a = state.auto;
  if (!a) return;
  $("#sh-auto").classList.toggle("on", a.on);
  const btn = $("#sh-autobtn");
  btn.classList.toggle("on", a.on);
  btn.querySelector("b").textContent = a.on ? "Auto is on" : "Auto";
  $("#sh-autosub").textContent = a.on ? "Tap to stop" : "Let the music run the lights";
  $("#sh-autometer").style.width = (a.on ? a.energy : 0) + "%";
  $("#sh-autometer").className = "lv" + (a.level === "Calm" ? 0 : a.level === "Groove" ? 1 : 2);
  let txt;
  if (!a.on) {
    txt = micOK ? "Listens to the music with the Mac's mic: slow and calm when it's quiet, fast and busy when it's loud, a new look every phrase."
      : "On this device Auto changes looks by itself at the Speed slider's pace. For it to follow the music, turn Auto on from the browser on the Mac.";
  } else {
    const moment = a.moment === "drop" ? " · Drop!" : a.moment === "spotlight" ? " · Spotlight moment" : "";
    txt = a.listening ? `${a.level} · ${esc(a.look)}${moment}` : `${esc(a.look)} · not listening, so it follows the Speed slider`;
    if (!a.listening && micOK && !autoMic) txt += ` <button id="sh-autolisten" class="sh-small">Listen with the mic</button>`;
  }
  const el = $("#sh-autotxt");
  if (el.dataset.t !== txt) {
    el.dataset.t = txt; el.innerHTML = txt;
    const l = $("#sh-autolisten");
    if (l) l.onclick = async () => { try { autoMic = await autoMicStart(); } catch (e) { showError("Could not open the mic: " + e.message); } };
  }
  if (Date.now() - autoSensTouched > 3000) { $("#sh-autosens").value = a.sensitivity; $("#sh-autosensv").textContent = sensWord(a.sensitivity); }
  $(".sh-sens").classList.toggle("hidden", !micOK);
  if (!a.on && autoMic) { autoMic.stop(); autoMic = null; }
}

// ---------- quick looks, tips and keys ----------
let qlooksSig = "";
let looksOpen = loadPref("looksopen", false);
const HIT_KEYS = { w: "white", r: "red", u: "blue" };
function renderQuickLooks() {
  const el = $("#sh-qlooks");
  const playing = state.schedule.playing_id, auto = !!(state.auto && state.auto.on);
  const limit = window.innerWidth < 760 ? 6 : 12;
  const sig = JSON.stringify([state.moods.map(m => [m.id, m.name, m.colors]), playing, auto, looksOpen, limit]);
  if (sig === qlooksSig) return;
  qlooksSig = sig;
  if (!state.moods.length) {
    el.innerHTML = `<button id="sh-addlooks" class="primary">Add the ready-made looks</button>`;
    $("#sh-addlooks").onclick = () => api("/api/mood/starters", {});
    return;
  }
  const grad = cs => {
    const c = (cs && cs.length ? cs : ["#333"]).slice(0, 5).map(esc);
    return c.length === 1 ? c[0] : `linear-gradient(90deg, ${c.join(", ")})`;
  };
  const shown = looksOpen ? state.moods : state.moods.slice(0, limit);
  el.innerHTML = shown.map((m, i) => `
    <button class="sh-qlook${m.id === playing && !auto ? " on" : ""}" data-qlook="${esc(m.id)}">
      <i class="sh-qbar" style="background:${grad(m.colors)}"></i>
      <span>${esc(m.name)}</span>${i < 9 ? `<kbd>${i + 1}</kbd>` : ""}
    </button>`).join("") +
    (state.moods.length > limit ? `<button class="sh-qlook sh-qmore" id="sh-alllooks"><span>${looksOpen ? "Fewer looks" : `All ${state.moods.length} looks`}</span></button>` : "") +
    (looksOpen || state.moods.length <= limit ? `<button class="sh-qlook sh-qmore" id="sh-morelooks" title="Adds any ready-made looks you don't have yet"><span>+ New looks</span></button>` : "");
  const all = $("#sh-alllooks");
  if (all) all.onclick = () => { looksOpen = !looksOpen; savePref("looksopen", looksOpen); renderQuickLooks(); };
  const more = $("#sh-morelooks");
  if (more) more.onclick = () => api("/api/mood/starters", {});
  $$("[data-qlook]", el).forEach(b => b.onclick = () => playLook(b.dataset.qlook));
}
function playLook(id) { api("/api/mood/play", { id, fade: lookFade }); }

if (!loadPref("tipsdone", false)) $("#sh-tips").classList.remove("hidden");
$("#sh-tipsok").onclick = () => { savePref("tipsdone", true); $("#sh-tips").classList.add("hidden"); };

document.addEventListener("keydown", e => {
  if (tab !== "show" || e.metaKey || e.ctrlKey || e.altKey) return;
  if (/INPUT|SELECT|TEXTAREA/.test(document.activeElement && document.activeElement.tagName)) return;
  const k = e.key.toLowerCase();
  if (k === "a") { if (!e.repeat) $("#sh-autobtn").click(); }
  else if (k === "b") { if (!e.repeat) $("#sh-blackout").click(); }
  else if (k === "s") { if (!e.repeat) $("#sh-strobe").click(); }
  else if (k === "z") { if (!e.repeat) $("#sh-undo").click(); }
  else if (HIT_KEYS[k]) { if (!e.repeat) api("/api/hit", { on: true, color: HIT_KEYS[k] }); }
  else if (k === "f") { if (!e.repeat) { $("#sh-flash").classList.add("on"); api("/api/flash", { on: true }); } }
  else if (/^[1-9]$/.test(k)) { if (state && state.moods[+k - 1]) playLook(state.moods[+k - 1].id); }
  else return;
  e.preventDefault();
});
document.addEventListener("keyup", e => {
  if (tab === "show" && e.key.toLowerCase() === "f") { $("#sh-flash").classList.remove("on"); api("/api/flash", { on: false }); }
  if (tab === "show" && HIT_KEYS[e.key.toLowerCase()]) api("/api/hit", { on: false });
});

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
  renderAuto();
  renderQuickLooks();
  renderCheck();
  const t = state.tools || {};
  const ub = $("#sh-undo");
  ub.disabled = !t.can_undo;
  ub.title = t.can_undo ? `Go back to ${t.undo_name || "what was on before"}` : "Nothing to go back to yet";
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
    sh.rig_ok, rigArmed, showVar, showSize, shSel, showPrograms.length, (state.tools || {}).aim_points,
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
// a small color strip on each effect button, so you can see what it does
const FX_PREVIEW = {
  chase: "linear-gradient(90deg,#fff 0 25%,#222 25% 50%,#fff 50% 75%,#222 75%)",
  jump: "linear-gradient(90deg,#ff3030 0 33%,#30ff60 33% 66%,#3060ff 66%)",
  fade: "linear-gradient(90deg,#ff3030,#30ff60,#3060ff)",
  rainbow: "linear-gradient(90deg,red,orange,yellow,lime,cyan,blue,violet)",
  wave: "linear-gradient(90deg,#222,#3a8bff,#222,#3a8bff,#222)",
  pulse: "linear-gradient(90deg,#ff2bd6,#222)",
  strobe: "linear-gradient(90deg,#fff 0 15%,#222 15% 50%,#fff 50% 65%,#222 65%)",
  police: "linear-gradient(90deg,#ff1010 0 50%,#1030ff 50%)",
  fire: "linear-gradient(90deg,#ff2000,#ff8c00,#ff3000,#ffb000)",
  sparkle: "radial-gradient(circle at 30% 50%,#fff 0 3px,transparent 4px),radial-gradient(circle at 75% 40%,#fff 0 2px,transparent 3px),#3a3fbf",
  breathe: "linear-gradient(90deg,#1b1b40,#5a7dff,#1b1b40)",
  heartbeat: "linear-gradient(90deg,#ff1040 0 12%,#300 12% 22%,#ff1040 22% 32%,#300 32%)",
  lightning: "linear-gradient(90deg,#0a005a 0 40%,#fff 40% 46%,#0a005a 46%)",
  ocean: "linear-gradient(90deg,#001eff,#00dcb4,#001eff)",
  sunset: "linear-gradient(90deg,#ff8c00,#ff3200,#ff003c,#aa00c8)",
  disco: "linear-gradient(90deg,#ff00c8,#ffe600,#00dcff,#5a00ff)",
  water: "linear-gradient(90deg,#0019ff,#0096ff,#ebf5ff,#0096ff,#0019ff)",
};
function choices(name, items, current) {
  return `<div class="sh-choices" data-name="${name}">${items.map(([label, v]) =>
    `<button class="${String(v) === String(current) ? "on" : ""}" data-v="${esc(v)}">${name === "fx" && FX_PREVIEW[v] ? `<i class="fxp" style="background:${FX_PREVIEW[v]}"></i>` : ""}${esc(label)}</button>`).join("")}</div>`;
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
    const pi = chans.findIndex(c => c.type === "pan"), ti = chans.findIndex(c => c.type === "tilt");
    const ax = pi >= 0 ? f0.values[pi] / 255 : 0.5, ay = ti >= 0 ? f0.values[ti] / 255 : 0.5;
    const pts = (state.tools && state.tools.aim_points) || [];
    html += section("Where it points", `<div class="aim">
        <div class="aim-pad xy" data-aimpad><i class="aim-cross"></i><b class="aim-dot" style="left:${ax * 100}%;top:${ay * 100}%"></b></div>
        <div class="aim-side">
          <p class="hint">Drag on the square to point the spots. Left and right across, the up/down channel down the square.</p>
          <div class="aim-pts">
            <button data-aimgo="0.5,0.5">Middle</button>
            ${pts.map(p => `<span class="aim-pt"><button data-aimgo="${p.x},${p.y}">${esc(p.name)}</button><button class="aim-x" data-aimdel="${esc(p.name)}" title="Forget">×</button></span>`).join("")}
          </div>
          <div class="aim-save"><input data-aimname placeholder="Name this spot, e.g. Dance floor" maxlength="30"><button data-aimsave>Save spot</button></div>
        </div>
      </div>`, move ? "it moves around this spot" : "");
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
  const pad = $("[data-aimpad]", panel);
  if (pad) {
    const dot = $(".aim-dot", pad);
    const aimAt = e => {
      const r = pad.getBoundingClientRect();
      const x = Math.min(1, Math.max(0, (e.clientX - r.left) / r.width)), y = Math.min(1, Math.max(0, (e.clientY - r.top) / r.height));
      dot.style.left = x * 100 + "%"; dot.style.top = y * 100 + "%";
      throttled("aim" + g.key, () => api("/api/show/aim", { fixtures: g.idx, x, y }));
    };
    pad.onpointerdown = e => { e.preventDefault(); pad.setPointerCapture(e.pointerId); dragging++; aimAt(e); pad.onpointermove = aimAt; };
    pad.onpointerup = pad.onpointercancel = () => { pad.onpointermove = null; dragging = 0; };
    $$("[data-aimgo]", panel).forEach(b => b.onclick = () => { const [x, y] = b.dataset.aimgo.split(",").map(Number); api("/api/show/aim", { fixtures: g.idx, x, y }); });
    $$("[data-aimdel]", panel).forEach(b => b.onclick = () => api("/api/aim/save", { name: b.dataset.aimdel, remove: true }));
    const nm = $("[data-aimname]", panel);
    const saveAim = () => { if (nm.value.trim()) api("/api/aim/save", { name: nm.value.trim() }); };
    $("[data-aimsave]", panel).onclick = saveAim;
    nm.onkeydown = e => { if (e.key === "Enter") saveAim(); };
  }
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
  if (id === "auto") return "linear-gradient(90deg,#7b2ff7,#f107a3)";
  const m = (state.moods || []).find(x => x.id === id);
  const cs = (m && m.colors && m.colors.length ? m.colors : ["#555"]).slice(0, 4);
  return cs.length > 1 ? `linear-gradient(90deg, ${cs.join(",")})` : cs[0];
}
function moodLabel(id) { return id === "off" ? "Lights off" : id === "auto" ? "Auto" : ((state.moods || []).find(x => x.id === id) || { name: "?" }).name; }

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
    `<option value="auto" ${id === "auto" ? "selected" : ""}>Auto (follows the music)</option>` +
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
