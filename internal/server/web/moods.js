"use strict";
// Moods & schedule: save whole looks as moods and play them at set times.
// Uses state, api, $, $$, esc, chips, inApp, loadPref and savePref from app.js.

const DAY_NAMES = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const DAY_LETTERS = ["S", "M", "T", "W", "T", "F", "S"];
const MOOD_FADES = [["Snap", 0], ["2 s", 2], ["5 s", 5], ["30 s", 30]];
const ENTRY_FADES = [["Snap", 0], ["5 seconds", 5], ["30 seconds", 30], ["1 minute", 60], ["5 minutes", 300], ["15 minutes", 900], ["30 minutes", 1800]];
const tz = (() => { try { return Intl.DateTimeFormat().resolvedOptions().timeZone || ""; } catch (_) { return ""; } })();

let moodFade = loadPref("moodfade", 5);
let schedDraft = null; // entries being edited
let schedDirty = false;
let moodSig = "", schedSig = "", statusSig = "";
let armedDelete = "";
let renaming = ""; // no prompt(): the iPad app's web view shows no JavaScript dialogs

function energy(m) {
  if (!m.program) return "";
  if (m.music) return `<span class="energy e2">music</span>`;
  const n = Math.min(3, Math.floor(m.speed / 25.01));
  return `<span class="energy e${n}">${["low", "medium", "high", "max"][n]} energy</span>`;
}

function moodById(id) { return state.moods.find(m => m.id === id); }
function moodName(id) { return id === "off" ? "Lights off" : (moodById(id) || { name: "?" }).name; }
function moodColor(id) {
  if (id === "off") return "#000";
  const m = moodById(id);
  return m && m.colors.length ? m.colors[0] : "#555";
}
function moodGradient(id) {
  const m = moodById(id);
  if (!m || !m.colors.length) return id === "off" ? "#111" : "#555";
  return m.colors.length === 1 ? m.colors[0] : `linear-gradient(90deg, ${m.colors.join(", ")})`;
}
function clock12(t) {
  const [h, m] = t.split(":").map(Number);
  return `${(h % 12) || 12}:${String(m).padStart(2, "0")} ${h < 12 ? "AM" : "PM"}`;
}
function fadeLabel(s) { const f = ENTRY_FADES.find(x => x[1] === s); return f ? f[0] : `${s} s`; }

function copyEntries(list) { return list.map(e => ({ time: e.time, days: e.days ? [...e.days] : [], mood: e.mood, fade: e.fade })); }

// ---------- render ----------
function renderMoods() {
  if (!state.moods || !state.schedule) return;
  if (!schedDirty) schedDraft = copyEntries(state.schedule.entries);
  renderStatus();
  if (tab !== "moods") return;
  const ms = JSON.stringify([state.moods, state.schedule.playing, moodFade, armedDelete, renaming]);
  if (ms !== moodSig) { moodSig = ms; renderMoodList(); }
  const ss = JSON.stringify([schedDraft, state.moods.map(m => [m.id, m.name, m.colors[0]]), schedDirty, state.schedule.current && state.schedule.current.entry]);
  if (ss !== schedSig) { schedSig = ss; renderRows(); }
  renderToday();
}

function renderStatus() {
  const s = state.schedule;
  const sig = JSON.stringify([s.enabled, s.playing, s.by_hand, s.current, s.next, s.now, s.problems, tab]);
  if (sig === statusSig) return;
  statusSig = sig;
  const t = $("#sc-toggle");
  t.classList.toggle("on", s.enabled);
  t.textContent = s.enabled ? "Schedule is ON" : "Schedule is off";
  const playing = s.playing ? `${esc(s.playing)}${s.by_hand && s.enabled ? ` <small style="display:inline;text-transform:none">(picked by hand)</small>` : ""}` : "nothing yet";
  const next = !s.enabled ? "schedule is off" : s.next ? `${esc(s.next.name)} at ${esc(s.next.clock)}` : "no times set";
  $("#sc-status").innerHTML = `<div><small>Playing now</small><b>${playing}</b></div>
    <div><small>Next</small><b>${next}</b></div>
    <div><small>Clock at the venue</small><b>${esc(s.now)}</b></div>`;
  $("#sc-zone").textContent = s.zone && s.zone !== "Local" ? `Times are in ${s.zone.replace(/_/g, " ")}` : "";
  const probs = [...s.problems];
  if (s.enabled && tz && s.time_zone && tz !== s.time_zone) probs.push(`This device is set to ${tz} but the schedule runs on ${s.time_zone} time. Save the schedule again to switch it to this device's time zone.`);
  $("#sc-problems").innerHTML = probs.map(p => `<p class="hint" style="color:var(--warn)">${esc(p)}</p>`).join("");
}

