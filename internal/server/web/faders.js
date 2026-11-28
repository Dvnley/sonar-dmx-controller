"use strict";

// Faders tab: every DMX channel 1-512 by hand, 16 at a time like the scanner
// pages of a hardware desk. A fader you move takes over that channel until
// you hand it back.

let fdPage = loadPref("fdpage", 0); // page k = channels 16k+1 .. 16k+16
let fdSig = "";
let fdTouched = new Map(); // channel -> time, so a slow reply can't undo a drag

// which light (if any) uses a channel, and what that channel does
function chanOwner(ch) {
  for (const f of state.fixtures) {
    if (!f.enabled || f.universe !== 0) continue;
    if (ch >= f.address && ch <= f.end) return { light: f.name, what: f.channels[ch - f.address].name };
  }
  return null;
}

function pageLabel(k) {
  const names = [];
  for (let ch = k * 16 + 1; ch <= k * 16 + 16; ch++) {
    const o = chanOwner(ch);
    if (o && !names.includes(o.light)) names.push(o.light);
  }
  return names.join(", ");
}

function renderFaders() {
  if (tab !== "faders" || !state || !state.faders) return;
  const fs = state.faders;
  renderConn();
  const held = Object.keys(fs.set).length;
  $("#fd-held").textContent = held ? `${held} channel${held > 1 ? "s are" : " is"} set by hand` : "No channels set by hand: the app is in control";
  $("#fd-release").disabled = !held;

  // update values in place while nothing structural changed
  const sig = JSON.stringify([fdPage, state.fixtures.map(f => [f.name, f.address, f.end, f.enabled]), Object.keys(fs.set)]);
  if (sig !== fdSig) {
    fdSig = sig;
    const pages = [];
    for (let k = 0; k < 32; k++) pages.push(k);
    const used = pages.filter(k => pageLabel(k) || fs.set && Object.keys(fs.set).some(c => Math.ceil(c / 16) - 1 === k));
    const other = pages.filter(k => !used.includes(k));
    $("#fd-pages").innerHTML = used.map(k => `<button class="${k === fdPage ? "on" : ""}" data-k="${k}"><b>${k * 16 + 1}–${k * 16 + 16}</b><span>${esc(pageLabel(k) || "hand set")}</span></button>`).join("") +
      `<select id="fd-other"><option value="">Other channels…</option>${other.map(k => `<option value="${k}" ${k === fdPage ? "selected" : ""}>${k * 16 + 1}–${k * 16 + 16}</option>`).join("")}</select>`;
    $$("#fd-pages button").forEach(b => b.onclick = () => { fdPage = +b.dataset.k; savePref("fdpage", fdPage); fdSig = ""; renderFaders(); });
    $("#fd-other").onchange = e => { if (e.target.value !== "") { fdPage = +e.target.value; savePref("fdpage", fdPage); fdSig = ""; renderFaders(); } };

    let html = "";
    for (let ch = fdPage * 16 + 1; ch <= fdPage * 16 + 16; ch++) {
      const o = chanOwner(ch);
      html += `<div class="fd-col ${ch in fs.set ? "held" : ""}" data-ch="${ch}">
        <b class="fd-val">${fs.output[ch - 1]}</b>
        <input type="range" min="0" max="255" value="${fs.output[ch - 1]}" data-ch="${ch}" aria-label="Channel ${ch}">
        <span class="fd-num">${ch}</span>
        <span class="fd-what" title="${o ? esc(o.light + ": " + o.what) : ""}">${o ? esc(o.what) : "—"}</span>
        <button class="fd-free" data-free="${ch}" title="Hand back to the app">${ch in fs.set ? "Auto" : ""}</button>
      </div>`;
    }
    $("#fd-bank").innerHTML = html;
    $$("#fd-bank input").forEach(inp => inp.oninput = () => {
      const ch = +inp.dataset.ch;
      fdTouched.set(ch, Date.now());
      inp.closest(".fd-col").classList.add("held");
      $(".fd-val", inp.closest(".fd-col")).textContent = inp.value;
      throttled("fd" + ch, () => api("/api/faders", { values: { [ch]: +inp.value } }));
    });
    $$("#fd-bank [data-free]").forEach(b => b.onclick = () => { if (b.textContent) api("/api/faders", { release: [+b.dataset.free] }); });
  }
  for (const col of $$("#fd-bank .fd-col")) {
    const ch = +col.dataset.ch, inp = $("input", col);
    if (Date.now() - (fdTouched.get(ch) || 0) < 1500) continue;
    const v = fs.output[ch - 1];
    inp.value = v;
    $(".fd-val", col).textContent = v;
  }
}

function renderConn() {
  const el = $("#fd-conn");
  if (!diag) { el.className = "fd-conn bad"; el.innerHTML = "<b>The app is not answering.</b> Is it still running?"; return; }
  if (diag.an2_online) {
    el.className = "fd-conn ok";
    el.innerHTML = `<b>Connected to the AN2 (${esc(diag.target)}).</b> If a light still stays dark, check its DMX cable and the address set on the light itself.`;
  } else if (diag.artnet_ip_ok) {
    el.className = "fd-conn mid";
    el.innerHTML = `<b>The Mac is on the lighting network, but the AN2 (${esc(diag.target)}) is not answering.</b> Check that the AN2 has power and its network cable is plugged in.`;
  } else {
    el.className = "fd-conn bad";
    el.innerHTML = `<b>The Mac can't reach the AN2.</b> Plug in the USB network adapter, then in System Settings › Network set it to IP address <code>2.0.0.2</code>, subnet mask <code>255.0.0.0</code> (Configure IPv4: Manually).`;
  }
}

const fdSetPage = v => {
  const vals = {};
  for (let ch = fdPage * 16 + 1; ch <= fdPage * 16 + 16; ch++) vals[ch] = v;
  fdSig = "";
  api("/api/faders", { values: vals });
};
$("#fd-full").onclick = () => fdSetPage(255);
$("#fd-zero").onclick = () => fdSetPage(0);
$("#fd-release").onclick = () => { fdSig = ""; api("/api/faders", { release_all: true }); };
