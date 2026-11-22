"use strict";
/* ZipNest — dialogs */

// ------------------------------------------------------------------ modal framework
function modal(o) {
  const ov = h("div", { class: "overlay" });
  const m = h("div", { class: "modal " + (o.size || ""), role: "dialog", "aria-modal": "true" });
  const head = h("div", { class: "m-head" },
    o.icon ? h("div", { class: "m-ic " + (o.iconClass || ""), html: icon(o.icon) }) : null,
    h("h3", null, o.title, o.sub ? h("small", { title: o.sub }, o.sub) : null),
    o.closable === false ? null : h("button", { class: "icon-btn", "aria-label": "Close", onclick: () => dismiss() }, ic("x")));
  const body = h("div", { class: "m-body" });
  const foot = h("div", { class: "m-foot" });
  m.append(head);
  let tabsEl = null;
  if (o.tabs) {
    tabsEl = h("div", { class: "m-tabs" });
    m.append(tabsEl);
  }
  m.append(body, foot);
  ov.append(m);
  const prevFocus = document.activeElement;
  let closed = false;
  const close = () => {
    if (closed) return;
    closed = true;
    document.removeEventListener("keydown", onKey, true);
    ov.remove();
    if (prevFocus && prevFocus.focus && document.body.contains(prevFocus)) prevFocus.focus();
  };
  const dismiss = () => { if (o.onDismiss) o.onDismiss(); else close(); };
  ov.addEventListener("dismiss", dismiss);
  const onKey = (e) => {
    if ($$(".overlay").pop() !== ov) return; // only the topmost dialog reacts
    if (e.key === "Escape") { e.stopPropagation(); e.preventDefault(); dismiss(); }
    if (e.key === "Enter" && !e.shiftKey && e.target.matches("input:not([type=checkbox]):not([type=radio]), select")) {
      const p = foot.querySelector(".btn.primary:not(:disabled)");
      if (p) { e.preventDefault(); p.click(); }
    }
    if (e.key === "Tab") { // keep focus inside
      const f = $$("button:not(:disabled), input:not(:disabled), select, textarea, [tabindex='0']", m).filter((x) => x.offsetParent);
      if (!f.length) return;
      if (e.shiftKey && document.activeElement === f[0]) { e.preventDefault(); f[f.length - 1].focus(); }
      else if (!e.shiftKey && (document.activeElement === f[f.length - 1] || !m.contains(document.activeElement))) { e.preventDefault(); f[0].focus(); }
    }
  };
  document.addEventListener("keydown", onKey, true);
  document.body.append(ov);
  const d = { ov, m, body, foot, head, close, dismiss, tabsEl };
  d.buttons = (list) => {
    foot.innerHTML = "";
    list.forEach((b) => {
      if (b === "grow") { foot.append(h("div", { class: "grow" })); return; }
      if (b instanceof Node) { foot.append(b); return; }
      const el = h("button", { class: "btn " + (b.kind || ""), id: b.id, onclick: b.run }, b.icon ? ic(b.icon) : null, b.label);
      foot.append(el);
      if (b.kind === "primary") d.primary = el;
    });
  };
  d.setTabs = (tabs, onChange) => {
    tabsEl.innerHTML = "";
    tabs.forEach(([id, label], i) => tabsEl.append(h("button", { class: i === 0 ? "on" : "", "data-tab": id, onclick: (e) => { $$("button", tabsEl).forEach((b) => b.classList.toggle("on", b === e.currentTarget)); onChange(id); } }, label)));
  };
  if (o.buttons) d.buttons(o.buttons);
  setTimeout(() => {
    const f = (o.focus === "primary" && d.primary) || m.querySelector("[autofocus]") || m.querySelector(".m-body input:not([type=checkbox]):not([type=radio]), .m-body select, .m-body textarea") || d.primary || m.querySelector("button");
    if (f) { f.focus(); if (f.select && f.dataset.noselect === undefined) try { f.select(); } catch (e) {} }
  }, 30);
  return d;
}

// small form builders
function field(label, control, hint) {
  return h("div", { class: "field" }, label ? h("label", null, label) : null, control, hint ? h("div", { class: "hint" }, hint) : null);
}
function checkbox(label, checked, onchange, small, opts = {}) {
  const inp = h("input", { type: opts.radio ? "radio" : "checkbox", checked, name: opts.name, disabled: opts.disabled, onchange: (e) => onchange && onchange(e.target.checked) });
  const el = h("label", { class: "check" + (opts.radio ? " radio" : ""), title: opts.title }, inp, h("span", { class: "box", html: icon("check") }),
    h("span", { class: "txt" }, h("b", null, label), small ? h("small", null, small) : null));
  el.input = inp;
  return el;
}
function toggle(label, checked, onchange, small, disabled) {
  const inp = h("input", { type: "checkbox", checked, disabled, onchange: (e) => onchange && onchange(e.target.checked) });
  const el = h("label", { class: "switch" }, h("span", { class: "txt" }, h("b", null, label), small ? h("small", null, small) : null), inp, h("span", { class: "tr" }));
  el.input = inp;
  return el;
}
function segmented(options, value, onchange) {
  const el = h("div", { class: "seg" });
  options.forEach(([v, label, ico]) => el.append(h("button", { class: v === value ? "on" : "", onclick: (e) => { $$("button", el).forEach((b) => b.classList.toggle("on", b === e.currentTarget)); onchange(v); } }, ico ? ic(ico) : null, label)));
  return el;
}
function select(options, value, onchange) {
  const s = h("select", { class: "input", onchange: (e) => onchange(e.target.value) });
  options.forEach(([v, label]) => s.append(h("option", { value: v, selected: String(v) === String(value) ? "" : null }, label)));
  s.value = value;
  return s;
}
function passwordInput(value, oninput, placeholder) {
  const inp = h("input", { class: "input", type: "password", value: value || "", placeholder: placeholder || "", autocomplete: "new-password", oninput: (e) => oninput(e.target.value), spellcheck: "false" });
  const eye = h("button", { class: "icon-btn", type: "button", title: "Show password", onclick: () => { const show = inp.type === "password"; inp.type = show ? "text" : "password"; eye.innerHTML = icon(show ? "eyeoff" : "eye"); } }, ic("eye"));
  const w = h("div", { class: "pw-wrap" }, inp, eye);
  w.input = inp;
  return w;
}

// ------------------------------------------------------------------ basic dialogs
function confirmDialog(o) {
  return new Promise((resolve) => {
    const d = modal({ title: o.title, icon: o.danger ? "alert" : o.icon || "help", iconClass: o.danger ? "red" : "", size: "narrow", onDismiss: () => { d.close(); resolve(false); } });
    d.body.append(h("p", null, o.text));
    d.buttons([{ label: o.cancel || "Cancel", run: () => { d.close(); resolve(false); } }, { label: o.ok || "OK", kind: o.danger ? "danger primary" : "primary", run: () => { d.close(); resolve(true); } }]);
  });
}
function choiceDialog(o) {
  return new Promise((resolve) => {
    const d = modal({ title: o.title, icon: o.icon || "help", size: "narrow", onDismiss: () => { d.close(); resolve(null); } });
    d.body.append(h("p", null, o.text));
    d.buttons([{ label: "Cancel", run: () => { d.close(); resolve(null); } }, "grow", ...o.choices.map((c) => ({ label: c.label, kind: c.primary ? "primary" : "", run: () => { d.close(); resolve(c.value); } }))]);
  });
}
function errorDialog(title, text, details) {
  return new Promise((resolve) => {
    const d = modal({ title, icon: "alert", iconClass: "red", size: "narrow", onDismiss: () => { d.close(); resolve(); } });
    const lines = String(text || "").split("\n");
    d.body.append(h("p", { style: { userSelect: "text" } }, lines[0]));
    if (lines.length > 1 || details) d.body.append(h("div", { class: "msg-list" }, (details || lines.slice(1)).join("\n")));
    d.buttons([{ label: "OK", kind: "primary", run: () => { d.close(); resolve(); } }]);
  });
}
function promptDialog(o) {
  return new Promise((resolve) => {
    const d = modal({ title: o.title, icon: o.icon || "pencil", size: "narrow", onDismiss: () => { d.close(); resolve(null); } });
    const inp = h("input", { class: "input", value: o.value || "", spellcheck: "false", "data-noselect": o.selectStem ? "" : null });
    d.body.append(field(o.label, inp));
    d.buttons([{ label: "Cancel", run: () => { d.close(); resolve(null); } }, { label: o.ok || "OK", kind: "primary", run: () => { const v = inp.value.trim(); d.close(); resolve(v || null); } }]);
    if (o.selectStem) setTimeout(() => { const v = inp.value, i = v.lastIndexOf("."); inp.focus(); inp.setSelectionRange(0, i > 0 ? i : v.length); }, 40);
  });
}
function passwordDialog(archive, wrong) {
  return new Promise((resolve) => {
    const d = modal({ title: "Enter password", sub: baseName(archive), icon: "key", size: "narrow", onDismiss: () => { d.close(); resolve(null); } });
    let pw = "";
    const pi = passwordInput("", (v) => (pw = v), "Password");
    if (wrong) pi.input.classList.add("bad");
    d.body.append(h("p", null, wrong ? "The password is incorrect. Please try again." : "This archive is encrypted. Enter the password to continue."), field(null, pi));
    d.buttons([{ label: "Cancel", run: () => { d.close(); resolve(null); } }, { label: "OK", kind: "primary", run: () => { d.close(); resolve(pw); } }]);
  });
}

