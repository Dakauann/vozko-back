(function () {
  "use strict";

  var body = document.body;
  var key = body.getAttribute("data-key");
  var prefix = body.getAttribute("data-prefix");
  var app = document.getElementById("app");
  var keys = {
    token: "vozko-webchat:" + key,
    read: "vozko-webchat:read:" + key,
    open: "vozko-webchat:open:" + key,
    draft: "vozko-webchat:draft:" + key
  };

  var TEXT = {
    pt: {
      open: "Abrir chat", minimize: "Minimizar chat", placeholder: "Escreva sua mensagem", send: "Enviar",
      attach: "Anexar arquivo", human: "Falar com um atendente", humanAsked: "Um atendente vai continuar a conversa.",
      typing: "digitando", team: "Equipe", assistant: "Assistente virtual",
      intakeTitle: "Antes de começar, conte quem você é.", name: "Nome", email: "E-mail", phone: "Telefone",
      consent: "Li e aceito a política de privacidade", start: "Começar conversa", skip: "Pular",
      loading: "Carregando a conversa", blocked: "Esta conversa foi encerrada.", unavailable: "Não foi possível abrir o chat.",
      retry: "Tentar de novo", notSent: "Não enviada.", tooMany: "Muitas mensagens em pouco tempo. Aguarde um instante e tente de novo.",
      failed: "Não foi possível enviar. Verifique sua conexão e tente de novo.",
      fileRefused: "Este tipo de arquivo não é aceito. Envie uma imagem ou um PDF.", fileTooLarge: "Arquivo grande demais. O limite é 10 MB.",
      required: "Preencha os campos obrigatórios.", invalid: "Confira os dados informados.", file: "Arquivo",
      unread: "{n} mensagens novas", unreadOne: "1 mensagem nova"
    },
    en: {
      open: "Open chat", minimize: "Minimize chat", placeholder: "Type your message", send: "Send",
      attach: "Attach a file", human: "Talk to a person", humanAsked: "A team member will take it from here.",
      typing: "typing", team: "Team", assistant: "Virtual assistant",
      intakeTitle: "Before we start, tell us who you are.", name: "Name", email: "Email", phone: "Phone",
      consent: "I have read and accept the privacy policy", start: "Start chat", skip: "Skip",
      loading: "Loading the conversation", blocked: "This conversation has ended.", unavailable: "The chat could not be opened.",
      retry: "Try again", notSent: "Not sent.", tooMany: "Too many messages at once. Wait a moment and try again.",
      failed: "Could not send. Check your connection and try again.",
      fileRefused: "This file type is not accepted. Send an image or a PDF.", fileTooLarge: "File is too large. The limit is 10 MB.",
      required: "Please fill in the required fields.", invalid: "Please check your details.", file: "File",
      unread: "{n} new messages", unreadOne: "1 new message"
    },
    es: {
      open: "Abrir chat", minimize: "Minimizar chat", placeholder: "Escribe tu mensaje", send: "Enviar",
      attach: "Adjuntar archivo", human: "Hablar con una persona", humanAsked: "Una persona del equipo seguirá la conversación.",
      typing: "escribiendo", team: "Equipo", assistant: "Asistente virtual",
      intakeTitle: "Antes de empezar, cuéntanos quién eres.", name: "Nombre", email: "Correo", phone: "Teléfono",
      consent: "He leído y acepto la política de privacidad", start: "Empezar", skip: "Omitir",
      loading: "Cargando la conversación", blocked: "Esta conversación ha terminado.", unavailable: "No se pudo abrir el chat.",
      retry: "Intentar de nuevo", notSent: "No enviado.", tooMany: "Demasiados mensajes seguidos. Espera un momento e inténtalo de nuevo.",
      failed: "No se pudo enviar. Revisa tu conexión e inténtalo de nuevo.",
      fileRefused: "Este tipo de archivo no se acepta. Envía una imagen o un PDF.", fileTooLarge: "Archivo demasiado grande. El límite es 10 MB.",
      required: "Completa los campos obligatorios.", invalid: "Revisa los datos.", file: "Archivo",
      unread: "{n} mensajes nuevos", unreadOne: "1 mensaje nuevo"
    },
    de: {
      open: "Chat öffnen", minimize: "Chat minimieren", placeholder: "Nachricht schreiben", send: "Senden",
      attach: "Datei anhängen", human: "Mit einer Person sprechen", humanAsked: "Ein Teammitglied übernimmt das Gespräch.",
      typing: "schreibt", team: "Team", assistant: "Virtueller Assistent",
      intakeTitle: "Bevor es losgeht: Wer sind Sie?", name: "Name", email: "E-Mail", phone: "Telefon",
      consent: "Ich habe die Datenschutzerklärung gelesen und akzeptiere sie", start: "Chat starten", skip: "Überspringen",
      loading: "Gespräch wird geladen", blocked: "Dieses Gespräch ist beendet.", unavailable: "Der Chat konnte nicht geöffnet werden.",
      retry: "Erneut versuchen", notSent: "Nicht gesendet.", tooMany: "Zu viele Nachrichten. Bitte kurz warten und erneut versuchen.",
      failed: "Senden fehlgeschlagen. Verbindung prüfen und erneut versuchen.",
      fileRefused: "Dieser Dateityp wird nicht akzeptiert. Senden Sie ein Bild oder ein PDF.", fileTooLarge: "Datei zu groß. Maximal 10 MB.",
      required: "Bitte Pflichtfelder ausfüllen.", invalid: "Bitte Angaben prüfen.", file: "Datei",
      unread: "{n} neue Nachrichten", unreadOne: "1 neue Nachricht"
    }
  };
  var lang = (navigator.language || "pt").slice(0, 2).toLowerCase();
  var t = TEXT[lang] || TEXT.pt;
  var clock = new Intl.DateTimeFormat(navigator.language || "pt-BR", { hour: "2-digit", minute: "2-digit" });

  var GLYPHS = {
    chat: ["M3 20l1.3 -3.9c-2.324 -3.437 -1.426 -7.872 2.1 -10.374c3.526 -2.501 8.59 -2.296 11.845 .48c3.255 2.777 3.695 7.266 1.029 10.501c-2.666 3.235 -7.615 4.215 -11.574 2.293l-4.7 1"],
    minimize: ["M6 9l6 6l6 -6"],
    send: ["M10 14l11 -11", "M21 3l-6.5 18a.55 .55 0 0 1 -1 0l-3.5 -7l-7 -3.5a.55 .55 0 0 1 0 -1l18 -6.5"],
    clip: ["M15 7l-6.5 6.5a1.5 1.5 0 0 0 3 3l6.5 -6.5a3 3 0 0 0 -6 -3l-6.5 6.5a4.5 4.5 0 0 0 9 9l6.5 -6.5"],
    person: ["M8 7a4 4 0 1 0 8 0a4 4 0 0 0 -8 0", "M6 21v-2a4 4 0 0 1 4 -4h4a4 4 0 0 1 4 4v2"],
    check: ["M5 12l5 5l10 -10"],
    alert: ["M3 12a9 9 0 1 0 18 0a9 9 0 0 0 -18 0", "M12 8v4", "M12 16h.01"],
    file: ["M14 3v4a1 1 0 0 0 1 1h4", "M17 21h-10a2 2 0 0 1 -2 -2v-14a2 2 0 0 1 2 -2h7l5 5v11a2 2 0 0 1 -2 2z"]
  };

  var state = {
    parentOrigin: resolveParentOrigin(),
    identity: "",
    page: "",
    token: readStore(window.localStorage, keys.token),
    widget: {
      name: body.getAttribute("data-name") || "Chat",
      accentColor: body.getAttribute("data-accent") || "#1F6FEB",
      position: body.getAttribute("data-position") === "left" ? "left" : "right",
      launcherLabel: body.getAttribute("data-label") || ""
    },
    session: null,
    failed: false,
    visitor: null,
    open: false,
    human: false,
    blocked: false,
    messages: [],
    seen: {},
    pending: {},
    options: [],
    lastAt: null,
    lastReadAt: readStore(window.localStorage, keys.read),
    unread: 0,
    typingTimer: null,
    typingSent: 0
  };

  var ui = {};

  function resolveParentOrigin() {
    var ancestors = window.location.ancestorOrigins;
    if (ancestors && ancestors.length > 0) {
      return ancestors[0];
    }
    return new URLSearchParams(window.location.search).get("origin") || "";
  }

  function readStore(store, name) {
    try { return store.getItem(name) || ""; } catch (e) { return ""; }
  }

  function writeStore(store, name, value) {
    try {
      if (value) { store.setItem(name, value); } else { store.removeItem(name); }
    } catch (e) {}
  }

  function writeToken(token) {
    state.token = token;
    writeStore(window.localStorage, keys.token, token);
  }

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) { node.className = className; }
    if (text !== undefined && text !== null) { node.textContent = text; }
    return node;
  }

  function glyph(name) {
    var ns = "http://www.w3.org/2000/svg";
    var svg = document.createElementNS(ns, "svg");
    svg.setAttribute("viewBox", "0 0 24 24");
    svg.setAttribute("class", "glyph");
    svg.setAttribute("aria-hidden", "true");
    GLYPHS[name].forEach(function (d) {
      var p = document.createElementNS(ns, "path");
      p.setAttribute("d", d);
      svg.appendChild(p);
    });
    return svg;
  }

  function parseHex(hex) {
    var m = /^#([0-9a-f]{6})$/i.exec(hex || "");
    if (!m) { return null; }
    var n = parseInt(m[1], 16);
    return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
  }

  function luminance(rgb) {
    var c = rgb.map(function (v) {
      v = v / 255;
      return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
    });
    return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
  }

  function contrast(a, b) {
    var la = luminance(a);
    var lb = luminance(b);
    return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
  }

  function shade(rgb, factor) {
    return rgb.map(function (v) { return Math.round(v * factor); });
  }

  function hex(rgb) {
    return "#" + rgb.map(function (v) { return ("0" + v.toString(16)).slice(-2); }).join("");
  }

  function darkenUntil(rgb, target, minimum) {
    var out = rgb;
    for (var i = 0; i < 24 && contrast(out, target) < minimum; i++) {
      out = shade(out, 0.9);
    }
    return out;
  }

  function applyAccent(color) {
    var rgb = parseHex(color) || parseHex("#1F6FEB");
    var white = [255, 255, 255];
    var graphite = [13, 15, 16];
    var darkInk = contrast(rgb, graphite) >= contrast(rgb, white);
    var root = document.documentElement.style;
    root.setProperty("--accent", hex(rgb));
    root.setProperty("--accent-ink", darkInk ? "#0D0F10" : "#FFFFFF");
    root.setProperty("--accent-hover", hex(shade(rgb, darkInk ? 0.93 : 0.9)));
    root.setProperty("--accent-edge", hex(darkenUntil(shade(rgb, 0.82), white, 3)));
    root.setProperty("--ring", hex(darkenUntil(rgb, white, 3)));
    root.setProperty("--accent-text", hex(darkenUntil(rgb, white, 4.5)));
  }

  function api(method, path, payload, isForm) {
    var headers = {};
    if (state.token) { headers.Authorization = "Bearer " + state.token; }
    var init = { method: method, headers: headers, credentials: "omit" };
    if (payload !== undefined) {
      if (isForm) {
        init.body = payload;
      } else {
        headers["Content-Type"] = "application/json";
        init.body = JSON.stringify(payload);
      }
    }
    return fetch(prefix + path, init).then(function (res) {
      if (res.status === 204) { return null; }
      return res.json().catch(function () { return {}; }).then(function (data) {
        if (!res.ok) {
          var err = new Error(data && data.error ? data.error : "request_failed");
          err.status = res.status;
          throw err;
        }
        return data;
      });
    });
  }

  function notify(message) {
    if (state.parentOrigin) {
      window.parent.postMessage(message, state.parentOrigin);
    }
  }

  function layout() {
    var position = state.widget.position === "left" ? "left" : "right";
    app.className = position + (state.open ? " open" : "");
    notify({ type: "vozko-webchat:layout", open: state.open, position: position });
  }

  function clientId() {
    if (window.crypto && window.crypto.randomUUID) { return window.crypto.randomUUID(); }
    var bytes = new Uint8Array(16);
    window.crypto.getRandomValues(bytes);
    return Array.prototype.map.call(bytes, function (b) { return ("0" + b.toString(16)).slice(-2); }).join("");
  }

  function leadingZeroBits(buffer) {
    var bytes = new Uint8Array(buffer);
    var total = 0;
    for (var i = 0; i < bytes.length; i++) {
      if (bytes[i] === 0) { total += 8; continue; }
      return total + Math.clz32(bytes[i]) - 24;
    }
    return total;
  }

  function solve(challenge) {
    var encoder = new TextEncoder();
    var batch = 512;
    function attempt(start) {
      var work = [];
      for (var n = start; n < start + batch; n++) {
        work.push(window.crypto.subtle.digest("SHA-256", encoder.encode(challenge.token + ":" + n)));
      }
      return Promise.all(work).then(function (digests) {
        for (var i = 0; i < digests.length; i++) {
          if (leadingZeroBits(digests[i]) >= challenge.bits) { return String(start + i); }
        }
        return attempt(start + batch);
      });
    }
    return attempt(0);
  }

  function startSession() {
    var base = { parentOrigin: state.parentOrigin, identity: state.identity, locale: navigator.language || "" };
    var resume = state.token
      ? api("POST", "/" + key + "/session", Object.assign({ token: state.token }, base)).catch(function (err) {
          if (err.status === 400 || err.status === 401) {
            writeToken("");
            return null;
          }
          throw err;
        })
      : Promise.resolve(null);
    return resume.then(function (view) {
      if (view) { return view; }
      return api("POST", "/" + key + "/challenge", { parentOrigin: state.parentOrigin }).then(function (challenge) {
        return solve(challenge).then(function (nonce) {
          return api("POST", "/" + key + "/session", Object.assign({ challengeToken: challenge.token, nonce: nonce }, base));
        });
      });
    }).then(function (view) {
      writeToken(view.token);
      state.widget = view.widget;
      state.visitor = view.visitor;
      applyAccent(view.widget.accentColor);
      return view;
    });
  }

  function renderShell() {
    app.textContent = "";

    ui.panel = el("section", "panel");
    ui.panel.hidden = !state.open;
    ui.panel.setAttribute("role", "dialog");
    ui.panel.setAttribute("aria-label", state.widget.name);

    var header = el("header", "header");
    var avatar = el("span", "avatar");
    avatar.appendChild(glyph("chat"));
    header.appendChild(avatar);
    var titles = el("div", "titles");
    var title = state.widget.welcomeTitle || state.widget.teamName || state.widget.name;
    titles.appendChild(el("h1", null, title));
    var subtitle = state.widget.teamName && state.widget.teamName !== title ? state.widget.teamName : "";
    if (subtitle) { titles.appendChild(el("p", null, subtitle)); }
    header.appendChild(titles);
    var minimize = el("button", "icon-button");
    minimize.type = "button";
    minimize.setAttribute("aria-label", t.minimize);
    minimize.appendChild(glyph("minimize"));
    minimize.addEventListener("click", function () { setOpen(false); ui.launcher.focus(); });
    header.appendChild(minimize);
    ui.panel.appendChild(header);

    ui.handoff = el("div", "handoff");
    ui.panel.appendChild(ui.handoff);

    ui.body = el("div", "body");
    ui.panel.appendChild(ui.body);

    ui.launcher = el("button", "launcher");
    ui.launcher.type = "button";
    ui.launcher.setAttribute("aria-label", state.widget.launcherLabel || t.open);
    ui.launcher.setAttribute("aria-expanded", String(state.open));
    ui.launcher.appendChild(glyph("chat"));
    ui.badge = el("span", "badge");
    ui.badge.setAttribute("aria-hidden", "true");
    ui.launcher.appendChild(ui.badge);
    ui.launcher.addEventListener("click", function () { setOpen(!state.open); });

    app.appendChild(ui.panel);
    app.appendChild(ui.launcher);
    renderHandoff();
    renderBody();
    renderBadge();
    layout();
  }

  function renderHandoff() {
    ui.handoff.textContent = "";
    var offer = state.visitor && !state.blocked && state.widget.allowHumanRequest && !(state.visitor.intakePending);
    ui.handoff.hidden = !offer;
    if (!offer) { return; }
    if (state.human) {
      ui.handoff.className = "handoff done";
      ui.handoff.appendChild(glyph("check"));
      ui.handoff.appendChild(el("span", null, t.humanAsked));
      return;
    }
    ui.handoff.className = "handoff";
    var button = el("button", "link-button");
    button.type = "button";
    button.appendChild(glyph("person"));
    button.appendChild(el("span", null, t.human));
    button.addEventListener("click", requestHuman);
    ui.handoff.appendChild(button);
  }

  function renderBody() {
    ui.body.textContent = "";
    ui.thread = null;
    ui.input = null;
    if (!state.visitor) {
      ui.body.appendChild(state.failed ? renderFailure() : renderLoading());
      return;
    }
    if (state.blocked) {
      var ended = el("div", "state");
      ended.appendChild(el("p", null, t.blocked));
      ui.body.appendChild(ended);
      return;
    }
    if (state.visitor.intakePending) {
      ui.body.appendChild(renderIntake());
      return;
    }
    ui.thread = el("div", "thread");
    ui.thread.setAttribute("role", "log");
    ui.thread.setAttribute("aria-live", "polite");
    ui.thread.setAttribute("aria-relevant", "additions");
    ui.body.appendChild(ui.thread);

    ui.typing = el("div", "typing");
    ui.typing.hidden = true;
    var dots = el("span", "dots");
    dots.appendChild(el("span"));
    dots.appendChild(el("span"));
    dots.appendChild(el("span"));
    ui.typing.appendChild(dots);
    ui.typingLabel = el("span");
    ui.typing.appendChild(ui.typingLabel);
    ui.body.appendChild(ui.typing);

    ui.notice = renderNotice();
    ui.body.appendChild(ui.notice);
    ui.body.appendChild(renderComposer());
    renderThread();
  }

  function renderLoading() {
    var box = el("div", "state");
    box.setAttribute("aria-busy", "true");
    var stack = el("div", "stack");
    stack.appendChild(el("div", "skeleton"));
    stack.appendChild(el("span", null, t.loading));
    box.appendChild(stack);
    return box;
  }

  function renderFailure() {
    var box = el("div", "state");
    var stack = el("div", "stack");
    stack.appendChild(el("p", null, t.unavailable));
    var retry = el("button", "secondary", t.retry);
    retry.type = "button";
    retry.addEventListener("click", function () {
      state.failed = false;
      renderBody();
      ensureSession();
    });
    stack.appendChild(retry);
    box.appendChild(stack);
    return box;
  }

  function renderNotice() {
    var notice = el("div", "notice");
    notice.setAttribute("role", "alert");
    notice.hidden = true;
    notice.appendChild(glyph("alert"));
    notice.appendChild(el("span"));
    return notice;
  }

  function showNotice(notice, text) {
    notice.lastChild.textContent = text;
    notice.hidden = !text;
  }

  function renderIntake() {
    var intake = state.widget.intake;
    var form = el("form", "intake");
    form.noValidate = true;
    form.appendChild(el("p", "lede", t.intakeTitle));
    var inputs = {};
    [["name", "text", t.name, "name"], ["email", "email", t.email, "email"], ["phone", "tel", t.phone, "tel"]].forEach(function (f) {
      var rule = intake[f[0]];
      if (rule !== "optional" && rule !== "required") { return; }
      var label = el("label", "field-label");
      var caption = el("span", null, f[2]);
      if (rule === "required") {
        var mark = el("span", "required", "*");
        mark.setAttribute("aria-hidden", "true");
        caption.appendChild(mark);
      }
      label.appendChild(caption);
      var input = el("input");
      input.type = f[1];
      input.autocomplete = f[3];
      input.required = rule === "required";
      input.value = state.visitor[f[0]] || "";
      if (state.visitor.verified && input.value) { input.readOnly = true; }
      label.appendChild(input);
      form.appendChild(label);
      inputs[f[0]] = input;
    });
    var trap = el("label", "trap", "Website");
    var trapInput = el("input");
    trapInput.type = "text";
    trapInput.tabIndex = -1;
    trapInput.autocomplete = "off";
    trap.appendChild(trapInput);
    form.appendChild(trap);

    var consent = null;
    if (intake.privacyPolicyUrl && /^https:\/\//.test(intake.privacyPolicyUrl)) {
      var consentLabel = el("label", "consent");
      consent = el("input");
      consent.type = "checkbox";
      consent.required = true;
      consentLabel.appendChild(consent);
      var link = el("a", null, t.consent);
      link.href = intake.privacyPolicyUrl;
      link.target = "_blank";
      link.rel = "noopener noreferrer";
      consentLabel.appendChild(link);
      form.appendChild(consentLabel);
    }

    var notice = renderNotice();
    form.appendChild(notice);

    var actions = el("div", "actions");
    var submit = el("button", "primary", t.start);
    submit.type = "submit";
    actions.appendChild(submit);
    if (!state.visitor.intakeRequired) {
      var skip = el("button", "link-button", t.skip);
      skip.type = "button";
      skip.addEventListener("click", function () { sendIntake({}, notice, submit); });
      actions.appendChild(skip);
    }
    form.appendChild(actions);

    form.addEventListener("submit", function (event) {
      event.preventDefault();
      var answers = { website: trapInput.value, consent: consent ? consent.checked : false };
      Object.keys(inputs).forEach(function (k) { answers[k] = inputs[k].value; });
      sendIntake(answers, notice, submit);
    });
    return form;
  }

  function sendIntake(answers, notice, submit) {
    submit.disabled = true;
    api("POST", "/session/intake", answers).then(function (visitor) {
      state.visitor = visitor && typeof visitor.intakePending === "boolean"
        ? visitor
        : Object.assign({}, state.visitor, { intakePending: false, intakeRequired: false });
      renderHandoff();
      renderBody();
      focusComposer();
    }).catch(function (err) {
      submit.disabled = false;
      showNotice(notice, err.message === "field_required" ? t.required : err.status === 429 ? t.tooMany : t.invalid);
    });
  }

  function renderComposer() {
    var composer = el("form", "composer");
    var field = el("div", "field");
    ui.input = el("textarea");
    ui.input.rows = 1;
    ui.input.maxLength = 2000;
    ui.input.placeholder = t.placeholder;
    ui.input.setAttribute("aria-label", t.placeholder);
    ui.input.value = readStore(window.sessionStorage, keys.draft);
    field.appendChild(ui.input);

    if (state.widget.allowAttachments) {
      var attach = el("button", "icon-button");
      attach.type = "button";
      attach.setAttribute("aria-label", t.attach);
      attach.appendChild(glyph("clip"));
      var file = el("input");
      file.type = "file";
      file.accept = "image/jpeg,image/png,image/webp,image/gif,application/pdf";
      file.hidden = true;
      attach.addEventListener("click", function () { file.click(); });
      file.addEventListener("change", function () {
        if (file.files && file.files[0]) { upload(file.files[0]); }
        file.value = "";
      });
      field.appendChild(attach);
      field.appendChild(file);
    }

    var send = el("button", "send");
    send.type = "submit";
    send.setAttribute("aria-label", t.send);
    send.appendChild(glyph("send"));
    field.appendChild(send);
    composer.appendChild(field);

    function sync() {
      send.disabled = !ui.input.value.trim();
      ui.input.style.height = "auto";
      ui.input.style.height = Math.min(ui.input.scrollHeight, 120) + "px";
    }
    ui.input.addEventListener("keydown", function (event) {
      if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
        event.preventDefault();
        composer.requestSubmit();
      }
    });
    ui.input.addEventListener("input", function () {
      writeStore(window.sessionStorage, keys.draft, ui.input.value);
      sync();
      typed();
    });
    composer.addEventListener("submit", function (event) {
      event.preventDefault();
      var text = ui.input.value.trim();
      if (!text) { return; }
      ui.input.value = "";
      writeStore(window.sessionStorage, keys.draft, "");
      sync();
      sendMessage({ text: text }, clientId());
    });
    requestAnimationFrame(sync);
    return composer;
  }

  function focusComposer() {
    if (state.open && ui.input) { ui.input.focus(); }
  }

  function typed() {
    var now = Date.now();
    if (now - state.typingSent > 3000) {
      state.typingSent = now;
      api("POST", "/session/typing", { typing: true }).catch(function () {});
    }
    clearTimeout(state.typingTimer);
    state.typingTimer = setTimeout(function () {
      state.typingSent = 0;
      api("POST", "/session/typing", { typing: false }).catch(function () {});
    }, 2500);
  }

  function errorText(err) {
    if (err.status === 429) { return t.tooMany; }
    if (err.status === 415) { return t.fileRefused; }
    if (err.status === 413) { return t.fileTooLarge; }
    return t.failed;
  }

  function handleBlocked(err) {
    if (err.status === 403 && err.message === "visitor_blocked") {
      state.blocked = true;
      renderHandoff();
      renderBody();
      return true;
    }
    return false;
  }

  function showError(err) {
    if (handleBlocked(err) || !ui.notice) { return; }
    showNotice(ui.notice, errorText(err));
    clearTimeout(state.noticeTimer);
    state.noticeTimer = setTimeout(function () { if (ui.notice) { showNotice(ui.notice, ""); } }, 8000);
  }

  function sendMessage(payload, id) {
    var pending = { id: "pending:" + id, clientId: id, author: "visitor", text: payload.text || "", createdAt: new Date().toISOString(), state: "sending", payload: payload };
    if (payload.text) {
      state.pending[id] = pending;
    }
    state.options = [];
    renderThread();
    var request = Object.assign({ clientMessageId: id, pageUrl: state.page }, payload);
    return api("POST", "/session/messages", request).then(function (message) {
      delete state.pending[id];
      addMessage(message);
    }).catch(function (err) {
      if (handleBlocked(err)) { return; }
      if (state.pending[id]) {
        state.pending[id].state = "failed";
        state.pending[id].error = errorText(err);
        renderThread();
      } else {
        showError(err);
      }
    });
  }

  function retry(pending) {
    pending.state = "sending";
    renderThread();
    sendMessage(pending.payload, pending.clientId);
  }

  function upload(file) {
    var form = new FormData();
    form.append("file", file);
    form.append("clientMessageId", clientId());
    form.append("pageUrl", state.page);
    api("POST", "/session/media", form, true).then(addMessage).catch(showError);
  }

  function requestHuman() {
    api("POST", "/session/handoff").then(function () {
      state.human = true;
      renderHandoff();
    }).catch(showError);
  }

  function matchesPending(message) {
    if (message.author !== "visitor") { return; }
    Object.keys(state.pending).forEach(function (id) {
      if (message.id.slice(-id.length - 1) === "_" + id) { delete state.pending[id]; }
    });
  }

  function addMessage(message) {
    if (!message || !message.id) { return; }
    matchesPending(message);
    if (state.seen[message.id]) { renderThread(); return; }
    state.seen[message.id] = true;
    state.messages.push(message);
    state.messages.sort(function (a, b) { return Date.parse(a.createdAt) - Date.parse(b.createdAt); });
    if (message.options && message.options.length) { state.options = message.options; }
    if (!state.lastAt || Date.parse(message.createdAt) > Date.parse(state.lastAt)) { state.lastAt = message.createdAt; }
    if (message.author !== "visitor" && isUnread(message)) {
      state.unread += 1;
      renderBadge();
    }
    renderThread();
  }

  function isUnread(message) {
    if (state.open && !document.hidden) {
      markRead();
      return false;
    }
    return !state.lastReadAt || Date.parse(message.createdAt) > Date.parse(state.lastReadAt);
  }

  function markRead() {
    var latest = state.lastAt || new Date().toISOString();
    state.lastReadAt = latest;
    writeStore(window.localStorage, keys.read, latest);
    if (state.unread) {
      state.unread = 0;
      renderBadge();
    }
  }

  function renderBadge() {
    if (!ui.badge) { return; }
    ui.badge.hidden = state.unread === 0 || state.open;
    ui.badge.textContent = state.unread > 9 ? "9+" : String(state.unread);
    var label = state.widget.launcherLabel || t.open;
    if (state.unread > 0 && !state.open) {
      label += ". " + (state.unread === 1 ? t.unreadOne : t.unread.replace("{n}", String(state.unread)));
    }
    ui.launcher.setAttribute("aria-label", label);
  }

  function authorLabel(author) {
    if (author === "team") { return state.widget.teamName || t.team; }
    return state.widget.assistantName || t.assistant;
  }

  function renderThread() {
    if (!ui.thread) { return; }
    var nearBottom = ui.thread.scrollHeight - ui.thread.scrollTop - ui.thread.clientHeight < 80;
    ui.thread.textContent = "";
    if (state.widget.welcomeMessage) {
      ui.thread.appendChild(el("div", "intro", state.widget.welcomeMessage));
    }
    var items = state.messages.concat(Object.keys(state.pending).map(function (id) { return state.pending[id]; }));
    var run = null;
    var runAuthor = null;
    items.forEach(function (m, index) {
      if (m.author !== runAuthor) {
        run = el("div", "run " + m.author);
        if (m.author !== "visitor") { run.appendChild(el("span", "who", authorLabel(m.author))); }
        ui.thread.appendChild(run);
        runAuthor = m.author;
      }
      run.appendChild(renderBubble(m));
      var next = items[index + 1];
      if (m.state === "failed") {
        run.appendChild(renderFailed(m));
      } else if (!next || next.author !== m.author) {
        run.appendChild(el("span", "meta", m.state === "sending" ? "" : clock.format(new Date(m.createdAt))));
      }
    });
    if (state.options.length) {
      var options = el("div", "options");
      state.options.forEach(function (o) {
        var b = el("button", "option", o.title);
        b.type = "button";
        b.addEventListener("click", function () {
          sendMessage({ selectionId: o.id }, clientId());
        });
        options.appendChild(b);
      });
      ui.thread.appendChild(options);
    }
    if (nearBottom || items.length && items[items.length - 1].author === "visitor") {
      ui.thread.scrollTop = ui.thread.scrollHeight;
    }
  }

  function renderBubble(m) {
    var bubble = el("div", "bubble" + (m.state ? " " + m.state : ""));
    bubble.title = m.createdAt ? new Date(m.createdAt).toLocaleString() : "";
    if (m.text) { bubble.appendChild(document.createTextNode(m.text)); }
    if (m.media && /^https:\/\//.test(m.media.url)) {
      if (m.media.kind === "image" || m.media.kind === "sticker") {
        var img = el("img");
        img.src = m.media.url;
        img.alt = m.media.filename || t.file;
        img.loading = "lazy";
        img.addEventListener("load", function () {
          if (ui.thread) { ui.thread.scrollTop = ui.thread.scrollHeight; }
        });
        bubble.appendChild(img);
      } else {
        var link = el("a", "file-link");
        link.href = m.media.url;
        link.target = "_blank";
        link.rel = "noopener noreferrer";
        link.appendChild(glyph("file"));
        link.appendChild(el("span", null, m.media.filename || t.file));
        bubble.appendChild(link);
      }
    }
    return bubble;
  }

  function renderFailed(m) {
    var meta = el("span", "meta failed");
    meta.appendChild(el("span", null, t.notSent));
    var again = el("button", "link-button", t.retry);
    again.type = "button";
    again.addEventListener("click", function () { retry(m); });
    meta.appendChild(again);
    meta.title = m.error || "";
    return meta;
  }

  function loadHistory(after) {
    var query = after ? "?after=" + encodeURIComponent(after) : "";
    return api("GET", "/session/messages" + query).then(function (view) {
      var before = state.unread;
      (view.messages || []).forEach(addMessage);
      if (view.options && view.options.length) { state.options = view.options; }
      if (view.human && !state.human) {
        state.human = true;
        renderHandoff();
      }
      if (state.unread !== before) { renderBadge(); }
      renderThread();
    });
  }

  function showTyping(on) {
    if (!ui.typing) { return; }
    ui.typing.hidden = !on;
    ui.typingLabel.textContent = on ? authorLabel(state.human ? "team" : "assistant") + " " + t.typing : "";
  }

  function handleEvent(name, payload) {
    if (name === "message" && payload.message) {
      addMessage(payload.message);
      showTyping(false);
    } else if (name === "typing") {
      showTyping(payload.typing === true);
    } else if (name === "status" && payload.state === "blocked") {
      state.blocked = true;
      renderHandoff();
      renderBody();
    } else if (name === "status" && payload.state === "human" && !state.human) {
      state.human = true;
      renderHandoff();
    }
  }

  function connect(attempt) {
    var controller = new AbortController();
    fetch(prefix + "/session/stream", {
      headers: { Authorization: "Bearer " + state.token },
      credentials: "omit",
      signal: controller.signal
    }).then(function (res) {
      if (!res.ok || !res.body) {
        var err = new Error("stream");
        err.status = res.status;
        throw err;
      }
      attempt = 0;
      if (state.lastAt) { loadHistory(state.lastAt).catch(function () {}); }
      var reader = res.body.getReader();
      var decoder = new TextDecoder();
      var buffer = "";
      function pump() {
        return reader.read().then(function (chunk) {
          if (chunk.done) { return; }
          buffer += decoder.decode(chunk.value, { stream: true });
          var parts = buffer.split("\n\n");
          buffer = parts.pop();
          parts.forEach(function (block) {
            var name = "message";
            var data = "";
            block.split("\n").forEach(function (line) {
              if (line.indexOf("event: ") === 0) { name = line.slice(7); }
              if (line.indexOf("data: ") === 0) { data += line.slice(6); }
            });
            if (data) {
              try { handleEvent(name, JSON.parse(data)); } catch (e) {}
            }
          });
          return pump();
        });
      }
      return pump();
    }).catch(function (err) {
      if (err && (err.status === 401 || err.status === 403)) {
        controller.abort();
        return;
      }
      attempt += 1;
    }).then(function () {
      if (controller.signal.aborted || state.blocked) { return; }
      var delay = Math.min(30000, 1000 * Math.pow(2, Math.min(attempt, 5)));
      setTimeout(function () { connect(attempt); }, delay);
    });
  }

  function setOpen(open) {
    state.open = open;
    writeStore(window.sessionStorage, keys.open, open ? "1" : "");
    if (ui.panel) { ui.panel.hidden = !open; }
    if (ui.launcher) { ui.launcher.setAttribute("aria-expanded", String(open)); }
    if (open) {
      markRead();
      ensureSession();
    }
    renderBadge();
    layout();
    if (open) { focusComposer(); }
  }

  function ensureSession() {
    if (state.session) { return state.session; }
    state.failed = false;
    state.session = startSession().then(function () {
      renderShell();
      return loadHistory(null);
    }).then(function () {
      if (state.open) { markRead(); }
      focusComposer();
      connect(0);
    }).catch(function () {
      state.session = null;
      state.failed = true;
      if (ui.body) { renderBody(); }
    });
    return state.session;
  }

  function boot() {
    if (!state.parentOrigin) {
      return;
    }
    applyAccent(state.widget.accentColor);
    state.open = readStore(window.sessionStorage, keys.open) === "1";
    renderShell();
    if (state.open || state.token || state.identity) {
      ensureSession();
    }
  }

  document.addEventListener("visibilitychange", function () {
    if (!document.hidden && state.open) { markRead(); }
  });

  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape" && state.open) {
      setOpen(false);
      if (ui.launcher) { ui.launcher.focus(); }
    }
  });

  var booted = false;
  window.addEventListener("message", function (event) {
    if (event.source !== window.parent || event.origin !== state.parentOrigin) {
      return;
    }
    var data = event.data || {};
    if (data.type === "vozko-webchat:context" && !booted) {
      booted = true;
      state.identity = typeof data.identity === "string" ? data.identity.slice(0, 4096) : "";
      state.page = typeof data.page === "string" ? data.page.slice(0, 2000) : "";
      boot();
    } else if (data.type === "vozko-webchat:open") {
      setOpen(true);
    } else if (data.type === "vozko-webchat:close") {
      setOpen(false);
    }
  });

  notify({ type: "vozko-webchat:ready" });
  setTimeout(function () {
    if (!booted) {
      booted = true;
      boot();
    }
  }, 1500);
})();