function renderMoodList() {
  chips($("#md-fades"), MOOD_FADES, moodFade, v => { moodFade = +v; savePref("moodfade", moodFade); moodSig = ""; renderMoods(); });
  const s = state.schedule;
  const list = state.moods;
  $("#md-starters").classList.toggle("primary", !list.length);
  if (!list.length) {
    $("#md-list").innerHTML = `<p class="hint">No moods yet. Tap <b>Add starter moods</b> for six ready-made ones (Warm welcome, Chill blue, Dinner glow, Party, Peak hour, Last call), or save your own look.</p>`;
    return;
  }
  $("#md-list").innerHTML = list.map((m, i) => {
    const bars = m.colors.length ? m.colors.map(c => `<i style="background:${c}"></i>`).join("") : `<i style="background:#222"></i>`;
    const on = s.playing === m.name;
    return `<div class="mood ${on ? "playing" : ""}" data-id="${esc(m.id)}">
      <div class="bar5">${bars}</div>
      ${renaming === m.id ? `<div class="row"><input class="grow rn" value="${esc(m.name)}"><button data-a="rename-ok" class="primary">OK</button></div>`
        : `<div class="mname">${esc(m.name)}${on ? "<em>● playing</em>" : ""}</div>`}
      <div class="meta">${m.program ? esc(m.program) : "Still colors"} ${energy(m)}</div>
      <div class="row"><button class="play primary" data-a="play">Play</button></div>
      <div class="row">
        <button data-a="update" title="Replace this mood with how the lights look now">Use current look</button>
        <button data-a="rename">Rename</button>
        <button data-a="up" ${i === 0 ? "disabled" : ""}>▲</button><button data-a="down" ${i === list.length - 1 ? "disabled" : ""}>▼</button>
        <button data-a="delete" class="${armedDelete === m.id ? "danger" : ""}">${armedDelete === m.id ? "Really delete?" : "Delete"}</button>
      </div></div>`;
  }).join("");
  $$("#md-list .mood button").forEach(b => b.onclick = () => moodAction(b.closest(".mood").dataset.id, b.dataset.a));
  const rn = $("#md-list .rn");
  if (rn) {
    rn.focus(); rn.select();
    rn.onkeydown = e => {
      if (e.key === "Enter") moodAction(renaming, "rename-ok");
      if (e.key === "Escape") { renaming = ""; moodSig = ""; renderMoods(); }
    };
  }
}

async function moodAction(id, a) {
  if (a !== "delete") armedDelete = "";
  const m = moodById(id);
  switch (a) {
    case "play": await api("/api/mood/play", { id, fade: moodFade }); break;
    case "update": await api("/api/mood/save", { id }); flashNote(`Saved the current look into ${m.name}.`); break;
    case "rename": renaming = renaming === id ? "" : id; break;
    case "rename-ok": {
      const name = $("#md-list .rn").value.trim();
      renaming = "";
      if (name && name !== m.name) await api("/api/mood/rename", { id, name });
      break;
    }
    case "up": case "down": await api("/api/mood/move", { id, delta: a === "up" ? -1 : 1 }); break;
    case "delete":
      if (armedDelete !== id) { armedDelete = id; moodSig = ""; renderMoods(); setTimeout(() => { if (armedDelete === id) { armedDelete = ""; moodSig = ""; renderMoods(); } }, 4000); return; }
      armedDelete = "";
      await api("/api/mood/delete", { id });
      if (schedDraft) { schedDraft = schedDraft.filter(e => e.mood !== id); }
      break;
  }
  moodSig = ""; renderMoods();
}

function flashNote(msg) {
  const el = $("#sc-dirty");
  el.textContent = msg;
  setTimeout(() => { if (el.textContent === msg) renderDirty(); }, 3000);
}

function renderDirty() { $("#sc-dirty").textContent = schedDirty ? "Not saved yet" : ""; $("#sc-save").disabled = !schedDirty; $("#sc-revert").disabled = !schedDirty; }

