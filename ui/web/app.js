"use strict";
/* ZipNest — main window: navigation, file list, commands. Dialogs live in dialogs.js. */

// ------------------------------------------------------------------ helpers
const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => Array.from(el.querySelectorAll(s));

function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  if (attrs) {
    for (const k in attrs) {
      const v = attrs[k];
      if (v == null || v === false) continue;
      if (k === "class") el.className = v;
      else if (k === "html") el.innerHTML = v;
      else if (k === "text") el.textContent = v;
      else if (k === "style" && typeof v === "object") Object.assign(el.style, v);
      else if (k.startsWith("on")) el.addEventListener(k.slice(2).toLowerCase(), v);
      else if (k === "value") el.value = v;
      else if (k === "checked") el.checked = !!v;
      else el.setAttribute(k, v === true ? "" : v);
    }
  }
  for (const c of kids.flat(Infinity)) {
    if (c == null || c === false) continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return el;
}
const ic = (name, cls) => h("span", { html: icon(name, cls), style: { display: "contents" } });
const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));

function fmtSize(n) {
  if (n == null || n < 0) return "";
  if (n < 1024) return n + " B";
  const u = ["KB", "MB", "GB", "TB"];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
  return (n >= 100 ? n.toFixed(0) : n >= 10 ? n.toFixed(1) : n.toFixed(2)) + " " + u[i];
}
function fmtDate(ms) {
  if (!ms) return "";
  const d = new Date(ms), now = new Date();
  const time = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  if (d.toDateString() === now.toDateString()) return "Today, " + time;
  const y = new Date(now); y.setDate(now.getDate() - 1);
  if (d.toDateString() === y.toDateString()) return "Yesterday, " + time;
  return d.toLocaleDateString([], { year: "numeric", month: "short", day: "numeric" }) + ", " + time;
}
function fmtDuration(ms) {
  if (ms == null || !isFinite(ms)) return "—";
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return s + "s";
  const m = Math.floor(s / 60);
  if (m < 60) return m + "m " + String(s % 60).padStart(2, "0") + "s";
  return Math.floor(m / 60) + "h " + String(m % 60).padStart(2, "0") + "m";
}
const plural = (n, w) => n + " " + w + (n === 1 ? "" : "s");
const debounce = (fn, ms) => { let t; return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); }; };

class ApiError extends Error {
  constructor(msg, code) { super(msg); this.code = code || "error"; }
}
async function api(name, body) {
  let r;
  try {
    r = await fetch("/api/" + name, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body || {}) });
  } catch (e) {
    throw new ApiError("ZipNest stopped responding. Please restart the app.", "offline");
  }
  let j = null;
  try { j = await r.json(); } catch (e) {}
  if (!r.ok) throw new ApiError((j && j.error) || "Request failed (" + r.status + ")", j && j.code);
  return j;
}
const isCancel = (e) => e && (e.code === "cancelled" || e.code === "dismissed");

// ------------------------------------------------------------------ file types
const EXT_INFO = (() => {
  const m = {};
  const add = (exts, color, type, ico) => exts.split(" ").forEach((e) => (m[e] = { color, type, ico }));
  add("zip zipx 7z rar nest tar gz tgz bz2 tbz2 xz txz zst tzst cab iso jar apk cbz cbr cb7 001", "", "archive");
  add("png jpg jpeg gif webp bmp tif tiff svg ico heic avif raw psd", "#16a34a", "image");
  add("mp4 mkv avi mov wmv webm flv m4v mpg mpeg 3gp", "#e11d48", "video");
  add("mp3 wav flac aac ogg m4a wma opus mid", "#db2777", "audio");
  add("doc docx odt rtf", "#2563eb", "document");
  add("xls xlsx ods csv", "#15803d", "spreadsheet");
  add("ppt pptx odp key", "#ea580c", "presentation");
  add("pdf", "#dc2626", "PDF document");
  add("txt md log ini cfg conf nfo", "#64748b", "text");
  add("js ts jsx tsx go py java c h cpp cs rs rb php html htm css json xml yml yaml toml sh bat ps1 kt swift sql", "#475569", "source code");
  add("exe msi dll sys com scr", "#0f766e", "application");
  add("ttf otf woff woff2", "#7c3aed", "font");
  return m;
})();
const TYPE_NAMES = { image: "image", video: "video", audio: "audio", document: "document", spreadsheet: "spreadsheet", presentation: "presentation", text: "text document", "source code": "source file", application: "application", font: "font", archive: "archive" };
function extOf(name) {
  const n = name.toLowerCase();
  for (const d of [".tar.gz", ".tar.bz2", ".tar.xz", ".tar.zst"]) if (n.endsWith(d)) return d.slice(1);
  const i = n.lastIndexOf(".");
  return i > 0 ? n.slice(i + 1) : "";
}
function typeName(it) {
  if (it.kind === "drive") return "Drive";
  if (it.dir) return "Folder";
  const e = extOf(it.name);
  if (!e) return "File";
  const info = EXT_INFO[e.split(".").pop()] || EXT_INFO[e];
  if (info && info.type === "archive") return e.toUpperCase() + " archive";
  if (info) return e.toUpperCase() + " " + (TYPE_NAMES[info.type] || info.type);
  return e.toUpperCase() + " file";
}
function tile(it, lg) {
  const cls = "ft" + (lg ? " lg" : "");
  if (it.kind === "drive") return `<span class="${cls} drive">${icon("hdd")}</span>`;
  if (it.dir) return `<span class="${cls} folder">${icon("folder")}</span>`;
  const e = extOf(it.name), last = e.split(".").pop();
  const info = EXT_INFO[last];
  if (info && info.type === "archive") return `<span class="${cls} arc">${esc(last.slice(0, 4))}</span>`;
  const color = info ? info.color : "#8b8b9c";
  const label = last ? esc(last.slice(0, 4)) : icon("file");
  return `<span class="${cls}" style="--c:${color}">${label}</span>`;
}
const isArchiveName = (n) => { const e = extOf(n).split(".").pop(); return !!(EXT_INFO[e] && EXT_INFO[e].type === "archive") || /\.(7z|zip|rar|nest)\.0*1$/i.test(n); };

// ------------------------------------------------------------------ state
const S = {
  init: null,
  prefs: {},
  loc: null,           // {kind:"fs"|"pc"|"archive", dir, archive, inner}
  items: [],
  view: [],
  sel: new Set(),
  cursor: -1,
  anchor: -1,
  sort: { key: "name", dir: 1 },
  filter: "",
  back: [],
  fwd: [],
  arc: null,           // {path, info, format, tree, readOnly, parent}
  passwords: new Map(),
  loading: false,
  selmode: false,
};
const IS_MOBILE = () => window.matchMedia("(max-width: 760px)").matches;
const android = window.ZipNestAndroid || null;
const sep = () => (S.init ? S.init.sep : "/");
const joinPath = (dir, name) => (dir.endsWith(sep()) ? dir + name : dir + sep() + name);
function dirName(p) {
  const s = sep();
  const i = p.lastIndexOf(s);
  if (i < 0) return p;
  if (i === 0) return s;
  if (s === "\\" && i === 2 && p[1] === ":") return p.slice(0, 3);
  return p.slice(0, i);
}
const baseName = (p) => { const s = p.replace(/[\\/]+$/, ""); const i = Math.max(s.lastIndexOf("/"), s.lastIndexOf("\\")); return i >= 0 ? s.slice(i + 1) : s; };
function stem(p) {
  let b = baseName(p);
  b = b.replace(/\.(part0*1\.rar|7z\.0*1|zip\.0*1|nest\.0*1|0*1)$/i, "");
  const e = extOf(b);
  return e ? b.slice(0, -(e.length + 1)) : b;
}

function pref(k, d) { return S.prefs[k] === undefined ? d : S.prefs[k]; }
function setPref(k, v) {
  S.prefs[k] = v;
  api("prefs/set", { [k]: v }).catch(() => {});
}

