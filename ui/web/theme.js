// Applies the saved theme before the page is drawn (no flash of the wrong theme).
(function () {
  var t = "system";
  try { t = localStorage.getItem("zn-theme") || "system"; } catch (e) {}
  if (t === "system") t = window.matchMedia && matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  document.documentElement.setAttribute("data-theme", t);
})();
