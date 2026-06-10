// Theme toggle (system / light / dark) — mirrors the app's no-flash approach.
(function () {
  var KEY = "seeurchin-site-theme";
  var root = document.documentElement;

  function systemDark() {
    return window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches;
  }
  function resolve(mode) {
    return mode === "dark" || (mode === "system" && systemDark()) ? "dark" : "light";
  }
  function apply(mode) {
    root.setAttribute("data-theme", resolve(mode));
  }

  var mode = localStorage.getItem(KEY) || "system";
  apply(mode);

  // Re-apply on OS scheme change while following the system.
  if (window.matchMedia) {
    window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", function () {
      if ((localStorage.getItem(KEY) || "system") === "system") apply("system");
    });
  }

  document.addEventListener("DOMContentLoaded", function () {
    var btn = document.getElementById("theme-toggle");
    if (btn) {
      btn.addEventListener("click", function () {
        var current = root.getAttribute("data-theme") === "dark" ? "light" : "dark";
        localStorage.setItem(KEY, current);
        apply(current);
      });
    }

    // Copy buttons on code blocks.
    document.querySelectorAll(".copy-btn").forEach(function (b) {
      b.addEventListener("click", function () {
        var block = b.closest(".codeblock").querySelector("code");
        navigator.clipboard.writeText(block.innerText.trim()).then(function () {
          var prev = b.textContent;
          b.textContent = "Copied!";
          setTimeout(function () { b.textContent = prev; }, 1400);
        });
      });
    });
  });
})();