// ------------------------------------------------------------------ theme
function applyTheme() {
  const t = pref("theme", "system");
  try { localStorage.setItem("zn-theme", t); } catch (e) {}
  const dark = t === "dark" || (t === "system" && matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.setAttribute("data-theme", dark ? "dark" : "light");
  if (window.ZipNestAndroid && ZipNestAndroid.setTheme) ZipNestAndroid.setTheme(dark);
}
matchMedia("(prefers-color-scheme: dark)").addEventListener("change", applyTheme);

// ------------------------------------------------------------------ shell (layout)
function renderShell() {
  const root = $("#root");
  root.innerHTML = "";
  root.append(
    h("div", { id: "app" },
      h("aside", { id: "sidebar" },
        h("div", { class: "brand" }, h("img", { src: "/assets/logo.png", alt: "" }),
          h("div", null, h("b", null, "ZipNest"), h("small", null, "Archive manager"))),
        h("button", { class: "new-btn", onclick: () => { closeDrawer(); CMD.add.run(); } }, ic("plus"), "New archive"),
        h("nav", { class: "nav", id: "nav" }),
        h("div", { class: "side-foot" },
          h("div", { class: "nav-item", onclick: () => { closeDrawer(); settingsDialog(); } }, ic("settings"), h("span", null, "Settings")),
          h("div", { class: "nav-item", style: { flex: "none" }, title: "Theme", onclick: cycleTheme }, h("span", { id: "theme-ic", style: { display: "flex", flex: "none" } })))),
      h("div", { class: "scrim", onclick: closeDrawer }),
      h("main", { id: "main" },
        // mobile header
        h("div", { id: "mtop" },
          h("button", { class: "icon-btn", onclick: () => document.body.classList.add("drawer"), "aria-label": "Menu" }, ic("menu")),
          h("button", { class: "icon-btn", id: "m-up", onclick: goUp, "aria-label": "Up" }, ic("back")),
          h("div", { class: "ttl" }, h("b", { id: "m-title" }), h("small", { id: "m-sub" })),
          h("button", { class: "icon-btn", onclick: (e) => showMenu(e.currentTarget, moreMenu()), "aria-label": "More" }, ic("more"))),
        h("div", { id: "msearch" }, searchBox("m")),
        h("div", { id: "mcrumbs" }),
        h("div", { id: "toolbar" }),
        h("div", { id: "addressbar" },
          h("button", { class: "icon-btn", id: "b-back", title: "Back (Alt+Left)", onclick: goBack }, ic("back")),
          h("button", { class: "icon-btn", id: "b-fwd", title: "Forward (Alt+Right)", onclick: goForward }, ic("forward")),
          h("button", { class: "icon-btn", id: "b-up", title: "Up one level (Backspace)", onclick: goUp }, ic("up")),
          h("button", { class: "icon-btn", title: "Refresh (F5)", onclick: () => reload() }, ic("refresh")),
          h("div", { id: "crumbs", onclick: (e) => { if (e.target.id === "crumbs" || e.target.classList.contains("crumb-list")) editAddress(); } }),
          h("button", { class: "icon-btn", id: "b-fav", title: "Add to favorites", onclick: toggleFavorite }, ic("star")),
          searchBox("d"),
          h("button", { class: "icon-btn", id: "b-details", title: "Details pane", onclick: toggleDetails }, ic("panel"))),
        h("div", { id: "banner", class: "hidden" }),
        h("div", { id: "content" },
          h("section", { id: "listwrap" },
            h("div", { class: "cols", id: "cols" }),
            h("div", { id: "list", tabindex: "0" }, h("div", { class: "spacer", id: "spacer" })),
            h("div", { id: "empty", class: "empty hidden" }),
            h("div", { id: "loadbar", class: "loading-bar hidden" })),
          h("aside", { id: "details" })),
        h("div", { id: "statusbar" }),
        h("div", { id: "mbar" }),
        h("div", { id: "mselbar" }))));
  renderToolbar();
  renderThemeIcon();
  const list = $("#list");
  list.addEventListener("scroll", () => drawRows());
  list.addEventListener("mousedown", onListMouseDown);
  list.addEventListener("dblclick", onListDblClick);
  list.addEventListener("contextmenu", onListContext);
  list.addEventListener("keydown", onListKey);
  list.addEventListener("touchstart", onTouchStart, { passive: true });
  list.addEventListener("touchend", onTouchEnd);
  list.addEventListener("touchmove", () => clearTimeout(touch.timer), { passive: true });
  list.addEventListener("click", onListClick);
  let lastCols = "";
  new ResizeObserver(() => {
    const sig = S.loc ? colsFor().map((c) => c[0]).join() : "";
    if (sig !== lastCols) { lastCols = sig; if (S.loc) { renderCols(); drawRows(true); } }
    else drawRows();
  }).observe($("#listwrap"));
  document.addEventListener("keydown", (e) => { if (/^(Arrow|Home|End|Page|Tab)/.test(e.key)) document.body.classList.add("kbd"); }, true);
  document.addEventListener("mousedown", () => document.body.classList.remove("kbd"), true);
  setupDrop();
}

function searchBox(which) {
  const inp = h("input", { type: "search", placeholder: "Filter this folder", id: "filter-" + which, spellcheck: "false",
    oninput: (e) => { S.filter = e.target.value; syncFilters(e.target); applyView(); },
    onkeydown: (e) => { if (e.key === "Escape") { e.target.value = ""; S.filter = ""; syncFilters(e.target); applyView(); $("#list").focus(); } if (e.key === "ArrowDown") { e.preventDefault(); $("#list").focus(); } } });
  return h("label", { class: "search" }, ic("search"), inp);
}
function syncFilters(src) { $$(".search input").forEach((i) => { if (i !== src) i.value = S.filter; }); }

function closeDrawer() { document.body.classList.remove("drawer"); }
function cycleTheme() {
  const order = ["system", "light", "dark"];
  setPref("theme", order[(order.indexOf(pref("theme", "system")) + 1) % 3]);
  applyTheme();
  renderThemeIcon();
  toast("Theme: " + { system: "follow system", light: "light", dark: "dark" }[pref("theme")]);
}
function renderThemeIcon() {
  const t = pref("theme", "system");
  const el = $("#theme-ic");
  if (el) el.innerHTML = icon(t === "dark" ? "moon" : t === "light" ? "sun" : "monitor");
}

// ------------------------------------------------------------------ commands
const inArchive = () => S.loc && S.loc.kind === "archive";
const selItems = () => S.view.filter((it) => S.sel.has(it.key));
const selArchives = () => selItems().filter((it) => !it.dir && it.archive);
const writable = () => inArchive() && S.arc && S.arc.info && S.arc.info.writable && !S.arc.readOnly;

const CMD = {
  add: { label: "Add", icon: "add", key: "Alt+A", tip: "Add files to an archive", run: () => cmdAdd() },
  extract: { label: "Extract To", icon: "extract", key: "Alt+E", tip: "Extract files to a folder", run: () => cmdExtract(), on: () => inArchive() || selArchives().length > 0 },
  test: { label: "Test", icon: "test", key: "Alt+T", tip: "Test archive integrity", run: () => cmdTest(), on: () => inArchive() || selArchives().length > 0 },
  view: { label: "View", icon: "view", key: "Alt+V", tip: "Open the selected file", run: () => cmdView(), on: () => selItems().length === 1 },
  delete: { label: "Delete", icon: "trash", key: "Del", tip: "Delete selected files", run: () => cmdDelete(), on: () => selItems().length > 0 && S.loc.kind !== "pc" && (!inArchive() || writable()) },
  find: { label: "Find", icon: "find", key: "F3", tip: "Find files", run: () => findDialog(), on: () => S.loc.kind !== "pc" },
  wizard: { label: "Wizard", icon: "wizard", key: "Alt+W", tip: "Step-by-step help", run: () => wizardDialog() },
  info: { label: "Info", icon: "info", key: "Alt+I", tip: "Archive information", run: () => cmdInfo(), on: () => inArchive() || selItems().length === 1 },
  repair: { label: "Repair", icon: "repair", key: "Alt+R", tip: "Repair a damaged .nest archive", run: () => cmdRepair(), on: () => (inArchive() && S.arc.format === "nest") || selArchives().some((a) => /\.nest$/i.test(a.name)) },
  convert: { label: "Convert", icon: "convert", key: "Alt+Q", tip: "Convert archives to another format", run: () => cmdConvert(), on: () => inArchive() || selArchives().length > 0 },
  benchmark: { label: "Benchmark", icon: "gauge", tip: "Measure compression speed", run: () => benchmarkDialog() },
  extractHere: { label: "Extract Here", icon: "extracthere", run: () => cmdQuickExtract(false), on: () => (inArchive() && !S.arc.readOnly) || selArchives().length > 0 },
  extractSub: { label: "Extract to subfolder", icon: "folderopen", run: () => cmdQuickExtract(true), on: () => (inArchive() && !S.arc.readOnly) || selArchives().length > 0 },
  rename: { label: "Rename", icon: "pencil", key: "F2", run: () => cmdRename(), on: () => selItems().length === 1 && S.loc.kind !== "pc" && (!inArchive() || writable()) },
  newFolder: { label: "New folder", icon: "folderplus", key: "Ctrl+Shift+N", run: () => cmdNewFolder(), on: () => S.loc.kind === "fs" },
  comment: { label: "Archive comment", icon: "comment", run: () => cmdInfo(true), on: () => inArchive() && ["zip", "nest"].includes(S.arc.format) },
  selectAll: { label: "Select all", icon: "selectall", key: "Ctrl+A", run: () => selectAll() },
  copyPath: { label: "Copy path", icon: "copy", run: () => cmdCopyPath(), on: () => selItems().length > 0 || inArchive() },
  reveal: { label: "Show in folder", icon: "external", run: () => cmdReveal(), on: () => S.init.platform === "windows" },
  refresh: { label: "Refresh", icon: "refresh", key: "F5", run: () => reload() },
  settings: { label: "Settings", icon: "settings", run: () => settingsDialog() },
  about: { label: "About ZipNest", icon: "help", run: () => aboutDialog() },
  share: { label: "Share", icon: "share", run: () => cmdShare(), on: () => !!android && selItems().length > 0 && !selItems().some((i) => i.dir) },
};
const enabled = (c) => !c.on || c.on();

function renderToolbar() {
  const tb = $("#toolbar");
  const btn = (id) => {
    const c = CMD[id];
    return h("button", { class: "tb", "data-cmd": id, title: c.tip ? c.tip + (c.key ? " (" + c.key + ")" : "") : c.label, onclick: () => enabled(c) && c.run() },
      h("span", { class: "ic", html: icon(c.icon) }), c.label);
  };
  tb.innerHTML = "";
  tb.append(btn("add"), btn("extract"), btn("test"), btn("view"), btn("delete"), h("div", { class: "tb-sep" }),
    btn("find"), btn("wizard"), btn("info"), btn("repair"), btn("convert"), h("div", { class: "tb-sep" }),
    h("button", { class: "tb", title: "More tools", onclick: (e) => showMenu(e.currentTarget, moreMenu(), "below") }, h("span", { class: "ic", html: icon("morehoriz") }), "More"));

  const mb = (id, label) => {
    const c = CMD[id];
    return h("button", { class: "mb", "data-cmd": id, onclick: () => enabled(c) && c.run() }, h("span", { html: icon(c.icon), style: { display: "contents" } }), label || c.label);
  };
  const bar = $("#mbar");
  bar.innerHTML = "";
  bar.append(mb("add", "Add"), mb("extract", "Extract"), mb("test"), mb("find"), h("button", { class: "mb", onclick: (e) => showMenu(e.currentTarget, moreMenu()) }, h("span", { html: icon("morehoriz"), style: { display: "contents" } }), "More"));
  const sb = $("#mselbar");
  sb.innerHTML = "";
  sb.append(
    h("button", { class: "mb", onclick: () => { setSelMode(false); } }, h("span", { html: icon("x"), style: { display: "contents" } }), "Cancel"),
    mb("extract", "Extract"), mb("add", "Compress"), mb("delete"),
    h("button", { class: "mb", onclick: (e) => showMenu(e.currentTarget, itemMenu()) }, h("span", { html: icon("morehoriz"), style: { display: "contents" } }), "More"));
}

function updateCommands() {
  $$("[data-cmd]").forEach((b) => { const c = CMD[b.dataset.cmd]; b.disabled = !enabled(c); });
  const addBtn = $('#toolbar [data-cmd="add"]');
  if (addBtn) addBtn.title = inArchive() ? "Add files to this archive (Alt+A)" : "Add files to an archive (Alt+A)";
  $("#b-back").disabled = !S.back.length;
  $("#b-fwd").disabled = !S.fwd.length;
  $("#b-up").disabled = !canGoUp();
  $("#m-up").style.visibility = canGoUp() ? "visible" : "hidden";
  const fav = isFavorite();
  const fb = $("#b-fav");
  fb.classList.toggle("on", fav);
  fb.title = fav ? "Remove from favorites" : "Add to favorites";
  fb.innerHTML = fav ? icon("star").replace("<path", '<path fill="currentColor"') : icon("star");
  fb.style.visibility = S.loc.kind === "pc" ? "hidden" : "visible";
  $("#b-details").classList.toggle("on", pref("details", true));
}

function moreMenu() {
  const m = [];
  if (IS_MOBILE()) {
    m.push(CMD.view, CMD.info, CMD.delete, CMD.rename, "-");
    if (inArchive()) m.push(CMD.extractHere);
  }
  m.push(CMD.extractHere, CMD.extractSub, CMD.convert, CMD.repair, CMD.comment, "-", CMD.benchmark, CMD.wizard, CMD.find, "-",
    CMD.newFolder, CMD.selectAll, CMD.refresh, "-", CMD.settings, CMD.about);
  // drop duplicates
  return m.filter((x, i) => x === "-" || m.indexOf(x) === i);
}

function itemMenu() {
  const items = selItems();
  const one = items.length === 1 ? items[0] : null;
  const m = [];
  if (one) {
    if (one.dir || one.kind === "drive") m.push({ label: "Open", icon: "folderopen", run: () => openItem(one), bold: true });
    else if (!inArchive() && one.archive) m.push({ label: "Open in ZipNest", icon: "archive", run: () => openItem(one), bold: true }, { label: "Open with system", icon: "external", run: () => cmdView(true) });
    else m.push({ label: "Open", icon: "view", run: () => cmdView(), bold: true });
  }
  if (inArchive()) {
    m.push(CMD.extract, CMD.extractHere, CMD.extractSub, "-");
  } else if (selArchives().length) {
    const a = selArchives();
    m.push(CMD.extract, { label: "Extract Here", icon: "extracthere", run: () => cmdQuickExtract(false) },
      { label: a.length === 1 ? 'Extract to "' + stem(a[0].name) + sep() + '"' : "Extract each to separate folder", icon: "folderopen", run: () => cmdQuickExtract(true) },
      CMD.test, CMD.convert, "-");
  }
  if (!inArchive() && S.loc.kind === "fs" && items.length) {
    const name = items.length === 1 ? stem(items[0].name) : baseName(S.loc.dir) || "archive";
    m.push({ label: "Add to archive…", icon: "add", run: () => cmdAdd() },
      { label: 'Add to "' + name + '.zip"', icon: "zap", run: () => quickAdd("zip") },
      { label: 'Add to "' + name + '.nest"', icon: "zap", run: () => quickAdd("nest") }, "-");
  }
  m.push(CMD.share, CMD.rename, CMD.copyPath);
  if (!inArchive() && S.init.platform === "windows" && one) m.push(CMD.reveal);
  if (one && !inArchive() && (one.dir || one.archive)) m.push({ label: isFavorite(one.path) ? "Remove from favorites" : "Add to favorites", icon: "star", run: () => toggleFavorite(one.path, one.dir ? "fs" : "archive") });
  m.push("-", CMD.info, CMD.delete);
  return m.filter((x) => x === "-" || !x.on || x.on());
}

// ------------------------------------------------------------------ menus
let openMenuEl = null;
function closeMenu() { if (openMenuEl) { openMenuEl.remove(); openMenuEl = null; } }
function showMenu(anchor, items, mode) {
  closeMenu();
  const menu = h("div", { class: "menu", role: "menu" });
  let lastSep = true;
  items.forEach((it) => {
    if (it === "-") { if (!lastSep) menu.append(h("div", { class: "ms" })); lastSep = true; return; }
    if (it.header) { menu.append(h("div", { class: "mh" }, it.header)); return; }
    const dis = it.on && !it.on();
    menu.append(h("div", { class: "mi" + (dis ? " dis" : "") + (it.danger ? " danger" : ""), role: "menuitem", style: it.bold ? { fontWeight: 600 } : null,
      onclick: () => { closeMenu(); it.run(); } }, h("span", { html: icon(it.icon || "file"), style: { display: "contents" } }), it.label, it.key ? h("kbd", null, it.key) : null));
    lastSep = false;
  });
  while (menu.lastChild && menu.lastChild.classList.contains("ms")) menu.lastChild.remove();
  document.body.append(menu);
  openMenuEl = menu;
  const mw = menu.offsetWidth, mh = menu.offsetHeight;
  let x, y;
  if (anchor && anchor.getBoundingClientRect && !anchor.clientX) {
    const r = anchor.getBoundingClientRect();
    x = mode === "below" ? r.left : r.right - mw;
    y = r.bottom + 4;
    if (x < 4) x = r.left;
  } else { x = anchor.clientX; y = anchor.clientY; }
  x = Math.max(4, Math.min(x, innerWidth - mw - 4));
  if (y + mh > innerHeight - 4) y = Math.max(4, (anchor.clientY || y) - mh);
  menu.style.left = x + "px";
  menu.style.top = y + "px";
  setTimeout(() => {
    const off = (e) => { if (!menu.contains(e.target)) { closeMenu(); document.removeEventListener("mousedown", off, true); document.removeEventListener("touchstart", off, true); } };
    document.addEventListener("mousedown", off, true);
    document.addEventListener("touchstart", off, true);
  });
}
window.addEventListener("blur", closeMenu);
window.addEventListener("resize", closeMenu);

// ------------------------------------------------------------------ toasts
function toast(text, opts = {}) {
  const t = h("div", { class: "toast " + (opts.kind || "") }, opts.kind ? ic(opts.kind === "ok" ? "checkcircle" : "alert") : null, h("span", { style: { flex: 1 } }, text));
  (opts.actions || []).forEach((a) => t.append(h("button", { onclick: () => { a.run(); t.remove(); } }, a.label)));
  $("#toasts").append(t);
  setTimeout(() => { t.style.transition = "opacity .25s"; t.style.opacity = "0"; setTimeout(() => t.remove(), 260); }, opts.ms || (opts.actions ? 7000 : 3200));
}

// ------------------------------------------------------------------ sidebar
const PLACE_ICONS = { home: "home", desktop: "desktop", documents: "documents", downloads: "downloads", pictures: "pictures", music: "music", videos: "videos", drive: "hdd", sd: "sd" };
function renderNav() {
  const nav = $("#nav");
  nav.innerHTML = "";
  const cur = S.loc ? (S.loc.kind === "archive" ? S.loc.archive : S.loc.kind === "pc" ? "::pc" : S.loc.dir) : "";
  const item = (label, ico, path, onclick, extra) => {
    const active = path != null && cur && path.toLowerCase() === cur.toLowerCase();
    return h("div", { class: "nav-item" + (active ? " active" : ""), title: path && path !== "::pc" ? path : label, onclick: () => { closeDrawer(); onclick(); } },
      h("span", { html: icon(ico), style: { display: "contents" } }), h("span", null, label), extra || null);
  };
  nav.append(h("h4", null, "Places"));
  S.init.places.forEach((p) => nav.append(item(p.name, PLACE_ICONS[p.icon] || "folder", p.path, () => navigate({ kind: "fs", dir: p.path }))));
  if (S.init.drives.length) {
    nav.append(h("h4", null, S.init.platform === "windows" ? "This PC" : "Storage"));
    if (S.init.platform === "windows") nav.append(item("This PC", "pc", "::pc", () => navigate({ kind: "pc" })));
    S.init.drives.forEach((d) => nav.append(item(d.name, "hdd", d.path, () => navigate({ kind: "fs", dir: d.path }))));
  }
  const favs = pref("favorites", []);
  nav.append(h("h4", null, "Favorites"));
  if (!favs.length) nav.append(h("div", { class: "nav-empty" }, "Star a folder or archive to pin it here."));
  favs.forEach((f) => nav.append(item(baseName(f.path) || f.path, f.kind === "archive" ? "archive" : "star", f.path,
    () => openPath(f.path, f.kind),
    h("span", { class: "x", title: "Remove", onclick: (e) => { e.stopPropagation(); toggleFavorite(f.path, f.kind); }, html: icon("x") }))));
  const recent = (pref("recent", []) || []).slice(0, 6);
  if (recent.length) {
    nav.append(h("h4", null, "Recent archives", h("span", { class: "x", style: { cursor: "pointer", display: "flex" }, title: "Clear list", onclick: () => { setPref("recent", []); renderNav(); }, html: icon("x") })));
    recent.forEach((p) => nav.append(item(baseName(p), "clock", p, () => openPath(p, "archive"))));
  }
}

function isFavorite(p) {
  p = p || (S.loc.kind === "archive" ? S.loc.archive : S.loc.dir);
  return pref("favorites", []).some((f) => f.path === p);
}
function toggleFavorite(p, kind) {
  if (typeof p !== "string") { p = S.loc.kind === "archive" ? S.loc.archive : S.loc.dir; kind = S.loc.kind === "archive" ? "archive" : "fs"; }
  if (!p) return;
  let favs = pref("favorites", []).slice();
  if (favs.some((f) => f.path === p)) { favs = favs.filter((f) => f.path !== p); toast("Removed from favorites"); }
  else { favs.push({ path: p, kind }); toast("Added to favorites", { kind: "ok" }); }
  setPref("favorites", favs);
  renderNav();
  updateCommands();
}

// ------------------------------------------------------------------ navigation
function locKey(l) { return l ? l.kind + "|" + (l.archive || "") + "|" + (l.dir || "") + "|" + (l.inner || "") : ""; }

async function navigate(loc, opts = {}) {
  if (S.loc && locKey(S.loc) === locKey(loc) && !opts.force) return;
  const prev = S.loc;
  const prevArc = S.arc;
  try {
    await load(loc, opts);
  } catch (e) {
    if (!isCancel(e)) errorDialog("Cannot open " + (loc.kind === "archive" ? baseName(loc.archive) : loc.dir || "location"), e.message);
    if (!prev) await load({ kind: "fs", dir: S.init.home }).catch(() => {});
    else { S.arc = prevArc; }
    return;
  }
  if (prev && opts.push !== false) { S.back.push(prev); if (S.back.length > 50) S.back.shift(); S.fwd = []; }
  if (S.loc.kind === "fs") setPref("lastDir", S.loc.dir);
  if (opts.select) {
    const it = S.view.find((i) => i.name === opts.select || i.path === opts.select);
    if (it) { S.sel = new Set([it.key]); S.cursor = S.view.indexOf(it); scrollToCursor(); }
  }
  renderAll();
}

async function load(loc, opts = {}) {
  setLoading(true);
  try {
    if (loc.kind === "pc") {
      const r = await api("fs/list", { dir: "" });
      S.items = r.items.map((d) => ({ key: d.path, name: d.name, path: d.path, dir: true, kind: "drive", size: -1, mtime: 0 }));
      S.arc = null;
    } else if (loc.kind === "fs") {
      const r = await api("fs/list", { dir: loc.dir });
      loc = { kind: "fs", dir: r.dir, parent: r.parent };
      const showHidden = pref("showHidden", false);
      S.items = r.items.filter((x) => showHidden || !x.hidden).map((x) => ({ key: x.path, name: x.name, path: x.path, dir: x.dir, size: x.dir ? -1 : x.size, mtime: x.mtime, archive: x.archive, hidden: x.hidden, kind: x.dir ? "folder" : "file" }));
      S.arc = null;
    } else {
      if (!S.arc || S.arc.path !== loc.archive || opts.force) {
        const r = await withPassword(loc.archive, (pw) => api("archive/list", { path: loc.archive, password: pw }));
        S.arc = { path: r.path, info: r.info, format: r.format, tree: buildTree(r.entries), hasPacked: r.entries.some((e) => !e.dir && e.packed >= 0), readOnly: !!loc.readOnly, parent: loc.parentArchive || null, count: r.entries.length };
        loc.archive = r.path;
      }
      const node = S.arc.tree.get(loc.inner || "");
      if (!node) { loc.inner = ""; }
      S.items = childrenOf(loc.inner || "");
    }
    S.loc = loc;
    S.sel = new Set();
    S.cursor = S.items.length ? 0 : -1;
    S.anchor = -1;
    S.filter = "";
    $$(".search input").forEach((i) => (i.value = ""));
    applyView(false);
    $("#list").scrollTop = 0;
  } finally {
    setLoading(false);
  }
}

function setLoading(on) {
  S.loading = on;
  const b = $("#loadbar");
  if (b) b.classList.toggle("hidden", !on);
}

function reload() {
  const keep = new Set(S.sel);
  const top = $("#list").scrollTop;
  return load(Object.assign({}, S.loc), { force: true }).then(() => {
    S.sel = new Set([...keep].filter((k) => S.items.some((i) => i.key === k)));
    $("#list").scrollTop = top;
    renderAll();
  }).catch((e) => {
    if (S.loc.kind === "archive" && e.code === "not_found") { navigate({ kind: "fs", dir: dirName(S.loc.archive) }); return; }
    if (!isCancel(e)) errorDialog("Refresh failed", e.message);
  });
}

function canGoUp() {
  if (!S.loc) return false;
  if (S.loc.kind === "archive") return true;
  if (S.loc.kind === "fs") return S.loc.parent !== null && S.loc.parent !== undefined;
  return false;
}
function goUp() {
  if (!canGoUp()) return;
  const l = S.loc;
  if (l.kind === "archive") {
    if (l.inner) {
      const i = l.inner.lastIndexOf("/");
      navigate(Object.assign({}, l, { inner: i < 0 ? "" : l.inner.slice(0, i) }), { select: l.inner.slice(i + 1) });
    } else if (l.parentArchive) {
      navigate(l.parentArchive, { select: baseName(l.archive) });
    } else navigate({ kind: "fs", dir: dirName(l.archive) }, { select: l.archive });
  } else if (l.parent === "") navigate({ kind: "pc" }, { select: l.dir });
  else navigate({ kind: "fs", dir: l.parent }, { select: l.dir });
}
function goBack() { if (!S.back.length) return; const to = S.back.pop(); S.fwd.push(S.loc); navigate(to, { push: false }); }
function goForward() { if (!S.fwd.length) return; const to = S.fwd.pop(); S.back.push(S.loc); navigate(to, { push: false }); }

async function openPath(p, kind) {
  if (kind === "archive") return navigate({ kind: "archive", archive: p, inner: "" });
  const st = await api("fs/stat", { path: p }).catch(() => ({ exists: false }));
  if (!st.exists) { errorDialog("Not found", p + " no longer exists."); return; }
  if (st.dir) return navigate({ kind: "fs", dir: p });
  if (st.archive) return navigate({ kind: "archive", archive: p, inner: "" });
  return navigate({ kind: "fs", dir: dirName(p) }, { select: p });
}

async function openItem(it) {
  if (!it) return;
  if (S.loc.kind === "pc" || (S.loc.kind === "fs" && it.dir)) return navigate({ kind: "fs", dir: it.path });
  if (S.loc.kind === "fs") {
    if (it.archive) return navigate({ kind: "archive", archive: it.path, inner: "" });
    return openFile(it.path);
  }
  if (it.dir) return navigate(Object.assign({}, S.loc, { inner: it.path }));
  if (isArchiveName(it.name)) {
    // nested archive: extract it to a temporary folder and browse it (read-only)
    const r = await withPassword(S.loc.archive, (pw) => busy("Opening " + it.name, api("archive/temp", { archive: S.loc.archive, password: pw, entry: it.path })));
    return navigate({ kind: "archive", archive: r.path, inner: "", readOnly: true, parentArchive: Object.assign({}, S.loc) });
  }
  return viewEntry(it);
}

async function openFile(p) {
  if (android) { android.openFile(p); return; }
  const r = await api("open", { path: p });
  if (!r.opened) errorDialog("Cannot open file", r.error || "No program is set to open this file.");
}

async function viewEntry(it) {
  try {
    const r = await withPassword(S.loc.archive, (pw) => busy("Opening " + it.name, api(android ? "archive/temp" : "open", { archive: S.loc.archive, password: pw, entry: it.path })));
    if (android) android.openFile(r.path);
    else if (!r.opened) previewDialog(it);
  } catch (e) {
    if (!isCancel(e)) errorDialog("Cannot open " + it.name, e.message);
  }
}

// a small "working…" indicator for short operations
async function busy(text, promise) {
  const t = setTimeout(() => { setLoading(true); $("#statusbar .st-msg") && ($("#statusbar .st-msg").textContent = text + "…"); }, 200);
  try { return await promise; } finally { clearTimeout(t); setLoading(false); }
}

// ------------------------------------------------------------------ archive tree
function buildTree(entries) {
  const tree = new Map();
  const root = { name: "", path: "", dir: true, children: new Map(), size: 0, packed: 0, mtime: 0, files: 0 };
  tree.set("", root);
  const ensureDir = (p) => {
    let n = tree.get(p);
    if (n) return n;
    const i = p.lastIndexOf("/");
    const parent = ensureDir(i < 0 ? "" : p.slice(0, i));
    n = { name: p.slice(i + 1), path: p, dir: true, children: new Map(), size: 0, packed: 0, mtime: 0, files: 0 };
    tree.set(p, n);
    parent.children.set(n.name, n);
    return n;
  };
  for (const e of entries) {
    if (e.dir) { const d = ensureDir(e.path); d.mtime = e.mtime || d.mtime; continue; }
    const i = e.path.lastIndexOf("/");
    const parent = ensureDir(i < 0 ? "" : e.path.slice(0, i));
    parent.children.set(e.path.slice(i + 1), { name: e.path.slice(i + 1), path: e.path, dir: false, size: e.size, packed: e.packed, mtime: e.mtime, encrypted: e.encrypted, link: e.link });
  }
  // folder totals
  const sum = (n) => {
    if (!n.dir) return;
    let size = 0, packed = 0, files = 0, known = true;
    for (const c of n.children.values()) {
      if (c.dir) sum(c);
      size += Math.max(0, c.size); files += c.dir ? c.files : 1;
      if (c.packed >= 0 && known) packed += c.packed; else known = false;
    }
    n.size = size; n.packed = known ? packed : -1; n.files = files;
  };
  sum(root);
  return tree;
}
function childrenOf(dir) {
  const n = S.arc.tree.get(dir);
  if (!n) return [];
  return Array.from(n.children.values()).map((c) => ({ key: "a:" + c.path, name: c.name, path: c.path, dir: c.dir, size: c.size, packed: c.packed, mtime: c.mtime, encrypted: c.encrypted, files: c.files, kind: c.dir ? "folder" : "file" }));
}

// ------------------------------------------------------------------ list view
const COLS = {
  fs: [["name", "Name", "minmax(200px,1fr)"], ["size", "Size", "100px", true], ["type", "Type", "150px"], ["mtime", "Modified", "170px"]],
  archive: [["name", "Name", "minmax(180px,1fr)"], ["size", "Size", "88px", true], ["packed", "Packed", "88px", true], ["ratio", "Ratio", "58px", true], ["type", "Type", "128px"], ["mtime", "Modified", "158px"]],
  pc: [["name", "Name", "minmax(200px,1fr)"], ["type", "Type", "160px"]],
};
function colsFor() {
  let c = COLS[S.loc.kind];
  if (S.loc.kind === "archive") {
    if (S.arc && !S.arc.hasPacked) c = c.filter((x) => x[0] !== "packed" && x[0] !== "ratio");
    const w = ($("#listwrap") || {}).clientWidth || 1000;
    if (w < 760) c = c.filter((x) => x[0] !== "type");
    if (w < 600) c = c.filter((x) => x[0] !== "ratio");
  } else if (S.loc.kind === "fs") {
    const w = ($("#listwrap") || {}).clientWidth || 1000;
    if (w < 560) c = c.filter((x) => x[0] !== "type");
  }
  return c;
}

function sortVal(it, key) {
  switch (key) {
    case "size": return it.size;
    case "packed": return it.packed;
    case "ratio": return it.size > 0 && it.packed >= 0 ? it.packed / it.size : 2;
    case "type": return typeName(it).toLowerCase();
    case "mtime": return it.mtime || 0;
  }
  return it.name.toLowerCase();
}
const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });
function applyView(keepScroll = true) {
  const f = S.filter.trim().toLowerCase();
  let v = f ? S.items.filter((i) => i.name.toLowerCase().includes(f)) : S.items.slice();
  const { key, dir } = S.sort;
  v.sort((a, b) => {
    if (a.dir !== b.dir) return a.dir ? -1 : 1;
    if (key === "name") return dir * collator.compare(a.name, b.name);
    const x = sortVal(a, key), y = sortVal(b, key);
    if (x < y) return -dir;
    if (x > y) return dir;
    return collator.compare(a.name, b.name);
  });
  S.view = v;
  if (S.cursor >= v.length) S.cursor = v.length - 1;
  renderCols();
  drawRows(true);
  renderEmpty();
  renderStatus();
  renderDetails();
  updateCommands();
}

