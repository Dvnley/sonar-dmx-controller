"use strict";
// Fixture presets: build your own sliders for a fixture model and apply
// them to patched fixtures. Uses state, profiles, api, $, $$ and esc from app.js.

let preset = null;        // the preset being edited: { id, custom, name, mode, notes, channels }
let presetApply = new Set();
let presetFixSig = "";
let deleteArmed = false;

function stopsText(stops) { return (stops || []).map(s => `${s.name}=${s.value}`).join(", "); }

function parseStops(text) {
  const out = [], bad = [];
  for (const part of text.split(/[,;\n]/)) {
    const t = part.trim();
    if (!t) continue;
    const m = t.match(/^(.*?)\s*[=:]\s*(\d{1,3})$/);
    if (!m || !m[1].trim() || +m[2] > 255) { bad.push(t); continue; }
    out.push({ name: m[1].trim(), value: +m[2] });
  }
  return { stops: out, bad };
}

function editFrom(p, modeName) {
  const m = p.modes.find(m => m.name === modeName) || p.modes[0];
  preset = {
    id: p.custom ? p.id : "", custom: !!p.custom, from: p.custom ? "" : p.name,
    name: p.custom ? p.name : `My ${p.name.replace(/^Generic /, "").replace(/^Chauvet /, "")}`,
    mode: m.name, notes: p.custom ? p.notes || "" : "",
    channels: m.channels.map(c => ({ name: c.name, type: c.type, default: c.default, stops: stopsText(c.stops) })),
  };
  presetApply = new Set(state.fixtures.filter(f => p.custom && f.profile === p.id).map(f => f.index));
  deleteArmed = false;
  renderPresetEditor();
}

function openPresets() { renderPresetList(); if (!preset) renderPresetEditor(); }

// Called on every state refresh while the tab is open; only redraws the
// parts that depend on the patch, so typing in the editor is not disturbed.
function renderPresets() {
  const sig = JSON.stringify(state.fixtures.map(f => [f.name, f.profile, f.address, f.end]));
  if (sig === presetFixSig) return;
  presetFixSig = sig;
  renderPresetList();
  renderApplyList();
}

function fixLabel(f) { return `${f.name} (ch ${f.address}${f.end > f.address ? "-" + f.end : ""})`; }

function renderPresetList() {
  const used = id => state.fixtures.filter(f => f.profile === id).length;
  const item = p => `<button data-pid="${esc(p.id)}" class="${preset && preset.id === p.id && p.custom ? "active" : ""}">
    <span>${esc(p.name)}</span><small>${p.modes[0].channels.length} ch${used(p.id) ? ` · on ${used(p.id)}` : ""}</small></button>`;
  const mine = profiles.filter(p => p.custom);
  $("#pr-mine").innerHTML = mine.map(item).join("") || `<p class="hint">None yet.</p>`;
  $("#pr-builtin").innerHTML = profiles.filter(p => !p.custom).map(item).join("");
  $$("#tab-presets [data-pid]").forEach(b => b.onclick = () => {
    const p = profiles.find(x => x.id === b.dataset.pid);
    if (p) { editFrom(p); renderPresetList(); }
  });
  const sel = $("#pr-fromfix"), keep = sel.value;
  sel.innerHTML = state.fixtures.map(f => `<option value="${f.index}">${esc(fixLabel(f))}</option>`).join("");
  if (keep && sel.querySelector(`option[value="${keep}"]`)) sel.value = keep;
  else { const spot = state.fixtures.find(f => f.kind === "moving"); if (spot) sel.value = spot.index; }
}

