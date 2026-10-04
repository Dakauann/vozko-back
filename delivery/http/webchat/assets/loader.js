(function () {
  "use strict";
  var script = document.currentScript;
  if (!script || window.__vozkoWebchat) {
    return;
  }
  var key = script.getAttribute("data-key");
  if (!key) {
    return;
  }
  window.__vozkoWebchat = true;

  var api = new URL(script.src).origin;
  var prefix = "/public/webchat";
  var settings = window.VozkoChat || {};
  var frame = document.createElement("iframe");
  var position = "right";
  var isOpen = false;

  frame.src = api + prefix + "/" + encodeURIComponent(key) + "/frame?origin=" + encodeURIComponent(window.location.origin);
  frame.title = settings.title || "Chat";
  frame.setAttribute("allow", "clipboard-write");
  frame.setAttribute("referrerpolicy", "origin");

  function applyLayout() {
    var small = window.matchMedia("(max-width: 480px)").matches;
    var s = frame.style;
    s.position = "fixed";
    s.border = "0";
    s.background = "transparent";
    s.colorScheme = "normal";
    s.zIndex = "2147483000";
    s.bottom = "0";
    s.left = position === "left" ? "0" : "auto";
    s.right = position === "left" ? "auto" : "0";
    if (isOpen && small) {
      s.width = "100%";
      s.height = "100%";
      s.top = "0";
    } else if (isOpen) {
      s.width = "420px";
      s.height = Math.min(700, window.innerHeight) + "px";
      s.top = "auto";
    } else {
      s.width = "104px";
      s.height = "104px";
      s.top = "auto";
    }
  }

  function post(message) {
    if (frame.contentWindow) {
      frame.contentWindow.postMessage(message, api);
    }
  }

  window.addEventListener("message", function (event) {
    if (event.origin !== api || event.source !== frame.contentWindow) {
      return;
    }
    var data = event.data || {};
    if (data.type === "vozko-webchat:ready") {
      post({
        type: "vozko-webchat:context",
        identity: typeof settings.identity === "string" ? settings.identity : "",
        page: window.location.href.slice(0, 2000)
      });
    } else if (data.type === "vozko-webchat:layout") {
      isOpen = data.open === true;
      position = data.position === "left" ? "left" : "right";
      applyLayout();
    }
  });
  window.addEventListener("resize", applyLayout);

  settings.open = function () { post({ type: "vozko-webchat:open" }); };
  settings.close = function () { post({ type: "vozko-webchat:close" }); };
  window.VozkoChat = settings;

  function mount() {
    applyLayout();
    document.body.appendChild(frame);
  }
  if (document.body) {
    mount();
  } else {
    document.addEventListener("DOMContentLoaded", mount);
  }
})();