function renderCols() {
  const cols = colsFor();
  const tmpl = cols.map((c) => c[2]).join(" ");
  $("#listwrap").style.setProperty("--cols", tmpl);
  const el = $("#cols");
  el.innerHTML = "";
  cols.forEach(([k, label, , num]) => {
    const on = S.sort.key === k;
    el.append(h("div", { class: num ? "num" : "", onclick: () => { S.sort = { key: k, dir: on ? -S.sort.dir : 1 }; setPref("sort", S.sort); applyView(); } },
      num && on ? ic(S.sort.dir > 0 ? "sortup" : "sortdown") : null, label, !num && on ? ic(S.sort.dir > 0 ? "sortup" : "sortdown") : null));
  });
}

function rowH() { return IS_MOBILE() ? 60 : 34; }
function cellHTML(it, k) {
  switch (k) {
    case "name": {
      const sub = it.kind === "drive" ? "" : it.dir ? (it.files != null ? plural(it.files, "file") : fmtDate(it.mtime)) : fmtSize(it.size) + (it.mtime ? " · " + fmtDate(it.mtime) : "");
      return `<div class="name"><span class="chk">${icon("check")}</span>${tile(it)}<span class="nm"><span>${esc(it.name)}</span><small>${esc(sub)}</small></span>${it.encrypted ? icon("lock", "lock") : ""}</div>`;
    }
    case "size": return `<div class="num sub">${it.kind === "drive" ? "" : it.dir ? (it.size > 0 && inArchive() ? fmtSize(it.size) : "") : fmtSize(it.size)}</div>`;
    case "packed": return `<div class="num sub">${it.packed >= 0 && (!it.dir || it.size > 0) ? fmtSize(it.packed) : ""}</div>`;
    case "ratio": return `<div class="num sub">${it.size > 0 && it.packed >= 0 ? Math.round((it.packed / it.size) * 100) + "%" : ""}</div>`;
    case "type": return `<div class="sub">${esc(typeName(it))}</div>`;
    case "mtime": return `<div class="sub">${esc(fmtDate(it.mtime))}</div>`;
  }
  return "<div></div>";
}
let lastDraw = "";
function drawRows(force) {
  const list = $("#list"), sp = $("#spacer");
  if (!list || !S.loc) return;
  const rh = rowH();
  sp.style.height = S.view.length * rh + "px";
  const top = list.scrollTop, hgt = list.clientHeight || 600;
  const first = Math.max(0, Math.floor(top / rh) - 8), last = Math.min(S.view.length, Math.ceil((top + hgt) / rh) + 8);
  const sig = first + ":" + last + ":" + rh;
  if (!force && sig === lastDraw) return;
  lastDraw = sig;
  const cols = colsFor();
  let html = "";
  for (let i = first; i < last; i++) {
    const it = S.view[i];
    const cls = "row" + (S.sel.has(it.key) ? " sel" : "") + (i === S.cursor ? " cursor" : "") + (it.hidden ? " hid" : "");
    html += `<div class="${cls}" data-i="${i}" style="top:${i * rh}px">` + cols.map((c) => cellHTML(it, c[0])).join("") + "</div>";
  }
  sp.innerHTML = html;
}
function refreshSelection() {
  $$("#spacer .row").forEach((r) => {
    const i = +r.dataset.i, it = S.view[i];
    r.classList.toggle("sel", S.sel.has(it.key));
    r.classList.toggle("cursor", i === S.cursor);
  });
  renderStatus();
  renderDetailsSoon();
  updateCommands();
  if (IS_MOBILE() && S.selmode) $("#m-title").textContent = plural(S.sel.size, "selected").replace(" selecteds", " selected");
}