function renderApplyList() {
  if (!preset) return;
  $("#pr-apply").innerHTML = state.fixtures.map(f => {
    const on = presetApply.has(f.index), using = preset.id && f.profile === preset.id;
    return `<label class="check"><input type="checkbox" data-fx="${f.index}" ${on || using ? "checked" : ""} ${using ? "disabled" : ""}>
      ${esc(fixLabel(f))} <span class="meta">${using ? "uses this preset" : esc(f.profile_name)}</span></label>`;
  }).join("");
  $$("#pr-apply [data-fx]").forEach(cb => cb.onchange = () => { cb.checked ? presetApply.add(+cb.dataset.fx) : presetApply.delete(+cb.dataset.fx); });
}

function renderPresetEditor() {
  $("#pr-empty").classList.toggle("hidden", !!preset);
  $("#pr-editor").classList.toggle("hidden", !preset);
  if (!preset) return;
  $("#pr-title").textContent = preset.custom ? `Edit ${preset.name}` : preset.from ? `New preset from ${preset.from}` : "New preset";
  $("#pr-name").value = preset.name;
  $("#pr-mode").value = preset.mode;
  $("#pr-notes").value = preset.notes;
  $("#pr-save").textContent = preset.custom ? "Save changes" : "Save preset";
  $("#pr-saveas").classList.toggle("hidden", !preset.custom);
  $("#pr-delete").classList.toggle("hidden", !preset.custom);
  $("#pr-delete").textContent = "Delete";
  renderPresetChannels();
  renderApplyList();
}

function renderPresetChannels() {
  const types = state.channel_types;
  const n = preset.channels.length;
  $("#pr-chs tbody").innerHTML = preset.channels.map((c, j) => `<tr data-j="${j}">
    <td class="meta">${j + 1}</td>
    <td><input data-c="name" value="${esc(c.name)}" placeholder="Ch ${j + 1}"></td>
    <td><select data-c="type">${types.map(t => `<option value="${t}" ${t === c.type ? "selected" : ""}>${t.replace("_", " ")}</option>`).join("")}</select></td>
    <td><input type="number" min="0" max="255" data-c="default" value="${c.default}"></td>
    <td><input data-c="stops" value="${esc(c.stops)}" placeholder="${c.type === "color" ? "White=0, Red=14" : c.type === "gobo" ? "Open=0, Gobo 1=10" : ""}"></td>
    <td style="white-space:nowrap"><button data-m="up" title="Move up" ${j ? "" : "disabled"}>↑</button><button data-m="down" title="Move down" ${j < n - 1 ? "" : "disabled"}>↓</button><button data-m="del" title="Remove channel">✕</button></td>
  </tr>`).join("");
  $("#pr-count").textContent = `${n} channel${n === 1 ? "" : "s"}. Set the light to a ${n}-channel DMX mode.`;
  $$("#pr-chs tr[data-j]").forEach(row => {
    const j = +row.dataset.j, c = preset.channels[j];
    $$("[data-c]", row).forEach(inp => inp[inp.tagName === "SELECT" ? "onchange" : "oninput"] = () => {
      const k = inp.dataset.c;
      c[k] = k === "default" ? Math.max(0, Math.min(255, +inp.value || 0)) : inp.value;
      if (k === "type") {
        if (!c.name || c.name.startsWith("Ch ")) c.name = inp.selectedOptions[0].text.replace(/^./, x => x.toUpperCase());
        if ((c.type === "pan" || c.type === "tilt") && c.default === 0) c.default = 128;
        renderPresetChannels();
      }
    });
    $$("[data-m]", row).forEach(b => b.onclick = () => {
      const m = b.dataset.m, ch = preset.channels;
      if (m === "del") ch.splice(j, 1);
      if (m === "up" && j > 0) [ch[j - 1], ch[j]] = [ch[j], ch[j - 1]];
      if (m === "down" && j < ch.length - 1) [ch[j + 1], ch[j]] = [ch[j], ch[j + 1]];
      renderPresetChannels();
    });
  });
}

function presetMsg(text, err) {
  const el = $("#pr-msg");
  el.textContent = text;
  el.style.color = err ? "var(--err)" : "";
}