function renderRows() {
  renderDirty();
  const cur = state.schedule.current;
  const opts = id => [...state.moods.map(m => [m.id, m.name]), ["off", "Lights off"]]
    .map(([v, n]) => `<option value="${esc(v)}" ${v === id ? "selected" : ""}>${esc(n)}</option>`).join("");
  if (!schedDraft.length) {
    $("#sc-rows").innerHTML = `<p class="hint">No times yet. Tap <b>Add a time</b>. Example: 8:00 PM Warm welcome, 10:00 PM Party, 1:00 AM Last call, 2:30 AM Lights off.</p>`;
    return;
  }
  $("#sc-rows").innerHTML = schedDraft.map((e, i) => {
    const days = e.days.length ? e.days : [0, 1, 2, 3, 4, 5, 6];
    const isNow = !schedDirty && cur && cur.entry === i && state.schedule.enabled;
    return `<div class="srow ${isNow ? "cur" : ""}" data-i="${i}">
      <i class="sw" style="background:${moodGradient(e.mood)}"></i>
      <input type="time" value="${esc(e.time)}" data-f="time">
      <select data-f="mood">${moodById(e.mood) || e.mood === "off" ? "" : `<option value="" selected>Pick a mood</option>`}${opts(e.mood)}</select>
      <span class="days">${DAY_LETTERS.map((l, d) => `<button data-d="${d}" class="${days.includes(d) ? "on" : ""}" title="${DAY_NAMES[d]}">${l}</button>`).join("")}</span>
      <label class="meta">fade <select data-f="fade">${ENTRY_FADES.map(([n, v]) => `<option value="${v}" ${v === e.fade ? "selected" : ""}>${n}</option>`).join("")}${ENTRY_FADES.some(f => f[1] === e.fade) ? "" : `<option selected value="${e.fade}">${e.fade} s</option>`}</select></label>
      <span class="grow"></span>
      ${isNow ? `<span class="meta" style="color:var(--ok)">● now</span>` : ""}
      <button data-a="del">Remove</button>
    </div>`;
  }).join("");
  $$("#sc-rows .srow").forEach(row => {
    const e = schedDraft[+row.dataset.i];
    $$("[data-f]", row).forEach(inp => inp.onchange = () => {
      const f = inp.dataset.f;
      e[f] = f === "fade" ? +inp.value : inp.value;
      markDirty();
    });
    $$(".days button", row).forEach(b => b.onclick = () => {
      let days = e.days.length ? [...e.days] : [0, 1, 2, 3, 4, 5, 6];
      const d = +b.dataset.d;
      days = days.includes(d) ? days.filter(x => x !== d) : [...days, d].sort();
      if (!days.length) return; // at least one day
      e.days = days.length === 7 ? [] : days;
      markDirty();
    });
    $("[data-a=del]", row).onclick = () => { schedDraft.splice(+row.dataset.i, 1); markDirty(); };
  });
}

function markDirty() { schedDirty = true; schedSig = ""; renderMoods(); }

// ---------- today's timeline ----------
function minutes(t) { const [h, m] = t.split(":").map(Number); return h * 60 + m; }
function entryOn(e, d) { return !e.days.length || e.days.includes(d); }

// What plays at the start of a day: the last entry from the days before.
function carryIn(entries, day) {
  for (let back = 1; back <= 7; back++) {
    const d = (day - back + 7) % 7;
    const todays = entries.filter(e => e.mood && entryOn(e, d)).sort((a, b) => minutes(a.time) - minutes(b.time));
    if (todays.length) return todays[todays.length - 1];
  }
  return null;
}