function renderEmpty() {
  const el = $("#empty");
  if (S.view.length || S.loading) { el.classList.add("hidden"); return; }
  el.classList.remove("hidden");
  el.innerHTML = S.filter
    ? icon("search") + "<b>No matches</b><span>Nothing in this folder matches “" + esc(S.filter) + "”.</span>"
    : inArchive()
      ? icon("archive") + "<b>This folder is empty</b><span>" + (writable() ? "Drop files here or use Add to put files in this archive." : "") + "</span>"
      : icon("folderopen") + "<b>This folder is empty</b><span>Drop files here to compress them.</span>";
}

function scrollToCursor() {
  const list = $("#list"), rh = rowH();
  if (S.cursor < 0) return;
  const y = S.cursor * rh;
  if (y < list.scrollTop) list.scrollTop = y;
  else if (y + rh > list.scrollTop + list.clientHeight) list.scrollTop = y + rh - list.clientHeight;
}

// selection & mouse
function rowIndex(e) { const r = e.target.closest(".row"); return r ? +r.dataset.i : -1; }
function selectIndex(i, e) {
  const it = S.view[i];
  if (!it) return;
  if (e && (e.ctrlKey || e.metaKey)) {
    if (S.sel.has(it.key)) S.sel.delete(it.key); else S.sel.add(it.key);
    S.anchor = i;
  } else if (e && e.shiftKey && S.anchor >= 0) {
    S.sel = new Set();
    const [a, b] = S.anchor < i ? [S.anchor, i] : [i, S.anchor];
    for (let k = a; k <= b; k++) S.sel.add(S.view[k].key);
  } else {
    S.sel = new Set([it.key]);
    S.anchor = i;
  }
  S.cursor = i;
  refreshSelection();
}
function onListMouseDown(e) {
  if (IS_MOBILE()) return;
  const i = rowIndex(e);
  if (i < 0) {
    if (e.button === 0 && !e.ctrlKey && !e.shiftKey) { S.sel = new Set(); refreshSelection(); }
    return;
  }
  if (e.button === 2 && S.sel.has(S.view[i].key)) { S.cursor = i; return; }
  selectIndex(i, e);
}
function onListClick(e) {
  if (!IS_MOBILE()) return;
  if (touch.long) { touch.long = false; return; }
  const i = rowIndex(e);
  if (i < 0) return;
  if (S.selmode) {
    const it = S.view[i];
    if (S.sel.has(it.key)) S.sel.delete(it.key); else S.sel.add(it.key);
    S.cursor = i;
    if (!S.sel.size) setSelMode(false); else refreshSelection();
  } else {
    S.sel = new Set([S.view[i].key]);
    S.cursor = i;
    openItem(S.view[i]);
  }
}
function onListDblClick(e) {
  if (IS_MOBILE()) return;
  const i = rowIndex(e);
  if (i >= 0) openItem(S.view[i]);
}
function onListContext(e) {
  e.preventDefault();
  if (IS_MOBILE()) return;
  const i = rowIndex(e);
  if (i < 0) {
    S.sel = new Set(); refreshSelection();
    showMenu(e, backgroundMenu());
    return;
  }
  showMenu(e, itemMenu());
}
function backgroundMenu() {
  const m = [CMD.add];
  if (inArchive()) m.push(CMD.extract, CMD.test, CMD.info);
  m.push("-", CMD.newFolder, CMD.selectAll, CMD.refresh, "-", { label: isFavorite() ? "Remove from favorites" : "Add to favorites", icon: "star", run: toggleFavorite });
  return m;
}
const touch = { timer: null, long: false };
function onTouchStart(e) {
  const i = rowIndex(e);
  if (i < 0) return;
  touch.long = false;
  clearTimeout(touch.timer);
  touch.timer = setTimeout(() => {
    touch.long = true;
    if (navigator.vibrate) navigator.vibrate(15);
    const it = S.view[i];
    if (!S.selmode) { S.sel = new Set(); setSelMode(true); }
    S.sel.add(it.key);
    S.cursor = i;
    refreshSelection();
  }, 450);
}
function onTouchEnd() { clearTimeout(touch.timer); }
function setSelMode(on) {
  S.selmode = on;
  document.body.classList.toggle("selmode", on);
  if (!on) { S.sel = new Set(); }
  refreshSelection();
  renderMobileHeader();
}
function selectAll() {
  S.sel = new Set(S.view.map((i) => i.key));
  if (IS_MOBILE() && S.view.length) setSelMode(true);
  refreshSelection();
}