async function savePreset(asNew) {
  preset.name = $("#pr-name").value;
  preset.mode = $("#pr-mode").value;
  preset.notes = $("#pr-notes").value;
  if (!preset.name.trim()) { presetMsg("Give the preset a name.", true); $("#pr-name").focus(); return; }
  if (!preset.channels.length) { presetMsg("Add at least one channel.", true); return; }
  const channels = [];
  for (const [j, c] of preset.channels.entries()) {
    const { stops, bad } = parseStops(c.stops || "");
    if (bad.length) { presetMsg(`Channel ${j + 1} buttons: "${bad[0]}" should look like Name=value (0-255).`, true); return; }
    channels.push({ name: c.name, type: c.type, default: +c.default || 0, stops });
  }
  const body = {
    profile: { id: asNew ? "" : preset.id, name: preset.name, notes: preset.notes, modes: [{ name: preset.mode, channels }] },
    apply: [...presetApply],
  };
  let data;
  try { data = await api("/api/profile", body); } catch (e) { presetMsg(e.message || "Could not save.", true); return; }
  profiles = data.profiles;
  state = data.state;
  render();
  const p = data.profile;
  const on = state.fixtures.filter(f => f.profile === p.id).map(f => f.name);
  editFrom(p);
  presetFixSig = "";
  renderPresetList();
  presetMsg(`Saved ${p.name}.` + (on.length ? ` ${on.join(", ")} now use${on.length === 1 ? "s" : ""} it; open the Fixtures tab to try the sliders.` : " Tick a fixture above and save again to use it."));
  if (typeof patchDirty !== "undefined" && !patchDirty && typeof resetDraft === "function") resetDraft();
}

$("#pr-new").onclick = () => {
  preset = { id: "", custom: false, from: "", name: "", mode: "", notes: "", channels: [
    { name: "Pan", type: "pan", default: 128, stops: "" },
    { name: "Tilt", type: "tilt", default: 128, stops: "" },
    { name: "Dimmer", type: "dimmer", default: 0, stops: "" },
  ] };
  presetApply = new Set();
  deleteArmed = false;
  renderPresetEditor();
  renderPresetList();
  presetMsg("");
  $("#pr-name").focus();
};
$("#pr-copyfix").onclick = () => {
  const f = state.fixtures[+$("#pr-fromfix").value];
  if (!f) return;
  const p = profiles.find(x => x.id === f.profile);
  preset = {
    id: "", custom: false, from: f.name, name: p?.custom ? `${p.name} copy` : `My ${f.name}`, mode: `${f.channels.length}CH`, notes: "",
    channels: f.channels.map(c => ({ name: c.name, type: c.type, default: c.default, stops: stopsText(c.stops) })),
  };
  presetApply = new Set(state.fixtures.filter(x => x.profile === f.profile).map(x => x.index));
  deleteArmed = false;
  renderPresetEditor();
  renderPresetList();
  presetMsg("");
};
$("#pr-addch").onclick = () => {
  preset.channels.push({ name: `Ch ${preset.channels.length + 1}`, type: "generic", default: 0, stops: "" });
  renderPresetChannels();
};
$("#pr-save").onclick = () => savePreset(false);
$("#pr-saveas").onclick = () => savePreset(true);
// No confirm() here: the iPad app's web view does not show JavaScript dialogs.
$("#pr-delete").onclick = async () => {
  if (!deleteArmed) {
    deleteArmed = true;
    $("#pr-delete").textContent = "Tap again to delete";
    setTimeout(() => { deleteArmed = false; if ($("#pr-delete")) $("#pr-delete").textContent = "Delete"; }, 4000);
    return;
  }
  let data;
  try { data = await api("/api/profile/delete", { id: preset.id }); } catch (e) { return; }
  profiles = data.profiles;
  state = data.state;
  render();
  preset = null;
  presetFixSig = "";
  renderPresetEditor();
  renderPresetList();
};