// ------------------------------------------------------------------ progress
const TASK_TITLES = { extract: "Extracting", create: "Creating archive", add: "Adding files", delete: "Deleting", rename: "Renaming", test: "Testing", convert: "Converting", repair: "Repairing", comment: "Saving comment", benchmark: "Benchmark", find: "Searching" };
function runTask(params, opts = {}) {
  return new Promise(async (resolve, reject) => {
    let id;
    try { id = (await api("task/start", params)).id; } catch (e) { reject(e); return; }
    const title = opts.title || TASK_TITLES[params.kind] || "Working";
    let view = null, tray = null, shownAt = 0, last = null;
    const samples = [];
    const cancel = () => api("task/cancel", { id }).catch(() => {});
    const build = () => {
      const d = modal({ title, sub: params.archive || (params.archives && params.archives[0]) || params.output || "", icon: { extract: "extract", create: "add", add: "add", test: "test", convert: "convert", repair: "repair", delete: "trash", benchmark: "gauge", find: "search" }[params.kind] || "zap", closable: false, onDismiss: () => toBackground() });
      const pct = h("div", { class: "prog-pct" }, "0%");
      const stage = h("div", { class: "hint" });
      const file = h("div", { class: "prog-file" }, "‎");
      const bar = h("div", { class: "bar indet" }, h("div", { style: { width: "0%" } }));
      const stat = (label) => { const b = h("b", null, "—"); return [h("div", null, h("small", null, label), b), b]; };
      const [e1, elapsed] = stat("Elapsed"), [e2, remaining] = stat("Remaining"), [e3, speed] = stat("Speed"), [e4, processed] = stat("Processed");
      d.body.append(h("div", { style: { display: "flex", alignItems: "baseline", justifyContent: "space-between" } }, pct, stage), bar, file, h("div", { class: "prog-stats" }, e1, e2, e3, e4));
      d.buttons([{ label: "Background", icon: "minimize", run: () => toBackground() }, "grow", { label: "Cancel", run: () => { cancel(); d.foot.querySelectorAll("button").forEach((b) => (b.disabled = true)); stage.textContent = "Cancelling…"; } }]);
      return { d, pct, stage, file, bar, elapsed, remaining, speed, processed };
    };
    const toBackground = () => {
      if (opts.foreground && S.standalone) return;
      if (view) { view.d.close(); view = null; }
      if (!tray) {
        tray = h("div", { class: "tray-card", title: "Show progress", onclick: () => { tray.remove(); tray = null; view = build(); if (last) update(last); } },
          h("div", { class: "row1" }, h("span", null, title), h("span", { class: "tp" }, "")), h("div", { class: "bar" }, h("div", { style: { width: "0%" } })), h("div", { class: "hint tf", style: { whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" } }));
        $("#tray").append(tray);
      }
      if (last) update(last);
    };
    const update = (st) => {
      last = st;
      const now = Date.now();
      samples.push([now, st.done]);
      while (samples.length > 2 && now - samples[0][0] > 3000) samples.shift();
      const rate = samples.length > 1 ? ((st.done - samples[0][1]) / (now - samples[0][0])) * 1000 : 0;
      const frac = st.total > 0 ? Math.min(1, st.done / st.total) : null;
      const pctText = frac == null ? "" : Math.floor(frac * 100) + "%";
      if (view) {
        view.pct.textContent = pctText || (st.kind === "find" ? st.done + " found" : "…");
        view.stage.textContent = st.stage || "";
        view.file.textContent = st.file ? "‎" + st.file : "‎";
        view.bar.classList.toggle("indet", frac == null);
        view.bar.firstChild.style.width = (frac == null ? 0 : frac * 100) + "%";
        view.elapsed.textContent = fmtDuration(st.elapsed);
        view.remaining.textContent = frac && rate > 0 ? fmtDuration(((st.total - st.done) / rate) * 1000) : "—";
        view.speed.textContent = rate > 0 && st.kind !== "find" ? fmtSize(rate) + "/s" : "—";
        view.processed.textContent = st.kind === "find" ? String(st.done) : st.done ? fmtSize(st.done) + (st.total ? " / " + fmtSize(st.total) : "") : "—";
      }
      if (tray) {
        $(".tp", tray).textContent = pctText;
        $(".bar", tray).classList.toggle("indet", frac == null);
        $(".bar > div", tray).style.width = (frac == null ? 0 : frac * 100) + "%";
        $(".tf", tray).textContent = st.file || st.stage || "";
      }
    };
    const showTimer = setTimeout(() => { view = build(); shownAt = Date.now(); if (last) update(last); }, opts.foreground ? 0 : 280);
    for (;;) {
      await new Promise((r) => setTimeout(r, view || tray ? 150 : 60));
      let st;
      try { st = await api("task/status", { id }); } catch (e) { clearTimeout(showTimer); if (view) view.d.close(); if (tray) tray.remove(); reject(e); return; }
      update(st);
      if (st.state === "running") continue;
      clearTimeout(showTimer);
      if (view && Date.now() - shownAt < 450) { update(Object.assign({}, st, { done: st.total || st.done })); await new Promise((r) => setTimeout(r, 450 - (Date.now() - shownAt))); }
      if (view) view.d.close();
      if (tray) { tray.remove(); if (st.state === "done") toast(title + " finished", { kind: "ok" }); }
      if (st.state === "done") resolve(st.result);
      else { const e = new ApiError(st.error || "Failed", st.code); e.result = st.result; reject(e); }
      return;
    }
  });
}

// ------------------------------------------------------------------ file & folder picker
function pickerDialog(o) {
  // o.mode: "folder" | "files" | "save" | "file"
  return new Promise((resolve) => {
    const d = modal({ title: o.title, icon: o.mode === "folder" ? "folderopen" : o.mode === "save" ? "add" : "file", size: "wide", onDismiss: () => { d.close(); resolve(null); } });
    let dir = o.start || S.init.home, parent = null, items = [];
    const sel = new Set();
    const pathInp = h("input", { class: "input", spellcheck: "false", onkeydown: (e) => { if (e.key === "Enter") { e.preventDefault(); e.stopPropagation(); go(pathInp.value.trim()); } } });
    const upBtn = h("button", { class: "icon-btn", title: "Up", onclick: () => parent != null && go(parent) }, ic("up"));
    const list = h("div", { class: "picker-list" });
    const side = h("div", { class: "picker-side" });
    const nameInp = o.mode === "save" ? h("input", { class: "input", value: o.name || "", spellcheck: "false" }) : null;
    const status = h("span", { class: "hint" });
    S.init.places.concat(S.init.drives).forEach((p) => side.append(h("div", { class: "nav-item", onclick: () => go(p.path) }, ic(PLACE_ICONS[p.icon] || "folder"), h("span", null, p.name))));
    d.body.append(h("div", { class: "picker" },
      h("div", { class: "picker-top" }, upBtn, pathInp,
        o.mode !== "file" ? h("button", { class: "icon-btn", title: "New folder", onclick: async () => { const n = await promptDialog({ title: "New folder", label: "Folder name", value: "New folder", ok: "Create" }); if (n) { try { const r = await api("fs/mkdir", { dir, name: n }); await go(dir); if (o.mode === "folder") go(r.path); } catch (e) { errorDialog("Could not create the folder", e.message); } } } }, ic("folderplus")) : null),
      h("div", { class: "picker-main" }, side, list)));
    if (nameInp) d.body.append(h("div", { style: { marginTop: "12px" } }, field("File name", nameInp)));
    const draw = () => {
      list.innerHTML = "";
      if (!items.length) list.append(h("div", { class: "nav-empty", style: { padding: "16px" } }, o.mode === "folder" ? "No subfolders" : "This folder is empty"));
      items.forEach((it) => {
        const isSel = sel.has(it.path);
        const multi = o.mode === "files";
        const row = h("div", { class: "p-item" + (isSel ? " sel" : ""), title: it.path },
          multi ? h("span", { class: "check", style: { padding: 0 }, onclick: (e) => { e.stopPropagation(); toggleSel(it); } }, h("input", { type: "checkbox", checked: isSel }), h("span", { class: "box", html: icon("check") })) : null,
          h("span", { html: tile(it) , style: { display: "contents" } }), h("span", null, it.name), !it.dir ? h("small", null, fmtSize(it.size)) : null);
        row.addEventListener("click", (e) => {
          if (it.dir && (!multi || !(e.ctrlKey || e.shiftKey))) {
            if (o.mode === "folder" || IS_MOBILE() || !multi) { go(it.path); return; }
          }
          if (multi) { if (e.ctrlKey || e.metaKey || IS_MOBILE() || !it.dir) toggleSel(it, !(e.ctrlKey || e.metaKey) && !IS_MOBILE()); else go(it.path); }
          else if (o.mode === "file" && !it.dir) { sel.clear(); sel.add(it.path); draw(); }
          else if (o.mode === "save" && !it.dir) { nameInp.value = it.name; }
        });
        row.addEventListener("dblclick", () => { if (it.dir) go(it.path); else if (o.mode === "file" || o.mode === "files") { sel.clear(); sel.add(it.path); finish(); } });
        list.append(row);
      });
      status.textContent = o.mode === "files" ? (sel.size ? sel.size + " selected" : "Tick files or folders, or double-click a folder to open it") : "";
    };
    const toggleSel = (it, single) => { if (single) { const had = sel.has(it.path) && sel.size === 1; sel.clear(); if (!had) sel.add(it.path); } else if (sel.has(it.path)) sel.delete(it.path); else sel.add(it.path); draw(); };
    const go = async (p) => {
      try {
        const r = await api("fs/list", { dir: p, dirsOnly: o.mode === "folder" });
        dir = r.dir; parent = r.parent;
        items = r.items.filter((x) => pref("showHidden", false) || !x.hidden).map((x) => ({ name: x.name, path: x.path, dir: x.dir, size: x.size, kind: x.dir ? (r.dir ? "folder" : "drive") : "file" }));
        items.sort((a, b) => (a.dir !== b.dir ? (a.dir ? -1 : 1) : collator.compare(a.name, b.name)));
        if (o.mode !== "files") sel.clear();
        pathInp.value = dir || "This PC";
        upBtn.disabled = parent == null;
        list.scrollTop = 0;
        draw();
      } catch (e) { errorDialog("Cannot open folder", e.message); }
    };
    const finish = () => {
      if (o.mode === "folder") { if (!dir) return; d.close(); resolve(dir); }
      else if (o.mode === "save") { const n = nameInp.value.trim(); if (!n || !dir) return; d.close(); resolve(joinPath(dir, n)); }
      else { if (!sel.size) return; d.close(); resolve(o.mode === "file" ? [...sel][0] : [...sel]); }
    };
    d.buttons([status, "grow", { label: "Cancel", run: () => { d.close(); resolve(null); } }, { label: o.ok || (o.mode === "folder" ? "Select folder" : "OK"), kind: "primary", run: finish }]);
    go(dir);
  });
}

// ------------------------------------------------------------------ Add (create archive)
const FORMATS = [
  ["nest", "NEST", "Smallest, repairable", "Best"],
  ["zip", "ZIP", "Opens anywhere"],
  ["7z", "7Z", "High compression"],
  ["tar.gz", "TAR.GZ", "Linux, macOS"],
  ["tar.xz", "TAR.XZ", "Smaller tarball"],
  ["tar.zst", "TAR.ZST", "Fast tarball"],
  ["tar", "TAR", "No compression"],
];
const LEVELS = [[0, "Store"], [1, "Fastest"], [3, "Fast"], [5, "Normal"], [7, "Good"], [9, "Best"]];
const LEVEL_HINTS = { 0: "No compression — just packs the files together.", 1: "Very fast, larger archive.", 3: "Fast with good compression.", 5: "A good balance of speed and size.", 7: "Smaller archive, slower.", 9: "Smallest archive, slowest." };
const VOLUMES = [[0, "Don't split"], [10 << 20, "10 MB"], [25 << 20, "25 MB (email)"], [100 << 20, "100 MB"], [700 << 20, "700 MB (CD)"], [4092 << 20, "4 GB (FAT32)"], [4700 * 1000 * 1000, "4.7 GB (DVD)"], ["custom", "Custom…"]];
const extFor = (f, sfx) => (sfx ? "exe" : f);
function replaceExt(p, f, sfx) {
  const b = baseName(p);
  const st = b.replace(/\.(tar\.(gz|xz|zst|bz2)|nest|zip|7z|tar|exe|tgz|txz)$/i, "");
  return p.slice(0, p.length - b.length) + st + "." + extFor(f, sfx);
}

async function addDialog(inputs, opts = {}) {
  const fmt0 = pref("defaultFormat", "nest");
  let suggested;
  try { suggested = (await api("suggest", { inputs, format: fmt0 })).path; } catch (e) { suggested = joinPath(S.init.home, "archive." + fmt0); }
  if (opts.outDir) suggested = joinPath(opts.outDir, baseName(suggested));
  const st = {
    output: suggested, format: fmt0, level: pref("defaultLevel", 5), volume: 0, customVol: "", sfx: false, recovery: 0, lock: false,
    testAfter: pref("testAfter", false), deleteAfter: false, addDate: false, password: "", confirm: "", encryptNames: false, comment: "", exclude: pref("exclude", ""), inputs: inputs.slice(),
  };
  return new Promise((resolve) => {
    const d = modal({ title: "Archive name and parameters", sub: inputs.length === 1 ? inputs[0] : plural(inputs.length, "item") + " selected", icon: "add", size: "wide", tabs: true, focus: "primary", onDismiss: () => { d.close(); resolve(null); } });
    const pages = { general: h("div"), password: h("div", { class: "off" }), advanced: h("div", { class: "off" }), files: h("div", { class: "off" }) };
    d.setTabs([["general", "General"], ["password", "Password"], ["advanced", "Advanced"], ["files", "Files (" + inputs.length + ")"]], (id) => { for (const k in pages) pages[k].classList.toggle("off", k !== id); });
    d.body.append(h("div", { class: "tabpages" }, ...Object.values(pages)));

    // --- general
    const outInp = h("input", { class: "input", value: st.output, spellcheck: "false", oninput: (e) => (st.output = e.target.value) });
    const browse = h("button", { class: "btn", onclick: async () => { const p = await pickerDialog({ title: "Save archive as", mode: "save", start: dirName(st.output), name: baseName(st.output), ok: "Save" }); if (p) { st.output = replaceExt(p, st.format, st.sfx); outInp.value = st.output; } } }, ic("folderopen"), "Browse…");
    const cards = h("div", { class: "cards" });
    const levelHint = h("div", { class: "hint" });
    const range = h("input", { type: "range", class: "range", min: 0, max: LEVELS.length - 1, step: 1, value: Math.max(0, LEVELS.findIndex((l) => l[0] >= st.level)), oninput: (e) => { st.level = LEVELS[+e.target.value][0]; drawLevel(); } });
    const levelName = h("b", { style: { color: "var(--accent)" } });
    const drawLevel = () => { const l = LEVELS.find((x) => x[0] === st.level) || LEVELS[3]; levelName.textContent = l[1]; levelHint.textContent = LEVEL_HINTS[l[0]]; };
    const customVol = h("input", { class: "input hidden", placeholder: "e.g. 50 MB", style: { marginTop: "6px" }, oninput: (e) => (st.customVol = e.target.value) });
    const volSel = select(VOLUMES.map(([v, l]) => [v, l]), 0, (v) => { st.volume = v === "custom" ? "custom" : +v; customVol.classList.toggle("hidden", v !== "custom"); sync(); });
    const optTest = checkbox("Test the archive after creating it", st.testAfter, (v) => (st.testAfter = v), "Checks that every file can be read back");
    const optDel = checkbox("Delete files after archiving", false, (v) => (st.deleteAfter = v), S.init.platform === "windows" ? "Originals go to the Recycle Bin" : "Originals are deleted permanently");
    const optDate = checkbox("Add today's date to the name", false, (v) => (st.addDate = v));
    const optSfx = checkbox("Create a self-extracting archive (.exe)", false, (v) => { st.sfx = v; if (v) st.format = "zip"; st.output = replaceExt(st.output, st.format, st.sfx); outInp.value = st.output; sync(); }, "Extracts itself on any Windows PC — no software needed");
    const recSel = select([[1, "1%"], [3, "3%"], [5, "5%"], [10, "10%"], [20, "20%"]], 3, (v) => { st.recovery = +v; });
    recSel.style.width = "90px"; recSel.style.height = "30px";
    const optRec = checkbox("Add recovery record", false, (v) => { st.recovery = v ? +recSel.value : 0; recSel.disabled = !v; }, "Lets ZipNest repair the archive if it gets damaged");
    recSel.disabled = true;
    const optLock = checkbox("Lock archive", false, (v) => (st.lock = v), "Prevents further changes to the archive");
    const fmtNote = h("div", { class: "hint", style: { marginTop: "6px" } });
    pages.general.append(
      field("Archive name", h("div", { class: "input-row" }, outInp, browse)),
      field("Archive format", cards, null),
      fmtNote,
      h("div", { class: "grid2", style: { marginTop: "14px" } },
        h("div", null,
          h("div", { class: "field" }, h("label", { style: { display: "flex", justifyContent: "space-between" } }, "Compression", levelName), range,
            h("div", { class: "range-labels" }, h("span", null, "Faster"), h("span", null, "Smaller")), levelHint),
          field("Split into volumes", h("div", null, volSel, customVol), "Creates parts like name.zip.001, .002 … for email or USB drives")),
        h("div", null, h("div", { class: "label", style: { marginBottom: "4px" } }, "Options"),
          h("div", { class: "box-group" }, optTest, optDel, optDate, S.init.sfx ? optSfx : null, h("div", { style: { display: "flex", alignItems: "center", gap: "8px" } }, optRec, recSel), optLock))));

    // --- password
    const pw1 = passwordInput("", (v) => { st.password = v; sync(); }, "Password");
    const pw2 = passwordInput("", (v) => { st.confirm = v; sync(); }, "Repeat the password");
    const strength = h("div", { class: "bar", style: { height: "5px", margin: "-6px 0 4px" } }, h("div", { style: { width: "0%" } }));
    const strengthTxt = h("div", { class: "hint" });
    const optNames = checkbox("Encrypt file names", false, (v) => (st.encryptNames = v), "Nobody can see what's inside without the password");
    const pwNote = h("div", { class: "hint" });
    pages.password.append(
      h("p", null, "Protect the archive with a password. Files are encrypted with AES-256 — without the password they cannot be opened, so keep it somewhere safe."),
      h("div", { class: "grid2" }, field("Password", pw1), field("Confirm password", pw2)), strength, strengthTxt,
      h("div", { class: "box-group", style: { marginTop: "12px" } }, optNames), pwNote);

    // --- advanced
    const comment = h("textarea", { class: "input", rows: 4, placeholder: "Shown when the archive is opened", oninput: (e) => (st.comment = e.target.value) });
    const exclude = h("input", { class: "input", value: st.exclude, placeholder: "*.tmp; *.log; Thumbs.db; node_modules", spellcheck: "false", oninput: (e) => (st.exclude = e.target.value) });
    const commentField = field("Archive comment", comment, "Saved in ZIP and NEST archives");
    pages.advanced.append(field("Exclude files", exclude, "Files and folders matching these patterns are skipped. Separate patterns with semicolons."), commentField,
      checkbox("Remember format and compression as default", false, (v) => (st.saveDefault = v)));

    // --- files
    const flist = h("div", { class: "results", style: { maxHeight: "300px" } });
    const drawFiles = () => {
      flist.innerHTML = "";
      st.inputs.forEach((p, i) => flist.append(h("div", { class: "p-item" }, h("span", { html: tile({ name: baseName(p), dir: !/\.[a-z0-9]{1,5}$/i.test(p) }), style: { display: "contents" } }),
        h("div", null, h("b", null, baseName(p)), h("small", { class: "hint" }, dirName(p))),
        h("button", { class: "icon-btn", title: "Remove", onclick: () => { st.inputs.splice(i, 1); drawFiles(); sync(); } }, ic("x")))));
      $('[data-tab="files"]', d.tabsEl).textContent = "Files (" + st.inputs.length + ")";
    };
    pages.files.append(flist, h("div", { style: { marginTop: "10px" } }, h("button", { class: "btn", onclick: async () => { const more = await pickerDialog({ title: "Add more files", mode: "files", start: dirName(st.inputs[0] || S.init.home), ok: "Add" }); if (more) { more.forEach((m) => !st.inputs.includes(m) && st.inputs.push(m)); drawFiles(); sync(); } } }, ic("plus"), "Add files…")));
    drawFiles();

    const sync = () => {
      cards.innerHTML = "";
      FORMATS.forEach(([f, label, desc, badge]) => cards.append(h("button", { type: "button", class: "card-opt" + (st.format === f ? " on" : ""), disabled: st.sfx && f !== "zip" ? "" : null, style: st.sfx && f !== "zip" ? { opacity: 0.4 } : null,
        onclick: () => { if (st.sfx && f !== "zip") return; st.format = f; st.output = replaceExt(st.output, f, st.sfx); outInp.value = st.output; sync(); } },
        badge ? h("span", { class: "badge" }, badge) : null, h("b", null, label), h("small", null, desc))));
      const isNest = st.format === "nest", isTar = st.format.startsWith("tar");
      fmtNote.textContent = isNest ? "NEST is ZipNest's own format. Recipients need ZipNest (Windows or Android) to open it — choose ZIP to share with anyone." : "";
      optSfx.input.disabled = !!st.volume;
      optRec.input.disabled = !isNest; recSel.disabled = !isNest || !st.recovery;
      if (!isNest) { optRec.input.checked = false; st.recovery = 0; }
      optLock.input.disabled = !isNest;
      if (!isNest) { optLock.input.checked = false; st.lock = false; }
      pw1.input.disabled = pw2.input.disabled = isTar;
      optNames.input.disabled = isTar || !(isNest || st.format === "7z");
      if (optNames.input.disabled) { optNames.input.checked = false; st.encryptNames = false; }
      pwNote.textContent = isTar ? "TAR archives cannot be password-protected. Choose NEST, ZIP or 7Z." : st.format === "zip" ? "ZIP uses AES-256 (WinZip compatible). File names stay visible in ZIP archives." : "";
      comment.disabled = !(isNest || st.format === "zip");
      const score = pwScore(st.password);
      strength.firstChild.style.width = st.password ? score * 25 + "%" : "0%";
      strength.firstChild.style.background = ["var(--red)", "var(--red)", "var(--amber)", "var(--green)", "var(--green)"][score];
      strengthTxt.textContent = st.password ? ["Very weak", "Weak", "Fair", "Strong", "Very strong"][score] + " password" : "";
      pw2.input.classList.toggle("bad", !!st.confirm && st.confirm !== st.password);
      const pwTab = $('[data-tab="password"]', d.tabsEl);
      pwTab.innerHTML = "";
      pwTab.append(st.password ? h("span", { style: { display: "inline-flex", gap: "5px", alignItems: "center" } }, ic("lock"), "Password") : "Password");
      if (d.primary) d.primary.disabled = !st.inputs.length;
    };
    drawLevel();

    const submit = async () => {
      if (!st.inputs.length) return;
      if (st.password && st.password !== st.confirm) { $('[data-tab="password"]', d.tabsEl).click(); pw2.input.focus(); pw2.input.classList.add("bad"); return; }
      let out = st.output.trim();
      if (!out) { outInp.focus(); return; }
      if (!/[\\/]/.test(out)) out = joinPath(dirName(st.inputs[0]), out);
      if (st.addDate) { const dt = new Date().toISOString().slice(0, 10); const b = baseName(out), m = b.match(/^(.*?)(\.(tar\.\w+|\w+))$/); out = out.slice(0, out.length - b.length) + (m ? m[1] + " " + dt + m[2] : b + " " + dt); }
      if (!new RegExp("\\." + extFor(st.format, st.sfx).replace(".", "\\.") + "$", "i").test(out)) out += "." + extFor(st.format, st.sfx);
      let volume = st.volume;
      if (volume === "custom") { volume = parseSize(st.customVol); if (!volume) { customVol.classList.add("bad"); customVol.focus(); return; } }
      let mode = "";
      const ex = await api("fs/stat", { path: out }).catch(() => ({ exists: false }));
      if (ex.exists) {
        mode = await choiceDialog({ title: "The archive already exists", text: baseName(out) + " already exists. Add the files to it, or replace it with a new archive?", choices: [{ label: "Replace", value: "replace" }, { label: "Add to it", value: "add", primary: true }] });
        if (!mode) return;
      }
      if (st.saveDefault) { setPref("defaultFormat", st.format); setPref("defaultLevel", st.level); }
      setPref("testAfter", st.testAfter);
      setPref("exclude", st.exclude);
      d.close();
      const params = { kind: "create", output: out, inputs: st.inputs, format: st.format, level: st.level, newPassword: st.password, encryptNames: st.encryptNames, volume: volume || 0, sfx: st.sfx, recovery: st.recovery, lock: st.lock, comment: st.comment, exclude: st.exclude.split(/[;,]/).map((x) => x.trim()).filter(Boolean), deleteAfter: st.deleteAfter, testAfter: st.testAfter, mode };
      try {
        const res = await runTask(params, { title: "Creating " + baseName(out), foreground: opts.standalone });
        if (S.loc && S.loc.kind === "fs" && !S.standalone) await reload().catch(() => {});
        if (!S.standalone) {
          const n = S.view.find((i) => i.path === res.path);
          if (n) { S.sel = new Set([n.key]); S.cursor = S.view.indexOf(n); scrollToCursor(); drawRows(true); refreshSelection(); }
          toast("Created " + baseName(res.path) + " · " + fmtSize(res.size) + (res.tested ? " · tested OK" : ""), { kind: "ok", actions: [{ label: "Open", run: () => openPath(res.path, st.volume ? "auto" : "archive") }] });
        }
        resolve(res);
      } catch (e) {
        if (!isCancel(e)) await errorDialog("Could not create the archive", e.message);
        resolve(null);
      }
    };
    d.buttons([{ label: "Cancel", run: () => { d.close(); resolve(null); } }, { label: "Create", icon: "add", kind: "primary", run: submit }]);
    sync();
  });
}
function parseSize(s) {
  const m = String(s).trim().match(/^([\d.]+)\s*([kmgt]?)b?$/i);
  if (!m) return 0;
  return Math.round(parseFloat(m[1]) * { "": 1, k: 1024, m: 1 << 20, g: 1 << 30, t: 2 ** 40 }[m[2].toLowerCase()]);
}
function pwScore(p) {
  if (!p) return 0;
  let s = 0;
  if (p.length >= 8) s++;
  if (p.length >= 12) s++;
  if (/[a-z]/.test(p) && /[A-Z]/.test(p)) s++;
  if (/\d/.test(p) && /[^\w]/.test(p)) s++;
  else if (/\d|[^\w]/.test(p) && p.length >= 10) s++;
  return Math.min(4, s);
}

// ------------------------------------------------------------------ Extract
function extractDialog(o) {
  const arcs = o.archives;
  const multi = arcs.length > 1;
  const base = dirName(arcs[0]);
  let sub = pref("extractSub", true);
  const destFor = (b) => (multi || !sub ? b : joinPath(b, stem(arcs[0])));
  let baseDest = base, overwrite = pref("overwrite", "overwrite"), onlySel = o.select && o.select.length > 0, openAfter = pref("openAfterExtract", false);
  return new Promise((resolve) => {
    const d = modal({ title: "Extraction path and options", sub: multi ? plural(arcs.length, "archive") : arcs[0], icon: "extract", size: "wide", focus: "primary", onDismiss: () => { d.close(); resolve(null); } });
    const destInp = h("input", { class: "input", value: destFor(baseDest), spellcheck: "false" });
    const setBase = (b) => { baseDest = b; destInp.value = destFor(b); };
    const browse = h("button", { class: "btn", onclick: async () => { const p = await pickerDialog({ title: "Extract to", mode: "folder", start: baseDest }); if (p) setBase(p); } }, ic("folderopen"), "Browse…");
    const quick = h("div", { style: { display: "flex", flexWrap: "wrap", gap: "6px", marginTop: "-4px", marginBottom: "14px" } });
    const chip = (label, path, ico) => h("button", { class: "btn sm", title: path, onclick: () => setBase(path) }, ic(ico), label);
    quick.append(chip("Archive folder", base, "folder"));
    S.init.places.filter((p) => ["desktop", "downloads", "documents"].includes(p.icon)).forEach((p) => quick.append(chip(p.name, p.path, PLACE_ICONS[p.icon])));
    (pref("recentDest", []) || []).slice(0, 3).forEach((p) => p !== base && quick.append(chip(baseName(p) || p, p, "clock")));
    const subChk = checkbox(multi ? "Extract each archive into its own folder" : 'Create a folder named "' + stem(arcs[0]) + '"', sub, (v) => { sub = v; destInp.value = destFor(baseDest); }, multi ? "Keeps the files of different archives apart" : "Keeps the extracted files together");
    const ow = h("div", { class: "box-group" });
    [["overwrite", "Overwrite existing files"], ["skip", "Skip existing files"], ["rename", "Keep both (rename extracted files)"]].forEach(([v, l]) => ow.append(checkbox(l, overwrite === v, () => (overwrite = v), null, { radio: true, name: "ow" })));
    const which = o.select && o.select.length ? h("div", { class: "box-group" },
      checkbox("Selected files (" + o.select.length + ")", true, () => (onlySel = true), null, { radio: true, name: "which" }),
      checkbox("All files", false, () => (onlySel = false), null, { radio: true, name: "which" })) : null;
    d.body.append(field("Destination folder", h("div", { class: "input-row" }, destInp, browse)), quick,
      h("div", { class: "grid2" },
        h("div", null, h("div", { class: "label", style: { marginBottom: "4px" } }, "When files already exist"), ow),
        h("div", null, which ? [h("div", { class: "label", style: { marginBottom: "4px" } }, "Extract"), which] : null,
          h("div", { class: "label", style: { marginBottom: "4px" } }, "Options"),
          h("div", { class: "box-group" }, subChk, checkbox("Open the folder when done", openAfter, (v) => (openAfter = v))))));
    d.buttons([{ label: "Cancel", run: () => { d.close(); resolve(null); } }, { label: "Extract", icon: "extract", kind: "primary", run: async () => {
      let dest = destInp.value.trim();
      if (!dest) { destInp.focus(); return; }
      setPref("extractSub", sub); setPref("overwrite", overwrite); setPref("openAfterExtract", openAfter);
      const rd = [baseDest].concat((pref("recentDest", []) || []).filter((p) => p !== baseDest)).slice(0, 5);
      setPref("recentDest", rd);
      d.close();
      const sel = onlySel ? o.select : [];
      try {
        const res = await doExtract({ archives: arcs, dest: multi ? baseDest : dest, separate: multi && sub, select: sel, baseDir: sel.length ? o.baseDir : "", overwrite, openAfter });
        resolve(res);
      } catch (e) { resolve(null); }
    } }]);
  });
}

// ------------------------------------------------------------------ results
function testResultDialog(arcs, res) {
  return new Promise((resolve) => {
    const ok = !res.errors || !res.errors.length;
    const d = modal({ title: ok ? "No errors found" : "Errors found", sub: arcs.length > 1 ? plural(arcs.length, "archive") : arcs[0], icon: ok ? "checkcircle" : "xcircle", iconClass: ok ? "green" : "red", size: "narrow", onDismiss: () => { d.close(); resolve(); } });
    d.body.append(h("p", null, ok ? "All " + plural(res.files, "file") + " (" + fmtSize(res.bytes) + ") were tested and are intact." : plural(res.errors.length, "problem") + " found while testing " + plural(res.files, "file") + "."));
    if (!ok) d.body.append(h("div", { class: "msg-list" }, res.errors.join("\n")));
    d.buttons([{ label: "OK", kind: "primary", run: () => { d.close(); resolve(); } }]);
  });
}
function repairResultDialog(arc, res) {
  const ok = res.lost === 0;
  const d = modal({ title: res.damaged === 0 ? "The archive is not damaged" : ok ? "Archive repaired" : "Archive partly repaired", sub: arc, icon: ok ? "checkcircle" : "alert", iconClass: ok ? "green" : "red", size: "narrow" });
  d.body.append(h("p", null, res.damaged === 0 ? "The recovery record found no damage." : "Found " + plural(res.damaged, "damaged block") + ", rebuilt " + res.repaired + (res.lost ? ", " + res.lost + " could not be recovered." : ".")));
  if (res.output) d.body.append(h("p", null, "Repaired copy: ", h("b", { style: { userSelect: "text" } }, res.output)));
  d.buttons([res.output ? { label: "Open repaired copy", run: () => { d.close(); openPath(res.output, "archive"); } } : null, { label: "OK", kind: "primary", run: () => d.close() }].filter(Boolean));
}

// ------------------------------------------------------------------ info / properties
async function archiveInfoDialog(path, focusComment) {
  let r;
  try { r = await withPassword(path, (pw) => api("archive/list", { path, password: pw })); }
  catch (e) { if (!isCancel(e)) errorDialog("Cannot read the archive", e.message); return; }
  const info = r.info;
  const d = modal({ title: baseName(path), sub: dirName(path), icon: "info", size: "wide" });
  const ratio = info.unpacked > 0 ? Math.round((info.size / info.unpacked) * 100) : null;
  const props = (pairs) => h("dl", { class: "props" }, pairs.filter((p) => p && p[1] !== "" && p[1] != null).map(([k, v]) => [h("dt", null, k), h("dd", null, v)]));
  d.body.append(h("div", { style: { display: "flex", gap: "18px", alignItems: "center", marginBottom: "16px" } },
    h("div", { class: "ratio", html: ratioRing(info.size, info.unpacked, 17).replace('viewBox="0 0 42 42"', 'viewBox="0 0 42 42" style="width:72px;height:72px"') }),
    h("div", null, h("div", { style: { fontSize: "22px", fontWeight: 700 } }, ratio != null ? ratio + "%" : "—"), h("div", { class: "hint" }, "of the original size"),
      h("div", { style: { display: "flex", gap: "6px", marginTop: "8px", flexWrap: "wrap" } }, ...infoChips(info, r.format)))));
  d.body.append(h("div", { class: "grid2" },
    props([["Format", String(r.format).toUpperCase()], ["Files", info.files.toLocaleString()], ["Folders", info.dirs.toLocaleString()], ["Original size", fmtSize(info.unpacked)], ["Archive size", fmtSize(info.size) + " (" + info.size.toLocaleString() + " bytes)"]]),
    props([["Encryption", info.encrypted ? (r.format === "nest" ? "AES-256-GCM" : "AES-256") + (info.hiddenNames ? ", names hidden" : "") : "None"], ["Recovery record", info.recoveryPercent ? info.recoveryPercent + "%" : r.format === "nest" ? "None" : "Not supported"], ["Locked", info.locked ? "Yes" : "No"], ["Volumes", info.volumes > 1 ? info.volumes : "1"], ["Self-extracting", info.sfx ? "Yes" : "No"], ["Modified", fmtDate(info.mtime)]])));
  let comment = info.comment || "";
  const canComment = info.writable && ["zip", "nest"].includes(r.format);
  const ta = h("textarea", { class: "input", rows: 4, disabled: !canComment, placeholder: canComment ? "No comment. Type one to add it." : "No comment", oninput: (e) => (comment = e.target.value) });
  ta.value = comment;
  d.body.append(h("div", { class: "section-title" }, "Comment"), ta);
  d.buttons([
    { label: "Test", icon: "test", run: async () => { d.close(); try { const res = await withPassword(path, (pw) => runTask({ kind: "test", archive: path, password: pw })); testResultDialog([path], res); } catch (e) { if (!isCancel(e)) errorDialog("Test failed", e.message); } } },
    "grow",
    canComment ? { label: "Save comment", run: async () => { try { await withPassword(path, (pw) => runTask({ kind: "comment", archive: path, password: pw, comment }, { title: "Saving comment" })); toast("Comment saved", { kind: "ok" }); d.close(); if (inArchive() && S.loc.archive === path) reload(); } catch (e) { if (!isCancel(e)) errorDialog("Could not save the comment", e.message); } } } : null,
    { label: "Close", kind: "primary", run: () => d.close() }].filter(Boolean));
  if (focusComment && canComment) setTimeout(() => ta.focus(), 60);
}
function propertiesDialog(it) {
  const d = modal({ title: it.name, sub: it.path, icon: "info", size: "narrow" });
  d.body.append(h("div", { style: { display: "flex", justifyContent: "center", margin: "6px 0 14px" }, html: tile(it, true) }),
    h("dl", { class: "props" }, [["Type", typeName(it)], ["Size", it.size >= 0 ? fmtSize(it.size) + " (" + it.size.toLocaleString() + " bytes)" : ""], ["Modified", fmtDate(it.mtime)], ["Location", dirName(it.path)]].filter((p) => p[1]).map(([k, v]) => [h("dt", null, k), h("dd", null, v)])));
  d.buttons([{ label: "Close", kind: "primary", run: () => d.close() }]);
}
async function previewDialog(it) {
  const d = modal({ title: it.name, sub: baseName(S.loc.archive), icon: "view", size: "wide" });
  const box = h("div", null, h("div", { class: "hint" }, "Loading preview…"));
  d.body.append(box);
  d.buttons([{ label: "Extract…", icon: "extract", run: () => { d.close(); S.sel = new Set([it.key]); CMD.extract.run(); } }, "grow", { label: "Close", kind: "primary", run: () => d.close() }]);
  try {
    const r = await api("preview", { archive: S.loc.archive, password: S.passwords.get(S.loc.archive) || "", entry: it.path });
    box.innerHTML = "";
    const p = previewEl(r);
    $$("pre", p).forEach((x) => (x.style.maxHeight = "60vh"));
    $$("img", p).forEach((x) => (x.style.maxHeight = "60vh"));
    box.append(p);
  } catch (e) { box.textContent = e.message; }
}

// ------------------------------------------------------------------ Convert
function convertDialog(arcs, opts = {}) {
  let format = arcs.some((a) => /\.nest$/i.test(a)) ? "zip" : "nest", level = pref("defaultLevel", 5), password = "", encryptNames = false, recovery = 0, deleteAfter = false;
  let destDir = dirName(arcs[0]);
  return new Promise((resolve) => {
    const d = modal({ title: "Convert archives", sub: arcs.length > 1 ? plural(arcs.length, "archives") : arcs[0], icon: "convert", size: "wide", onDismiss: () => { d.close(); resolve(null); } });
    const cards = h("div", { class: "cards" });
    const drawCards = () => {
      cards.innerHTML = "";
      FORMATS.forEach(([f, label, desc, badge]) => cards.append(h("button", { type: "button", class: "card-opt" + (format === f ? " on" : ""), onclick: () => { format = f; drawCards(); } }, badge ? h("span", { class: "badge" }, badge) : null, h("b", null, label), h("small", null, desc))));
      pw.input.disabled = format.startsWith("tar");
      names.input.disabled = !(format === "nest" || format === "7z");
      rec.input.disabled = format !== "nest";
    };
    const pw = passwordInput("", (v) => (password = v), "Leave empty for no password");
    const names = checkbox("Encrypt file names", false, (v) => (encryptNames = v));
    const rec = checkbox("Add a 3% recovery record", false, (v) => (recovery = v ? 3 : 0));
    const destInp = h("input", { class: "input", value: destDir, spellcheck: "false", oninput: (e) => (destDir = e.target.value) });
    d.body.append(h("p", null, "Repack archives into another format — for example RAR or 7Z into ZIP so anyone can open them, or into NEST for a smaller, repairable archive."),
      field("Convert to", cards),
      h("div", { class: "grid2", style: { marginTop: "12px" } },
        h("div", null,
          field("Compression", select(LEVELS.map(([v, l]) => [v, l]), level, (v) => (level = +v))),
          field("Save to folder", h("div", { class: "input-row" }, destInp, h("button", { class: "btn", onclick: async () => { const p = await pickerDialog({ title: "Save converted archives to", mode: "folder", start: destDir }); if (p) { destDir = p; destInp.value = p; } } }, ic("folderopen"))))),
        h("div", null, field("New password (optional)", pw), h("div", { class: "box-group" }, names, rec, checkbox("Delete the original archives", false, (v) => (deleteAfter = v))))));
    drawCards();
    d.buttons([{ label: "Cancel", run: () => { d.close(); resolve(null); } }, { label: "Convert", icon: "convert", kind: "primary", run: async () => {
      d.close();
      try {
        const res = await withPassword(arcs[0], (pw0) => runTask({ kind: "convert", archives: arcs, password: pw0, output: arcs.length > 1 ? destDir : joinPath(destDir, stem(arcs[0]) + "." + format), format, level, newPassword: password, encryptNames, recovery, deleteAfter }, { title: "Converting", foreground: opts.standalone }));
        if (!S.standalone) {
          if (S.loc.kind === "fs") await reload();
          toast("Converted " + plural(res.outputs.length, "archive"), { kind: "ok", actions: res.outputs.length === 1 ? [{ label: "Open", run: () => openPath(res.outputs[0], "archive") }] : [] });
        }
        resolve(res);
      } catch (e) { if (!isCancel(e)) await errorDialog("Conversion failed", e.message); resolve(null); }
    } }]);
  });
}

// ------------------------------------------------------------------ Find
function findDialog() {
  const inArc = inArchive();
  let pattern = "", text = "", root = inArc ? S.loc.archive : S.loc.dir, inArchives = false, results = [];
  const d = modal({ title: "Find files", sub: inArc ? "In " + baseName(S.loc.archive) : "In " + root, icon: "find", size: "wide" });
  const pat = h("input", { class: "input", placeholder: "e.g. *.jpg; report", spellcheck: "false", oninput: (e) => (pattern = e.target.value), autofocus: true });
  const txt = h("input", { class: "input", placeholder: "Optional", spellcheck: "false", oninput: (e) => (text = e.target.value) });
  const rootInp = h("input", { class: "input", value: root, spellcheck: "false", disabled: inArc, oninput: (e) => (root = e.target.value) });
  const res = h("div", { class: "results hidden" });
  const summary = h("div", { class: "hint", style: { margin: "10px 0 4px" } });
  d.body.append(h("div", { class: "grid2" }, field("File names", pat, "Wildcards * and ?; separate several with ;"), field("Containing text", txt)),
    !inArc ? field("Look in", h("div", { class: "input-row" }, rootInp, h("button", { class: "btn", onclick: async () => { const p = await pickerDialog({ title: "Look in", mode: "folder", start: root }); if (p) { root = p; rootInp.value = p; } } }, ic("folderopen")))) : null,
    !inArc ? checkbox("Also search inside archives", false, (v) => (inArchives = v)) : null,
    summary, res);
  const draw = () => {
    res.innerHTML = "";
    res.classList.toggle("hidden", !results.length);
    results.slice(0, 1000).forEach((r) => res.append(h("div", { class: "p-item", title: "Double-click to show", ondblclick: () => go(r) },
      h("span", { html: tile({ name: r.name, dir: r.dir }), style: { display: "contents" } }),
      h("div", null, h("b", null, r.name), h("small", { class: "hint" }, r.archive ? baseName(r.archive) + " › " + (r.path.includes("/") ? r.path.slice(0, r.path.lastIndexOf("/")).replace(/\//g, " › ") : "") : dirName(r.path))),
      h("small", null, r.dir ? "" : fmtSize(r.size)))));
  };
  const go = (r) => {
    d.close();
    if (r.archive) { const i = r.path.lastIndexOf("/"); navigate({ kind: "archive", archive: r.archive, inner: i < 0 ? "" : r.path.slice(0, i) }, { select: r.name }); }
    else navigate({ kind: "fs", dir: dirName(r.path) }, { select: r.path });
  };
  const run = async () => {
    summary.textContent = "";
    try {
      const params = inArc ? { kind: "find", archive: S.loc.archive, password: S.passwords.get(S.loc.archive) || "", pattern, text } : { kind: "find", root, pattern, text, inArchives };
      const r = await runTask(params, { title: "Searching" });
      results = r.results || [];
      summary.textContent = results.length ? plural(results.length, "match") .replace("matchs", "matches") + (r.truncated ? " (showing the first " + results.length + ")" : "") + " — double-click one to show it" : "Nothing found.";
      draw();
    } catch (e) { if (!isCancel(e)) errorDialog("Search failed", e.message); }
  };
  d.buttons([{ label: "Close", run: () => d.close() }, { label: "Find", icon: "search", kind: "primary", run }]);
}

// ------------------------------------------------------------------ Benchmark
function benchmarkDialog() {
  const d = modal({ title: "Benchmark", sub: S.init.cpus + " CPU threads", icon: "gauge" });
  const out = h("div");
  d.body.append(h("p", null, "Measures how fast this device compresses and decompresses 32 MB of test data with each format. Nothing on your disk is changed."), out);
  const run = async () => {
    d.primary.disabled = true;
    try {
      const res = await runTask({ kind: "benchmark" }, { title: "Running benchmark" });
      out.innerHTML = "";
      const maxC = Math.max(...res.map((r) => r.compress)), maxD = Math.max(...res.map((r) => r.decompress));
      res.forEach((r) => out.append(h("div", { class: "bench-row" },
        h("div", { class: "hd" }, h("span", null, r.method), h("span", { class: "hint" }, "size " + Math.round(r.ratio * 100) + "%")),
        h("div", { class: "bench-bar" }, h("span", null, "Compress"), h("div", { class: "bar" }, h("div", { style: { width: (r.compress / maxC) * 100 + "%" } })), h("span", null, r.compress.toFixed(0) + " MB/s")),
        h("div", { class: "bench-bar" }, h("span", null, "Decompress"), h("div", { class: "bar alt" }, h("div", { style: { width: (r.decompress / maxD) * 100 + "%" } })), h("span", null, r.decompress.toFixed(0) + " MB/s")))));
      d.primary.textContent = "Run again";
    } catch (e) { if (!isCancel(e)) errorDialog("Benchmark failed", e.message); }
    d.primary.disabled = false;
  };
  d.buttons([{ label: "Close", run: () => d.close() }, { label: "Start", icon: "play", kind: "primary", run }]);
}

// ------------------------------------------------------------------ Wizard
function wizardDialog() {
  let choice = "extract";
  const d = modal({ title: "ZipNest Wizard", sub: "What would you like to do?", icon: "wizard" });
  const opts = [
    ["extract", "extract", "Unpack an archive", "Extract files from a ZIP, RAR, 7Z, NEST or other archive"],
    ["create", "add", "Create a new archive", "Compress files and folders into one archive"],
    ["add", "layers", "Add files to an existing archive", "Put more files into an archive you already have"],
  ];
  const list = h("div");
  const draw = () => {
    list.innerHTML = "";
    opts.forEach(([v, ico, t, s]) => list.append(h("div", { class: "big-opt" + (choice === v ? " on" : ""), onclick: () => { choice = v; draw(); }, ondblclick: next },
      h("div", { class: "m-ic", html: icon(ico) }), h("div", null, h("b", null, t), h("small", null, s)))));
  };
  async function next() {
    d.close();
    const startDir = S.loc.kind === "fs" ? S.loc.dir : S.init.home;
    if (choice === "extract") {
      const a = await pickerDialog({ title: "Choose the archive to unpack", mode: "file", start: startDir, ok: "Next" });
      if (a) extractDialog({ archives: [a], select: [], baseDir: "" });
    } else if (choice === "create") {
      const f = await pickerDialog({ title: "Choose files and folders to compress", mode: "files", start: startDir, ok: "Next" });
      if (f && f.length) addDialog(f);
    } else {
      const a = await pickerDialog({ title: "Choose the archive", mode: "file", start: startDir, ok: "Next" });
      if (!a) return;
      const f = await pickerDialog({ title: "Choose files to add to " + baseName(a), mode: "files", start: dirName(a), ok: "Add" });
      if (!f || !f.length) return;
      try {
        await withPassword(a, (pw) => runTask({ kind: "add", archive: a, password: pw, inputs: f }, { title: "Adding to " + baseName(a) }));
        toast("Added " + plural(f.length, "item") + " to " + baseName(a), { kind: "ok", actions: [{ label: "Open", run: () => openPath(a, "archive") }] });
      } catch (e) { if (!isCancel(e)) errorDialog("Could not add the files", e.message); }
    }
  }
  draw();
  d.body.append(list);
  d.buttons([{ label: "Cancel", run: () => d.close() }, { label: "Next", icon: "chevronright", kind: "primary", run: next }]);
}

// ------------------------------------------------------------------ Settings
function settingsDialog(tab) {
  const d = modal({ title: "Settings", icon: "settings", size: "wide", tabs: true });
  const pages = { general: h("div"), integration: h("div", { class: "off" }), about: h("div", { class: "off" }) };
  const tabs = [["general", "General"]];
  if (S.init.integration) tabs.push(["integration", S.init.platform === "windows" ? "Windows integration" : "Integration"]);
  tabs.push(["about", "About"]);
  d.setTabs(tabs, (id) => { for (const k in pages) pages[k].classList.toggle("off", k !== id); if (id === "integration") loadIntegration(); });
  d.body.append(h("div", { class: "tabpages" }, ...Object.values(pages)));
  // general
  const g = pages.general;
  g.append(field("Appearance", segmented([["system", "System", "monitor"], ["light", "Light", "sun"], ["dark", "Dark", "moon"]], pref("theme", "system"), (v) => { setPref("theme", v); applyTheme(); renderThemeIcon(); })));
  g.append(h("div", { class: "grid2" },
    h("div", null,
      field("Default archive format", select(FORMATS.map(([f, l]) => [f, l]), pref("defaultFormat", "nest"), (v) => setPref("defaultFormat", v))),
      field("Default compression", select(LEVELS.map(([v, l]) => [v, l]), pref("defaultLevel", 5), (v) => setPref("defaultLevel", +v)))),
    h("div", null,
      toggle("Show hidden files", pref("showHidden", false), (v) => { setPref("showHidden", v); if (S.loc && S.loc.kind === "fs") reload(); }),
      toggle("Show details pane", pref("details", true), (v) => { setPref("details", v); renderDetails(); updateCommands(); }),
      toggle("Ask before deleting", pref("confirmDelete", true), (v) => setPref("confirmDelete", v)),
      toggle("Open the folder after extracting", pref("openAfterExtract", false), (v) => setPref("openAfterExtract", v)))));
  g.append(h("div", { class: "section-title" }, "Privacy"),
    h("div", { style: { display: "flex", gap: "8px", flexWrap: "wrap" } },
      h("button", { class: "btn", onclick: () => { setPref("recent", []); setPref("recentDest", []); renderNav(); toast("History cleared", { kind: "ok" }); } }, ic("clock"), "Clear history"),
      h("button", { class: "btn", onclick: () => { S.passwords.clear(); toast("Remembered passwords forgotten", { kind: "ok" }); } }, ic("key"), "Forget passwords")));
  // integration
  let integ = null;
  const ip = pages.integration;
  async function loadIntegration() {
    if (integ) return;
    ip.innerHTML = '<div class="hint">Loading…</div>';
    try { integ = await api("integration/get"); } catch (e) { ip.textContent = e.message; return; }
    ip.innerHTML = "";
    ip.append(integrationForm(integ));
    ip.append(h("div", { style: { display: "flex", justifyContent: "flex-end", marginTop: "12px" } }, h("button", { class: "btn primary", onclick: async () => {
      try { await busy("Applying", api("integration/set", integ)); toast("Windows integration updated", { kind: "ok" }); }
      catch (e) { errorDialog("Some settings could not be applied", e.message); }
    } }, ic("check"), "Apply")));
  }
  pages.about.append(aboutContent());
  d.buttons([{ label: "Done", kind: "primary", run: () => d.close() }]);
  if (tab) $('[data-tab="' + tab + '"]', d.tabsEl).click();
}

function integrationForm(integ) {
  const wrap = h("div");
  const grid = h("div", { class: "assoc-grid" });
  const boxes = integ.types.map((t) => { const c = checkbox(t.label, t.on, (v) => (t.on = v), null, { title: t.exts.join(" ") }); grid.append(c); return c; });
  const all = h("button", { class: "btn sm", onclick: () => { const on = !integ.types.every((t) => t.on); integ.types.forEach((t, i) => { t.on = on; boxes[i].input.checked = on; }); } }, "Toggle all");
  const shellItems = h("div");
  const casc = toggle("Group in a ZipNest submenu", integ.cascaded, (v) => (integ.cascaded = v), "Keeps the right-click menu tidy");
  const icons = toggle("Show icons in the menu", integ.icons, (v) => (integ.icons = v));
  const w11 = toggle("Windows 11 top-level menu", integ.win11 && integ.win11Available, (v) => (integ.win11 = v), integ.win11Available ? "ZipNest appears without “Show more options”" : "Available on Windows 11", !integ.win11Available);
  const sync = () => { [casc, icons, w11].forEach((t) => (t.input.disabled = !integ.shell || (t === w11 && !integ.win11Available))); };
  const shell = toggle("Add ZipNest to the right-click menu", integ.shell, (v) => { integ.shell = v; sync(); }, "Extract Here, Extract to folder, Add to archive and more");
  shellItems.append(shell, casc, icons, w11);
  sync();
  wrap.append(h("div", { class: "grid2" },
    h("div", null, h("div", { class: "label", style: { display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: "6px" } }, "Open these file types with ZipNest", all), h("div", { class: "box-group" }, grid)),
    h("div", null, h("div", { class: "label", style: { marginBottom: "6px" } }, "Explorer menu"), h("div", { class: "box-group" }, shellItems))));
  return wrap;
}

function aboutContent() {
  const fm = [["NEST", 1, 1], ["ZIP / ZIPX", 1, 1], ["7Z", 1, 1], ["RAR (v4, v5)", 1, 0], ["TAR, GZ, BZ2, XZ, ZST", 1, 1], ["Split .001 volumes", 1, 1], ["Self-extracting .exe", 1, S.init.sfx ? 1 : 0]];
  return h("div", { class: "about" }, h("img", { src: "/assets/logo.png", alt: "" }), h("h2", null, "ZipNest"), h("p", null, "Version " + S.init.version + " · " + { windows: "Windows", android: "Android", linux: "Linux", darwin: "macOS" }[S.init.platform]),
    h("p", { class: "hint" }, "A fast, modern archive manager. Free and open source."),
    h("table", { class: "fmt-table" }, h("tr", null, h("th", null, "Format"), h("th", null, "Open & extract"), h("th", null, "Create")),
      fm.map(([n, r, w]) => h("tr", null, h("td", null, n), h("td", { class: r ? "yes" : "no" }, r ? "Yes" : "—"), h("td", { class: w ? "yes" : "no" }, w ? "Yes" : "—")))),
    h("p", { class: "hint", style: { marginTop: "12px" } }, "RAR is a trademark of RARLAB. ZipNest reads RAR archives with an independent decoder."));
}
function aboutDialog() {
  const d = modal({ title: "About ZipNest", icon: "help" });
  d.body.append(aboutContent());
  d.buttons([{ label: "Close", kind: "primary", run: () => d.close() }]);
}

// ------------------------------------------------------------------ installer setup page
async function setupPage() {
  S.setup = true;
  let integ;
  try { integ = await api("integration/get"); } catch (e) { integ = null; }
  let desktop = true, startMenu = true;
  const root = $("#root");
  root.innerHTML = "";
  const body = h("div", { class: "setup-body" });
  root.append(h("div", { class: "setup" },
    h("div", { class: "setup-head" }, h("img", { src: "/assets/logo.png", alt: "" }), h("div", null, h("h1", null, "Set up ZipNest"), h("p", null, "Choose how ZipNest works with Windows. You can change this later in Settings."))),
    body,
    h("div", { class: "setup-foot" }, h("button", { class: "btn primary", style: { minWidth: "120px" }, onclick: finish }, "OK"))));
  if (integ) {
    const f = integrationForm(integ);
    const [left, right] = f.firstChild.children;
    body.append(h("div", { class: "setup-card" }, left), h("div", { class: "setup-card" }, right));
  }
  body.append(h("div", { class: "setup-card", style: { gridColumn: "1 / -1" } }, h("h3", null, "Shortcuts"),
    h("div", { style: { display: "flex", gap: "28px", flexWrap: "wrap" } }, checkbox("Add ZipNest to the Desktop", true, (v) => (desktop = v)), checkbox("Add ZipNest to the Start menu", true, (v) => (startMenu = v)))));
  async function finish() {
    const btn = $(".setup-foot .btn");
    btn.disabled = true;
    btn.textContent = "Applying…";
    if (integ) { try { await api("integration/set", integ); } catch (e) { await errorDialog("Some settings could not be applied", e.message); } }
    await api("exit", { code: (desktop ? 1 : 0) | (startMenu ? 2 : 0) });
  }
}

// ------------------------------------------------------------------ Android storage permission
function storageDialog() {
  const d = modal({ title: "Allow access to your files", icon: "folderopen", size: "narrow", closable: false });
  d.body.append(h("p", null, "ZipNest needs permission to read and write files so it can open archives and extract them to your storage."));
  d.buttons([{ label: "Not now", run: () => d.close() }, { label: "Allow", kind: "primary", run: () => { d.close(); android.requestStorage(); } }]);
}
window.znStorageGranted = () => reload();