// keyboard
let typeAhead = "", typeTimer;
function onListKey(e) {
  const n = S.view.length;
  const move = (to) => {
    if (!n) return;
    to = Math.max(0, Math.min(n - 1, to));
    if (e.shiftKey) {
      if (S.anchor < 0) S.anchor = Math.max(0, S.cursor);
      S.sel = new Set();
      const [a, b] = S.anchor < to ? [S.anchor, to] : [to, S.anchor];
      for (let k = a; k <= b; k++) S.sel.add(S.view[k].key);
      S.cursor = to;
    } else if (e.ctrlKey) {
      S.cursor = to;
    } else {
      S.sel = new Set([S.view[to].key]);
      S.cursor = S.anchor = to;
    }
    scrollToCursor();
    drawRows(true);
    refreshSelection();
    e.preventDefault();
  };
  const page = Math.max(1, Math.floor($("#list").clientHeight / rowH()) - 1);
  switch (e.key) {
    case "ArrowDown": return move(S.cursor + 1);
    case "ArrowUp": return move(S.cursor - 1);
    case "Home": return move(0);
    case "End": return move(n - 1);
    case "PageDown": return move(S.cursor + page);
    case "PageUp": return move(S.cursor - page);
    case " ":
      if (e.ctrlKey && S.cursor >= 0) { const k = S.view[S.cursor].key; S.sel.has(k) ? S.sel.delete(k) : S.sel.add(k); refreshSelection(); e.preventDefault(); }
      return;
    case "Enter":
      if (S.cursor >= 0) { e.preventDefault(); if (S.sel.size > 1 && !inArchive()) return; openItem(S.view[S.cursor]); }
      return;
  }
  if (e.key.length === 1 && !e.ctrlKey && !e.altKey && !e.metaKey) {
    clearTimeout(typeTimer);
    typeAhead += e.key.toLowerCase();
    typeTimer = setTimeout(() => (typeAhead = ""), 800);
    const start = typeAhead.length === 1 ? S.cursor + 1 : S.cursor;
    for (let k = 0; k < n; k++) {
      const idx = (start + k) % n;
      if (S.view[idx].name.toLowerCase().startsWith(typeAhead)) { move(idx); break; }
    }
  }
}

function modalOpen() { return !!document.querySelector(".overlay"); }
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && openMenuEl) { closeMenu(); return; }
  if (modalOpen() || !S.loc || S.setup) return;
  const inInput = e.target.matches("input, textarea, select");
  const k = (e.ctrlKey ? "Ctrl+" : "") + (e.altKey ? "Alt+" : "") + (e.shiftKey ? "Shift+" : "") + (e.key.length === 1 ? e.key.toUpperCase() : e.key);
  const map = {
    "Alt+A": "add", "Alt+E": "extract", "Alt+T": "test", "Alt+V": "view", "Alt+I": "info", "Alt+R": "repair", "Alt+W": "wizard", "Alt+Q": "convert",
    F3: "find", F5: "refresh", "Ctrl+R": "refresh", "Ctrl+Shift+N": "newFolder",
  };
  if (!inInput) Object.assign(map, { Delete: "delete", F2: "rename", "Ctrl+A": "selectAll" });
  if (map[k]) { e.preventDefault(); const c = CMD[map[k]]; if (enabled(c)) c.run(); return; }
  if (k === "Ctrl+F") { e.preventDefault(); const f = $(IS_MOBILE() ? "#filter-m" : "#filter-d"); f.focus(); f.select(); return; }
  if (k === "Ctrl+L" || k === "Alt+D") { e.preventDefault(); editAddress(); return; }
  if (inInput) return;
  if (k === "Backspace" || k === "Alt+ArrowUp") { e.preventDefault(); goUp(); }
  else if (k === "Alt+ArrowLeft" || k === "BrowserBack") { e.preventDefault(); goBack(); }
  else if (k === "Alt+ArrowRight" || k === "BrowserForward") { e.preventDefault(); goForward(); }
  else if (document.activeElement === document.body && /^(Arrow|Home|End|Page)/.test(e.key)) { $("#list").focus(); onListKey(e); }
});
window.addEventListener("mouseup", (e) => { if (e.button === 3) goBack(); if (e.button === 4) goForward(); });