function renderToday() {
  const el = $("#sc-today");
  const entries = (schedDraft || []).filter(e => e.mood && /^\d\d?:\d\d$/.test(e.time));
  if (!entries.length) { el.innerHTML = ""; return; }
  const now = new Date();
  const day = now.getDay();
  const todays = entries.filter(e => entryOn(e, day)).sort((a, b) => minutes(a.time) - minutes(b.time));
  const segs = [];
  let at = 0, mood = (carryIn(entries, day) || {}).mood;
  for (const e of todays) {
    const m = minutes(e.time);
    if (m > at) segs.push([at, m, mood]);
    at = m; mood = e.mood;
  }
  segs.push([at, 1440, mood]);
  const nowPct = (now.getHours() * 60 + now.getMinutes()) / 14.4;
  const sig = JSON.stringify([segs, Math.round(nowPct * 10), state.moods.map(m => [m.name, m.colors]), schedDirty]);
  if (el.dataset.sig === sig) return;
  el.dataset.sig = sig;
  el.innerHTML = `<div class="hint" style="margin-top:0">Today (${DAY_NAMES[day]})${schedDirty ? ", with your unsaved changes" : ""}</div>
    <div class="strip24">${segs.map(([a, b, m]) => `<div class="seg" style="width:${(b - a) / 14.4}%;background:${m ? moodGradient(m) : "#1d212b"}" title="${esc(m ? moodName(m) : "nothing scheduled")} from ${clock12(`${Math.floor(a / 60)}:${a % 60}`)}">${b - a >= 60 && m ? `<span>${esc(moodName(m))}</span>` : ""}</div>`).join("")}
    <div class="nowline" style="left:${nowPct}%"></div></div>
    <div class="hours"><span>12 AM</span><span>6 AM</span><span>12 PM</span><span>6 PM</span><span>12 AM</span></div>`;
}

// ---------- actions ----------
$("#md-save").onclick = async () => {
  const name = $("#md-name").value.trim();
  await api("/api/mood/save", { name });
  $("#md-name").value = "";
  moodSig = ""; renderMoods();
};
$("#md-name").onkeydown = e => { if (e.key === "Enter") $("#md-save").click(); };
$("#md-starters").onclick = async () => { await api("/api/mood/starters", {}); moodSig = ""; renderMoods(); };

$("#sc-add").onclick = () => {
  const last = schedDraft[schedDraft.length - 1];
  let time = "20:00";
  if (last) { const m = (minutes(last.time) + 120) % 1440; time = `${String(Math.floor(m / 60)).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`; }
  schedDraft.push({ time, days: [], mood: state.moods.length ? state.moods[0].id : "off", fade: 30 });
  markDirty();
};
$("#sc-revert").onclick = () => { schedDirty = false; schedSig = ""; renderMoods(); };

async function saveSchedule(enabled) {
  const bad = schedDraft.findIndex(e => !e.mood);
  if (bad >= 0) { showError(`Line ${bad + 1}: pick a mood.`); return false; }
  await api("/api/schedule", { enabled, time_zone: tz, entries: schedDraft });
  schedDirty = false; schedSig = ""; statusSig = "";
  renderMoods();
  return true;
}
$("#sc-save").onclick = () => saveSchedule(state.schedule.enabled);
$("#sc-toggle").onclick = () => {
  if (!state.schedule.enabled && !schedDraft.length) { showError("Add at least one time to the schedule first."); return; }
  saveSchedule(!state.schedule.enabled);
};

$("#sc-tips").innerHTML = `
  <div class="${inApp ? "" : "here"}"><b>Running it from the Mac</b><ol>
    <li>Keep the Mac plugged in and the <b>START</b> window open. The app stops the Mac from going to sleep while it runs (the screen can still turn off).</li>
    <li>On a MacBook, leave the lid open, or it goes to sleep anyway.</li>
    <li>Leave this tab or any tab open, or close the browser: the schedule runs inside the app, not the page.</li>
    <li>If the power goes out, start the app again and it goes straight to the mood that should be on.</li>
  </ol></div>
  <div class="${inApp ? "here" : ""}"><b>Running it from the iPad app</b><ol>
    <li>Keep the iPad charging and the Sonar DMX app open on screen. The app keeps the screen awake, but iPadOS pauses it if you switch apps or lock it.</li>
    <li>Lock it into the app with <b>Guided Access</b>: Settings, Accessibility, Guided Access, turn it on and set a passcode. Then in the app, triple-click the top button and tap Start.</li>
    <li>Turn the screen brightness down if it bothers anyone; that's fine.</li>
  </ol></div>
  <div><b>Good to know</b><ol>
    <li>Only one device can drive the lights. Quit the app on the other one.</li>
    <li>Tapping Play on a mood, or changing anything by hand, lasts until the next time on the schedule.</li>
    <li>Moods saved with music mode on follow the TAP button or the Mac's mic. With nobody there they keep the last tempo they heard. For a night on autopilot, save moods with music mode off.</li>
  </ol></div>`;