// ------------------------------------------------------------------ address bar
function crumbsFor() {
  const out = [];
  const l = S.loc;
  if (l.kind === "pc") return [{ label: "This PC", icon: "pc", loc: { kind: "pc" } }];
  const fsCrumbs = (p) => {
    const s = sep();
    const res = [];
    if (S.init.platform === "windows") res.push({ label: "This PC", icon: "pc", loc: { kind: "pc" } });
    let acc = "";
    const parts = p.split(s).filter(Boolean);
    if (s === "/") { acc = "/"; if (S.init.platform !== "android") res.push({ label: "/", loc: { kind: "fs", dir: "/" } }); }
    parts.forEach((part, i) => {
      acc = s === "\\" && i === 0 ? part + "\\" : acc.endsWith(s) ? acc + part : acc + s + part;
      res.push({ label: part, loc: { kind: "fs", dir: acc } });
    });
    if (S.init.platform === "android") {
      const home = S.init.home;
      const i = res.findIndex((c) => c.loc.dir === home);
      if (i >= 0) { res.splice(0, i + 1, { label: "Internal storage", icon: "sd", loc: { kind: "fs", dir: home } }); }
    }
    return res;
  };
  if (l.kind === "fs") return fsCrumbs(l.dir);
  const chain = [];
  for (let a = l; a; a = a.parentArchive) chain.unshift(a);
  const first = chain[0];
  out.push(...fsCrumbs(dirName(first.archive)));
  chain.forEach((a, idx) => {
    const isLast = idx === chain.length - 1;
    out.push({ label: idx === 0 ? baseName(a.archive) : baseName(a.archive).replace(/^.*?[\\/]/, ""), arc: true, icon: "archive", loc: Object.assign({}, a, { inner: "" }) });
    if (!isLast) {
      // inner folders of a parent archive lead to the nested archive
      return;
    }
    let acc = "";
    (a.inner || "").split("/").filter(Boolean).forEach((part) => {
      acc = acc ? acc + "/" + part : part;
      out.push({ label: part, loc: Object.assign({}, a, { inner: acc }) });
    });
  });
  return out;
}
function renderCrumbs() {
  const cs = crumbsFor();
  const mk = (c) => h("div", { class: "crumb" + (c.arc ? " arc" : ""), title: c.loc.archive || c.loc.dir || c.label, onclick: (e) => { e.stopPropagation(); navigate(c.loc); } },
    c.icon ? ic(c.icon) : null, h("span", null, c.label));
  const box = $("#crumbs");
  box.innerHTML = "";
  const list = h("div", { class: "crumb-list" });
  cs.forEach((c, i) => { if (i) list.append(h("span", { class: "crumb-sep", html: icon("chevronright") })); list.append(mk(c)); });
  box.append(list);
  // keep the end visible
  requestAnimationFrame(() => { list.scrollLeft = list.scrollWidth; const over = list.scrollWidth > list.clientWidth; if (over) { const kids = $$(".crumb", list); for (let i = 0; i < kids.length - 2 && list.scrollWidth > list.clientWidth; i++) { kids[i].style.display = "none"; if (kids[i].nextSibling) kids[i].nextSibling.style.display = "none"; } } });
  const m = $("#mcrumbs");
  m.innerHTML = "";
  cs.forEach((c) => m.append(mk(c)));
  requestAnimationFrame(() => (m.scrollLeft = m.scrollWidth));
}
function editAddress() {
  if (IS_MOBILE()) return;
  const box = $("#crumbs");
  const cur = S.loc.kind === "archive" ? S.loc.archive + (S.loc.inner ? sep() + S.loc.inner.split("/").join(sep()) : "") : S.loc.dir || "";
  box.innerHTML = "";
  const inp = h("input", { value: cur, spellcheck: "false" });
  const done = (go) => {
    if (go && inp.value.trim() && inp.value.trim() !== cur) goToTyped(inp.value.trim());
    else renderCrumbs();
  };
  inp.addEventListener("keydown", (e) => { if (e.key === "Enter") done(true); if (e.key === "Escape") done(false); });
  inp.addEventListener("blur", () => done(false));
  box.append(inp);
  inp.focus();
  inp.select();
}
async function goToTyped(p) {
  // a path inside an archive: walk up until an archive file is found
  let probe = p.replace(/[\\/]+$/, "");
  const rest = [];
  for (let i = 0; i < 40 && probe; i++) {
    const st = await api("fs/stat", { path: probe }).catch(() => ({ exists: false }));
    if (st.exists) {
      if (st.dir && !rest.length) return navigate({ kind: "fs", dir: probe });
      if (!st.dir && st.archive) return navigate({ kind: "archive", archive: probe, inner: rest.reverse().join("/") });
      if (!st.dir) return navigate({ kind: "fs", dir: dirName(probe) }, { select: probe });
      break;
    }
    rest.push(baseName(probe));
    const up = dirName(probe);
    if (up === probe) break;
    probe = up;
  }
  renderCrumbs();
  errorDialog("Location not found", "ZipNest cannot find “" + p + "”. Check the spelling and try again.");
}

// ------------------------------------------------------------------ banner, details, status
function ratioRing(packed, size, r = 17) {
  const pct = size > 0 && packed >= 0 ? Math.min(1, packed / size) : 0;
  const c = 2 * Math.PI * r;
  return `<svg viewBox="0 0 42 42"><circle cx="21" cy="21" r="${r}" fill="none" stroke="var(--surface-3)" stroke-width="5"/><circle cx="21" cy="21" r="${r}" fill="none" stroke="url(#zg)" stroke-width="5" stroke-linecap="round" stroke-dasharray="${(pct * c).toFixed(1)} ${c.toFixed(1)}"/><defs><linearGradient id="zg"><stop offset="0" stop-color="#8b3dff"/><stop offset="1" stop-color="#b636d6"/></linearGradient></defs></svg>`;
}
function infoChips(info, format) {
  const c = [h("span", { class: "chip accent" }, String(format).toUpperCase())];
  if (info.encrypted) c.push(h("span", { class: "chip amber" }, ic("lock"), info.hiddenNames ? "Encrypted + names" : "Encrypted"));
  if (info.locked) c.push(h("span", { class: "chip red" }, ic("lock"), "Locked"));
  if (info.recoveryPercent) c.push(h("span", { class: "chip green" }, ic("shield"), "Recovery " + info.recoveryPercent + "%"));
  if (info.sfx) c.push(h("span", { class: "chip" }, "SFX"));
  if (info.volumes > 1) c.push(h("span", { class: "chip" }, info.volumes + " volumes"));
  if (S.arc && S.arc.readOnly) c.push(h("span", { class: "chip" }, "Read-only"));
  return c;
}
function renderBanner() {
  const b = $("#banner");
  if (!inArchive() || !S.arc || !S.arc.info) { b.classList.add("hidden"); return; }
  const info = S.arc.info;
  b.classList.remove("hidden");
  b.innerHTML = "";
  const ratio = info.unpacked > 0 && info.size > 0 ? Math.round((info.size / info.unpacked) * 100) : null;
  b.append(
    h("div", { class: "arc-ic", html: icon("archive") }),
    h("div", { class: "t" }, h("b", null, baseName(S.arc.path)),
      h("div", { class: "meta" }, ...infoChips(info, S.arc.format),
        h("span", null, plural(info.files, "file") + (info.dirs ? ", " + plural(info.dirs, "folder") : "")),
        h("span", null, fmtSize(info.unpacked) + " → " + fmtSize(info.size)))),
    ratio != null ? h("div", { class: "ratio", title: "Archive size compared with the original files", html: ratioRing(info.size, info.unpacked) + `<div class="lbl"><b>${ratio}%</b>of original</div>` }) : null,
    h("div", { class: "bn-actions" },
      h("button", { class: "btn primary", onclick: () => CMD.extract.run() }, ic("extract"), "Extract"),
      h("button", { class: "btn", onclick: () => CMD.test.run() }, ic("test"), "Test")));
}

let detTimer;
function renderDetailsSoon() { clearTimeout(detTimer); detTimer = setTimeout(renderDetails, 120); }
let previewSeq = 0;
function renderDetails() {
  const d = $("#details");
  if (!d) return;
  const show = pref("details", true) && S.loc.kind !== "pc";
  d.classList.toggle("hidden", !show);
  if (!show) return;
  const items = selItems();
  d.innerHTML = "";
  const head = h("div", { class: "d-head" });
  const body = h("div", { class: "d-body" });
  d.append(head, body);
  const props = (pairs) => h("dl", { class: "props" }, pairs.filter((p) => p && p[1] !== "" && p[1] != null).map(([k, v]) => [h("dt", null, k), h("dd", null, v)]));
  if (items.length === 1) {
    const it = items[0];
    head.append(h("div", { html: tile(it, true) }), h("b", null, it.name), h("small", null, typeName(it)));
    const pairs = [];
    if (!it.dir || inArchive()) pairs.push(["Size", it.size >= 0 ? fmtSize(it.size) + (it.size >= 1024 ? " (" + it.size.toLocaleString() + " bytes)" : "") : ""]);
    if (inArchive()) {
      if (it.packed >= 0) pairs.push(["Packed", fmtSize(it.packed)]);
      if (it.size > 0 && it.packed >= 0) pairs.push(["Ratio", Math.round((it.packed / it.size) * 100) + "%"]);
      if (it.dir) pairs.push(["Contains", plural(it.files || 0, "file")]);
      if (it.encrypted) pairs.push(["Encrypted", "Yes"]);
    }
    pairs.push(["Modified", fmtDate(it.mtime)], ["Location", inArchive() ? baseName(S.arc.path) + (it.path.includes("/") ? " › " + it.path.slice(0, it.path.lastIndexOf("/")).replace(/\//g, " › ") : "") : dirName(it.path)]);
    const prev = h("div");
    body.append(prev, props(pairs));
    const acts = h("div", { class: "d-actions" });
    if (!it.dir) acts.append(h("button", { class: "btn sm", onclick: () => openItem(it) }, ic(it.archive && !inArchive() ? "archive" : "view"), it.archive && !inArchive() ? "Open" : "Open"));
    if (inArchive() || it.archive) acts.append(h("button", { class: "btn sm", onclick: () => CMD.extract.run() }, ic("extract"), "Extract"));
    if (!inArchive() && !it.archive) acts.append(h("button", { class: "btn sm", onclick: () => cmdAdd() }, ic("add"), "Compress"));
    body.append(acts);
    if (!it.dir && !it.archive && it.size >= 0 && it.size < 64 * 1024 * 1024) loadPreview(prev, it);
  } else if (items.length > 1) {
    const files = items.filter((i) => !i.dir), dirs = items.length - files.length;
    head.append(h("div", { html: `<span class="ft lg folder">${icon("layers")}</span>` }), h("b", null, plural(items.length, "item") + " selected"),
      h("small", null, [files.length ? plural(files.length, "file") : "", dirs ? plural(dirs, "folder") : ""].filter(Boolean).join(", ")));
    const total = items.reduce((a, i) => a + Math.max(0, i.size), 0);
    body.append(props([["Total size", total > 0 ? fmtSize(total) : "—"]]));
    const acts = h("div", { class: "d-actions" });
    if (!inArchive()) acts.append(h("button", { class: "btn sm primary", onclick: () => cmdAdd() }, ic("add"), "Add to archive"));
    if (inArchive() || selArchives().length) acts.append(h("button", { class: "btn sm", onclick: () => CMD.extract.run() }, ic("extract"), "Extract"));
    body.append(acts);
  } else if (inArchive() && S.arc.info) {
    const info = S.arc.info;
    head.append(h("div", { html: `<span class="ft lg arc">${esc(String(S.arc.format).split(".").pop().slice(0, 4))}</span>` }), h("b", null, baseName(S.arc.path)), h("small", null, String(S.arc.format).toUpperCase() + " archive"));
    body.append(h("div", { class: "ratio", style: { margin: "4px 0 14px" }, html: ratioRing(info.size, info.unpacked) + `<div class="lbl"><b>${info.unpacked > 0 ? Math.round((info.size / info.unpacked) * 100) + "%" : "—"}</b>compressed size</div>` }));
    body.append(props([["Files", info.files.toLocaleString()], ["Folders", info.dirs.toLocaleString()], ["Original", fmtSize(info.unpacked)], ["Archive", fmtSize(info.size)],
      ["Saved", info.unpacked > info.size ? fmtSize(info.unpacked - info.size) : "—"], ["Encrypted", info.encrypted ? (info.hiddenNames ? "Yes, with file names" : "Yes") : "No"],
      ["Recovery", info.recoveryPercent ? info.recoveryPercent + "%" : S.arc.format === "nest" ? "None" : ""], ["Locked", info.locked ? "Yes" : ""], ["Modified", fmtDate(info.mtime)]]));
    if (info.comment) body.append(h("div", { class: "section-title" }, "Comment"), h("div", { class: "comment-box" }, info.comment));
    const acts = h("div", { class: "d-actions" },
      h("button", { class: "btn sm primary", onclick: () => CMD.extract.run() }, ic("extract"), "Extract all"),
      h("button", { class: "btn sm", onclick: () => CMD.test.run() }, ic("test"), "Test"),
      h("button", { class: "btn sm", onclick: () => cmdInfo() }, ic("info"), "Info"));
    body.append(acts);
  } else {
    const name = S.loc.dir ? baseName(S.loc.dir) || S.loc.dir : "";
    head.append(h("div", { html: tile({ dir: true, name }, true) }), h("b", null, name), h("small", null, "Folder"));
    const files = S.items.filter((i) => !i.dir);
    const arcs = files.filter((i) => i.archive).length;
    body.append(props([["Folders", S.items.length - files.length], ["Files", files.length], ["Archives", arcs || ""], ["Total size", fmtSize(files.reduce((a, i) => a + i.size, 0))], ["Path", S.loc.dir]]));
    body.append(h("p", { class: "hint", style: { marginTop: "16px" } }, "Select files and choose Add to compress them, or open an archive to see what’s inside. Tip: you can drop files anywhere in this window."));
  }
}

async function loadPreview(el, it) {
  const seq = ++previewSeq;
  const ext = extOf(it.name);
  const textish = EXT_INFO[ext] ? ["text", "source code", "image"].includes(EXT_INFO[ext].type) : true;
  if (!textish && it.size > 1 << 20) return;
  try {
    const body = inArchive() ? { archive: S.loc.archive, password: S.passwords.get(S.loc.archive) || "", entry: it.path } : { path: it.path };
    const r = await api("preview", body);
    if (seq !== previewSeq) return;
    el.append(previewEl(r));
  } catch (e) { /* no preview */ }
}
function previewEl(r) {
  const box = h("div", { class: "preview" });
  if (r.kind === "image") box.append(h("img", { src: r.url, alt: "" }));
  else if (r.kind === "text") box.append(h("pre", null, r.text.slice(0, 20000)));
  else if (r.kind === "hex") box.append(h("pre", null, hexDump(r.hex)));
  if (r.truncated && r.kind !== "image") box.append(h("div", { class: "note" }, "Showing the first " + fmtSize(r.kind === "hex" ? 4096 : r.text.length) + " of " + fmtSize(r.size)));
  return box;
}
function hexDump(hex) {
  let out = "";
  for (let off = 0; off < hex.length / 2; off += 16) {
    const bytes = [];
    for (let j = 0; j < 16 && off + j < hex.length / 2; j++) bytes.push(hex.substr((off + j) * 2, 2));
    const ascii = bytes.map((b) => { const c = parseInt(b, 16); return c >= 32 && c < 127 ? String.fromCharCode(c) : "."; }).join("");
    out += off.toString(16).padStart(6, "0") + "  " + bytes.join(" ").padEnd(48) + "  " + ascii + "\n";
  }
  return out;
}

function renderStatus() {
  const st = $("#statusbar");
  if (!st || !S.loc) return;
  const items = selItems();
  const total = S.view.length;
  const selSize = items.reduce((a, i) => a + Math.max(0, i.size), 0);
  let left = plural(total, "item");
  if (S.filter) left += " (filtered)";
  if (items.length) left += "  ·  " + items.length + " selected" + (selSize > 0 ? " (" + fmtSize(selSize) + ")" : "");
  st.innerHTML = "";
  st.append(h("span", { class: "st-msg" }, left), h("span", { class: "grow" }));
  if (inArchive() && S.arc.info) st.append(h("span", null, String(S.arc.format).toUpperCase() + " · " + fmtSize(S.arc.info.size) + (S.arc.info.encrypted ? " · encrypted" : "")));
  st.append(h("span", null, h("span", { class: "dot" }), "Ready"));
}

function renderMobileHeader() {
  const t = $("#m-title"), s = $("#m-sub");
  if (!t) return;
  if (S.selmode) { t.textContent = S.sel.size + " selected"; s.textContent = ""; return; }
  const l = S.loc;
  if (l.kind === "pc") { t.textContent = "This PC"; s.textContent = ""; }
  else if (l.kind === "fs") { t.textContent = l.dir === S.init.home && S.init.platform === "android" ? "Internal storage" : baseName(l.dir) || l.dir; s.textContent = plural(S.view.length, "item"); }
  else { t.textContent = l.inner ? baseName(l.inner) : baseName(l.archive); s.textContent = String(S.arc.format).toUpperCase() + " archive · " + plural(S.arc.info ? S.arc.info.files : 0, "file"); }
}

function toggleDetails() { setPref("details", !pref("details", true)); renderDetails(); updateCommands(); }

function renderAll() {
  renderNav();
  renderCrumbs();
  renderBanner();
  applyView();
  renderMobileHeader();
  document.title = (S.loc.kind === "archive" ? baseName(S.loc.archive) : S.loc.kind === "pc" ? "This PC" : baseName(S.loc.dir) || S.loc.dir) + " - ZipNest";
  if (window.znSetTitle) window.znSetTitle(document.title);
}

// ------------------------------------------------------------------ password handling
async function withPassword(archive, fn) {
  let pw = S.passwords.get(archive) || "";
  let bad = false;
  for (;;) {
    try {
      return await fn(pw);
    } catch (e) {
      if (e.code === "password_required" || e.code === "bad_password") {
        bad = e.code === "bad_password" || (bad && pw !== "");
        const r = await passwordDialog(archive, e.code === "bad_password");
        if (r == null) throw new ApiError("cancelled", "cancelled");
        pw = r;
        S.passwords.set(archive, pw);
        continue;
      }
      throw e;
    }
  }
}

// ------------------------------------------------------------------ command implementations
function currentArchiveFor(cmdName) {
  if (inArchive()) return [S.loc.archive];
  return selArchives().map((a) => a.path);
}

async function cmdAdd(inputs) {
  if (inArchive()) {
    if (!writable()) {
      return errorDialog("This archive cannot be changed", S.arc.readOnly ? "Archives opened from inside another archive are read-only. Extract it first." : S.arc.info && S.arc.info.locked ? "This archive is locked." : String(S.arc.format).toUpperCase() + " archives can be extracted but not modified. Use Convert to turn it into a ZIP, 7Z or NEST archive.");
    }
    const files = inputs || (await pickerDialog({ title: "Add files to " + baseName(S.arc.path), mode: "files", start: dirName(S.arc.path), ok: "Add" }));
    if (!files || !files.length) return;
    return runArchiveEdit("add", { inputs: files, baseDir: S.loc.inner || "" }, "Adding files to " + baseName(S.arc.path));
  }
  let ins = inputs;
  if (!ins) {
    ins = S.loc.kind === "fs" ? selItems().map((i) => i.path) : [];
    if (!ins.length) {
      ins = await pickerDialog({ title: "Choose files to compress", mode: "files", start: S.loc.dir || S.init.home, ok: "Next" });
      if (!ins || !ins.length) return;
    }
  }
  return addDialog(ins);
}

async function quickAdd(format) {
  const ins = selItems().map((i) => i.path);
  if (!ins.length) return;
  const r = await api("suggest", { inputs: ins, format });
  try {
    const res = await runTask({ kind: "create", inputs: ins, output: r.path, format, level: pref("defaultLevel", 5), mode: "add" }, { title: "Creating " + baseName(r.path) });
    await reload();
    toast("Created " + baseName(res.path) + " (" + fmtSize(res.size) + ")", { kind: "ok", actions: [{ label: "Open", run: () => openPath(res.path, "archive") }] });
  } catch (e) { if (!isCancel(e)) errorDialog("Could not create the archive", e.message); }
}

async function runArchiveEdit(kind, params, title) {
  const arc = S.loc.archive;
  try {
    const res = await withPassword(arc, (pw) => runTask(Object.assign({ kind, archive: arc, password: pw }, params), { title }));
    await reload();
    return res;
  } catch (e) {
    if (!isCancel(e)) errorDialog("The archive was not changed", e.message);
    throw e;
  }
}

async function cmdExtract() {
  const arcs = currentArchiveFor();
  if (!arcs.length) return;
  const select = inArchive() ? selItems().map((i) => i.path) : [];
  return extractDialog({ archives: arcs, select, baseDir: inArchive() ? S.loc.inner || "" : "" });
}

async function cmdQuickExtract(sub) {
  const arcs = currentArchiveFor();
  if (!arcs.length) return;
  const dest = dirName(arcs[0]);
  const select = inArchive() ? selItems().map((i) => i.path) : [];
  return doExtract({ archives: arcs, dest: sub ? (arcs.length > 1 ? dest : joinPath(dest, stem(arcs[0]))) : dest, separate: sub && arcs.length > 1, select, baseDir: inArchive() && select.length ? S.loc.inner || "" : "", overwrite: "rename" });
}

async function doExtract(p) {
  const arc = p.archives[0];
  try {
    const res = await withPassword(arc, (pw) => runTask({ kind: "extract", archives: p.archives, password: pw, dest: p.dest, separate: p.separate, select: p.select, baseDir: p.baseDir, overwrite: p.overwrite },
      { title: "Extracting " + (p.archives.length > 1 ? plural(p.archives.length, "archive") : baseName(arc)) }));
    if (S.loc.kind === "fs") reload();
    const where = p.separate ? p.dest : p.dest;
    const msg = "Extracted " + plural(res.files, "file") + " (" + fmtSize(res.bytes) + ")" + (res.skipped && res.skipped.length ? ", skipped " + res.skipped.length : "");
    if (p.openAfter || pref("openAfterExtract", false)) openDest(where, res);
    if (S.standalone) return res;
    toast(msg, { kind: "ok", actions: [{ label: "Show files", run: () => openDest(where, res, true) }] });
    return res;
  } catch (e) {
    if (!isCancel(e)) errorDialog("Extraction failed", e.message);
    throw e;
  }
}
function openDest(dest, res, inApp) {
  const target = res && res.outputs && res.outputs.length === 1 && !/\.[^\\/]+$/.test(baseName(res.outputs[0])) ? res.outputs[0] : dest;
  if (inApp || S.init.platform !== "windows") navigate({ kind: "fs", dir: target });
  else api("open", { path: target }).catch(() => navigate({ kind: "fs", dir: target }));
}

async function cmdTest() {
  const arcs = currentArchiveFor();
  if (!arcs.length) return;
  try {
    const res = await withPassword(arcs[0], (pw) => runTask({ kind: "test", archives: arcs, password: pw }, { title: "Testing " + (arcs.length > 1 ? plural(arcs.length, "archive") : baseName(arcs[0])) }));
    testResultDialog(arcs, res);
  } catch (e) { if (!isCancel(e)) errorDialog("Test failed", e.message); }
}

async function cmdView(system) {
  const it = selItems()[0];
  if (!it) return;
  if (inArchive()) {
    if (it.dir) return openItem(it);
    return viewEntry(it);
  }
  if (it.dir || it.kind === "drive") return openItem(it);
  if (it.archive && !system) return openItem(it);
  return openFile(it.path);
}

async function cmdDelete() {
  const items = selItems();
  if (!items.length) return;
  const what = items.length === 1 ? "“" + items[0].name + "”" : plural(items.length, "item");
  if (inArchive()) {
    if (!writable()) return;
    if (pref("confirmDelete", true) && !(await confirmDialog({ title: "Delete from archive?", text: "Delete " + what + " from " + baseName(S.arc.path) + "? This cannot be undone.", ok: "Delete", danger: true }))) return;
    await runArchiveEdit("delete", { paths: items.map((i) => i.path) }, "Deleting from " + baseName(S.arc.path)).catch(() => {});
    return;
  }
  const trash = S.init.platform === "windows";
  if (pref("confirmDelete", true) && !(await confirmDialog({ title: trash ? "Move to Recycle Bin?" : "Delete permanently?", text: (trash ? "Move " : "Permanently delete ") + what + (trash ? " to the Recycle Bin?" : "? This cannot be undone."), ok: "Delete", danger: true }))) return;
  try {
    await busy("Deleting", api("fs/delete", { paths: items.map((i) => i.path) }));
    toast((trash ? "Moved to Recycle Bin: " : "Deleted ") + what, { kind: "ok" });
  } catch (e) { errorDialog("Delete failed", e.message); }
  reload();
}

async function cmdRename() {
  const it = selItems()[0];
  if (!it) return;
  const name = await promptDialog({ title: "Rename", label: "New name", value: it.name, ok: "Rename", selectStem: !it.dir });
  if (!name || name === it.name) return;
  if (inArchive()) {
    await runArchiveEdit("rename", { path: it.path, name }, "Renaming").catch(() => {});
    return;
  }
  try {
    const r = await api("fs/rename", { path: it.path, name });
    await reload();
    const n = S.view.find((i) => i.path === r.path);
    if (n) { S.sel = new Set([n.key]); refreshSelection(); }
  } catch (e) { errorDialog("Rename failed", e.message); }
}

async function cmdNewFolder() {
  const name = await promptDialog({ title: "New folder", label: "Folder name", value: "New folder", ok: "Create" });
  if (!name) return;
  try {
    const r = await api("fs/mkdir", { dir: S.loc.dir, name });
    await reload();
    const n = S.view.find((i) => i.path === r.path);
    if (n) { S.sel = new Set([n.key]); S.cursor = S.view.indexOf(n); scrollToCursor(); refreshSelection(); drawRows(true); }
  } catch (e) { errorDialog("Could not create the folder", e.message); }
}

async function cmdInfo(focusComment) {
  if (inArchive()) return archiveInfoDialog(S.arc.path, focusComment);
  const it = selItems()[0];
  if (!it) return;
  if (it.archive) return archiveInfoDialog(it.path);
  propertiesDialog(it);
}

async function cmdRepair() {
  const arc = inArchive() ? S.loc.archive : (selArchives().find((a) => /\.nest$/i.test(a.name)) || {}).path;
  if (!arc) return;
  try {
    const res = await runTask({ kind: "repair", archive: arc }, { title: "Repairing " + baseName(arc) });
    repairResultDialog(arc, res);
    if (S.loc.kind === "fs") reload();
  } catch (e) { if (!isCancel(e)) errorDialog("Repair failed", e.message); }
}

async function cmdConvert() {
  const arcs = currentArchiveFor();
  if (arcs.length) convertDialog(arcs);
}

function cmdCopyPath() {
  const items = selItems();
  let text;
  if (inArchive()) text = items.length ? items.map((i) => S.arc.path + sep() + i.path.split("/").join(sep())).join("\n") : S.arc.path;
  else text = items.map((i) => i.path).join("\n");
  navigator.clipboard.writeText(text).then(() => toast("Path copied", { kind: "ok" }), () => promptDialog({ title: "Path", label: "Copy this path", value: text, ok: "Close" }));
}
function cmdReveal() {
  const it = selItems()[0];
  api("reveal", { path: it ? it.path : S.loc.dir }).catch((e) => errorDialog("Cannot show the file", e.message));
}
async function cmdShare() {
  const items = selItems();
  if (!android) return;
  let paths = items.map((i) => i.path);
  if (inArchive()) {
    paths = [];
    for (const it of items) {
      const r = await withPassword(S.loc.archive, (pw) => api("archive/temp", { archive: S.loc.archive, password: pw, entry: it.path }));
      paths.push(r.path);
    }
  }
  android.share(JSON.stringify(paths));
}

// ------------------------------------------------------------------ drag & drop
function setupDrop() {
  let depth = 0, overlay = null;
  const hasFiles = (e) => e.dataTransfer && Array.from(e.dataTransfer.types || []).includes("Files");
  window.addEventListener("dragenter", (e) => {
    if (!hasFiles(e) || S.setup) return;
    e.preventDefault();
    if (depth++ === 0) {
      overlay = h("div", { id: "drop" }, h("div", null, ic(inArchive() ? "add" : "archive"), inArchive() ? "Drop to add to " + baseName(S.arc.path) : "Drop files to compress them"));
      document.body.append(overlay);
    }
  });
  window.addEventListener("dragover", (e) => { if (hasFiles(e)) { e.preventDefault(); e.dataTransfer.dropEffect = "copy"; } });
  window.addEventListener("dragleave", (e) => { if (hasFiles(e) && --depth <= 0) { depth = 0; overlay && overlay.remove(); } });
  window.addEventListener("drop", async (e) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    depth = 0;
    overlay && overlay.remove();
    if (modalOpen()) return;
    const files = await collectDropped(e.dataTransfer);
    if (!files.length) return;
    if (files.length === 1 && isArchiveName(files[0].rel) && !inArchive()) {
      // a dropped archive: open it (it is copied to a temporary folder first)
      const up = await uploadFiles(files, "Opening " + files[0].rel);
      if (up) navigate({ kind: "archive", archive: up.paths[0], inner: "", readOnly: false });
      return;
    }
    const up = await uploadFiles(files, "Reading dropped files");
    if (!up) return;
    if (inArchive()) { if (writable()) cmdAdd(up.paths); else cmdAdd(); return; }
    addDialog(up.paths, { outDir: S.loc.kind === "fs" ? S.loc.dir : S.init.home });
  });
}
async function collectDropped(dt) {
  const out = [];
  const entries = Array.from(dt.items || []).map((i) => i.webkitGetAsEntry && i.webkitGetAsEntry()).filter(Boolean);
  if (!entries.length) { Array.from(dt.files || []).forEach((f) => out.push({ file: f, rel: f.name })); return out; }
  const walk = async (entry, prefix) => {
    if (entry.isFile) {
      const f = await new Promise((res, rej) => entry.file(res, rej));
      out.push({ file: f, rel: prefix + entry.name });
    } else if (entry.isDirectory) {
      const reader = entry.createReader();
      let batch;
      do {
        batch = await new Promise((res, rej) => reader.readEntries(res, rej));
        for (const c of batch) await walk(c, prefix + entry.name + "/");
      } while (batch.length);
    }
  };
  for (const e of entries) await walk(e, "");
  return out;
}
async function uploadFiles(files, title) {
  const fd = new FormData();
  files.forEach((f) => fd.append("file", f.file, f.rel));
  const t = setTimeout(() => setLoading(true), 150);
  try {
    const r = await fetch("/api/upload", { method: "POST", body: fd });
    if (!r.ok) throw new Error(await r.text());
    return await r.json();
  } catch (e) {
    errorDialog("Could not read the dropped files", e.message);
    return null;
  } finally { clearTimeout(t); setLoading(false); }
}

// ------------------------------------------------------------------ host bridges
window.znOpen = (p) => openPath(p, "auto");        // Android: open a file passed by another app
window.znBack = () => {                              // Android back button; returns true if handled
  if (openMenuEl) { closeMenu(); return true; }
  const ov = $$(".overlay").pop();
  if (ov) { ov.dispatchEvent(new CustomEvent("dismiss")); return true; }
  if (document.body.classList.contains("drawer")) { closeDrawer(); return true; }
  if (S.selmode) { setSelMode(false); return true; }
  if (S.filter) { S.filter = ""; syncFilters(null); applyView(); return true; }
  if (canGoUp() && !(S.init.platform === "android" && S.loc.kind === "fs" && S.loc.dir === S.init.home)) { goUp(); return true; }
  return false;
};
window.znRefresh = () => reload();

// ------------------------------------------------------------------ start
async function start() {
  try {
    S.init = await api("init");
  } catch (e) {
    document.body.innerHTML = '<div style="padding:40px;font:15px system-ui">ZipNest could not start: ' + esc(e.message) + "</div>";
    return;
  }
  S.prefs = S.init.prefs || {};
  if (S.prefs.sort) S.sort = S.prefs.sort;
  applyTheme();
  document.body.classList.add("platform-" + S.init.platform);
  setInterval(() => api("heartbeat").catch(() => {}), 3000);
  api("heartbeat").catch(() => {});

  const action = S.init.action || "";
  if (action === "setup") return setupPage();
  if (action && action !== "open") return standalone(action, S.init.args || []);

  renderShell();
  let first = { kind: "fs", dir: pref("lastDir", "") || S.init.home };
  const st = S.init.start;
  if (st) {
    const s = await api("fs/stat", { path: st }).catch(() => ({ exists: false }));
    if (s.exists && s.dir) first = { kind: "fs", dir: st };
    else if (s.exists && s.archive) first = { kind: "archive", archive: st, inner: "" };
    else if (s.exists) first = { kind: "fs", dir: dirName(st), select: st };
  }
  try {
    await navigate(first, { push: false, select: first.select });
  } catch (e) {}
  if (!S.loc) await navigate({ kind: "fs", dir: S.init.home }, { push: false });
  if (first.kind === "archive" && S.loc.kind === "archive") {
    // the archive's folder is "up" from here
  }
  $("#list").focus();
  if (S.init.platform === "android" && android && android.hasStorage && !android.hasStorage()) storageDialog();
}

// Shell commands from Explorer's context menu run in a small window that shows only the dialog.
async function standalone(action, args) {
  S.standalone = true;
  document.body.classList.add("standalone");
  $("#root").innerHTML = "";
  $("#root").append(h("div", { class: "setup" }, h("div", { class: "setup-head" }, h("img", { src: "/assets/logo.png", alt: "" }), h("div", null, h("h1", null, "ZipNest"), h("p", null, "Working…")))));
  S.loc = { kind: "fs", dir: args[0] ? dirName(args[0]) : S.init.home };
  const quit = (code) => api("exit", { code: code || 0 });
  try {
    switch (action) {
      case "add": await addDialog(args, { standalone: true }); break;
      case "add-zip": {
        const r = await api("suggest", { inputs: args, format: "zip" });
        await runTask({ kind: "create", inputs: args, output: r.path, format: "zip", level: pref("defaultLevel", 5), mode: "add" }, { title: "Creating " + baseName(r.path), foreground: true });
        break;
      }
      case "extract-here": await doExtract({ archives: args, dest: dirName(args[0]), overwrite: "rename", separate: false }); break;
      case "extract-auto": await doExtract({ archives: args, dest: args.length > 1 ? dirName(args[0]) : joinPath(dirName(args[0]), stem(args[0])), separate: args.length > 1, overwrite: "rename" }); break;
      case "extract-dialog": await extractDialog({ archives: args, select: [], baseDir: "", standalone: true }); break;
      case "test": {
        const res = await withPassword(args[0], (pw) => runTask({ kind: "test", archives: args, password: pw }, { title: "Testing " + baseName(args[0]), foreground: true }));
        await testResultDialog(args, res);
        break;
      }
      case "convert": await convertDialog(args, { standalone: true }); break;
    }
  } catch (e) {
    if (!isCancel(e)) await errorDialog("ZipNest", e.message);
  }
  quit(0);
}

window.addEventListener("DOMContentLoaded", start);
