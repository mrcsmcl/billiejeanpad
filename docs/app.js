(() => {
  "use strict";

  const STORAGE_KEY = "billie-jean-pad.control.config.v2";
  const MAX_ACTIVITY = 6;
  const STALE_AFTER_SECONDS = 7 * 60;

  const SOUNDS = {
    subtle: ["Hee", "Huh", "Uh", "Hoo", "He", "Kiss", "Sshh", "Ha", "Ha 2", "Ooh"],
    punchy: ["Hehe", "Hehehe", "AH", "Aah", "Ooh strong", "Go", "No no", "Oh baby", "Daoh"],
    scare: ["Stab Hit 3", "Stab Hit 5", "DJ Scratches"]
  };
  const FAVORITES = [
    { name: "Hee", category: "subtle" },
    { name: "Hehe", category: "punchy" },
    { name: "Go", category: "punchy" },
    { name: "Kiss", category: "subtle" },
    { name: "No no", category: "punchy" },
    { name: "Stab Hit 3", category: "scare" },
    { name: "Ooh strong", category: "punchy" },
    { name: "Sshh", category: "subtle" },
    { name: "DJ Scratches", category: "scare" }
  ];
  const CATEGORY_LABELS = { subtle: "Sutil", punchy: "Médio", scare: "Susto" };
  const PRESET_LABELS = {
    fantasma: "Fantasma · cerca de 1× ao dia",
    discreto: "Discreto · cerca de 2–3× ao dia",
    travesso: "Travesso · cerca de 4–8× ao dia"
  };

  const byId = id => document.getElementById(id);
  const els = {
    liveState: byId("liveState"),
    liveLabel: byId("liveLabel"),
    settingsButton: byId("settingsButton"),
    settingsDialog: byId("settingsDialog"),
    settingsForm: byId("settingsForm"),
    closeSettings: byId("closeSettings"),
    baseUrlInput: byId("baseUrlInput"),
    keyInput: byId("keyInput"),
    copyInviteButton: byId("copyInviteButton"),
    clearConfigButton: byId("clearConfigButton"),
    secureContextWarning: byId("secureContextWarning"),
    padTab: byId("padTab"),
    machinesTab: byId("machinesTab"),
    padPanel: byId("padPanel"),
    machinesPanel: byId("machinesPanel"),
    target: byId("target"),
    onlineCount: byId("onlineCount"),
    favoritePads: byId("favoritePads"),
    allSounds: byId("allSounds"),
    randomButton: byId("randomButton"),
    stopButton: byId("stopButton"),
    activityLog: byId("activityLog"),
    machineList: byId("machineList"),
    toast: byId("toast")
  };

  let config = null;
  let eventSource = null;
  let reconnectTimer = null;
  let hmacKeyPromise = null;
  let toastTimer = null;
  const hosts = new Map();
  const activity = [];

  function loadConfig() {
    try {
      const parsed = JSON.parse(localStorage.getItem(STORAGE_KEY));
      if (parsed?.baseUrl && parsed?.key) return parsed;
    } catch (_) {}
    return null;
  }

  async function deriveTopicPrefix(key) {
    if (!window.crypto?.subtle) throw new Error("Abra o painel pela URL HTTPS do GitHub Pages.");
    const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(key));
    const first16 = new Uint8Array(digest).slice(0, 16);
    return "bjp-" + Array.from(first16, byte => byte.toString(16).padStart(2, "0")).join("");
  }

  async function saveConfig(next) {
    const clean = {
      baseUrl: next.baseUrl.replace(/\/+$/, ""),
      key: next.key.trim()
    };
    clean.prefix = await deriveTopicPrefix(clean.key);
    config = clean;
    localStorage.setItem(STORAGE_KEY, JSON.stringify(clean));
    hmacKeyPromise = null;
  }

  async function importInviteFragment() {
    if (!location.hash) return false;
    const params = new URLSearchParams(location.hash.slice(1));
    const key = params.get("key");
    const baseUrl = params.get("server") || "https://ntfy.sh";
    if (!key) return false;
    await saveConfig({ baseUrl, key });
    history.replaceState(null, "", location.pathname + location.search);
    return true;
  }

  function openSettings() {
    const source = config || { baseUrl: "https://ntfy.sh", key: "" };
    els.baseUrlInput.value = source.baseUrl;
    els.keyInput.value = source.key;
    els.secureContextWarning.hidden = Boolean(window.crypto?.subtle);
    if (!els.settingsDialog.open) els.settingsDialog.showModal();
  }

  function closeSettings() {
    if (els.settingsDialog.open) els.settingsDialog.close();
  }

  function switchView(view) {
    const machines = view === "machines";
    els.padTab.setAttribute("aria-selected", String(!machines));
    els.machinesTab.setAttribute("aria-selected", String(machines));
    els.padPanel.hidden = machines;
    els.machinesPanel.hidden = !machines;
  }

  function setConnection(online, label = online ? "Ao vivo" : "Offline") {
    els.liveState.dataset.online = String(online);
    els.liveLabel.textContent = label;
  }

  function showToast(message, kind = "success") {
    clearTimeout(toastTimer);
    els.toast.textContent = message;
    els.toast.dataset.kind = kind;
    els.toast.classList.add("is-visible");
    toastTimer = setTimeout(() => els.toast.classList.remove("is-visible"), 2800);
  }

  function selectedTarget() { return els.target.value; }
  function targetLabel() { return els.target.selectedOptions[0]?.textContent?.replace(/\s+[●◌]$/, "") || "Todas as máquinas"; }
  function formatTime(date) {
    return new Intl.DateTimeFormat("pt-BR", { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(date);
  }

  function describeCommand(command) {
    if (command.cmd === "play") {
      if (command.sound) return command.sound;
      if (command.category) return CATEGORY_LABELS[command.category] || command.category;
      return "Surpresa";
    }
    if (command.cmd === "timer") return command.enabled === false ? "Automático desligado" : `Automático ${command.preset}`;
    if (command.cmd === "stop") return "Som interrompido";
    if (command.cmd === "uninstall") return "Aplicativo removido";
    return command.cmd;
  }

  function addActivity(command, destination) {
    activity.unshift({ time: formatTime(new Date()), action: describeCommand(command), target: destination });
    activity.splice(MAX_ACTIVITY);
    renderActivity();
  }

  function renderActivity() {
    els.activityLog.replaceChildren();
    if (!activity.length) {
      const empty = document.createElement("div");
      empty.className = "empty-state";
      empty.textContent = "Nenhuma ação enviada nesta sessão.";
      els.activityLog.appendChild(empty);
      return;
    }
    for (const item of activity) {
      const row = document.createElement("div");
      row.className = "activity-row";
      const time = document.createElement("span");
      time.className = "activity-time";
      time.textContent = item.time;
      const action = document.createElement("span");
      action.className = "activity-action";
      action.textContent = item.action;
      const target = document.createElement("span");
      target.className = "activity-target";
      target.textContent = item.target;
      row.append(time, action, target);
      els.activityLog.appendChild(row);
    }
  }

  function buildSoundButton(sound, favorite = false) {
    const button = document.createElement("button");
    button.type = "button";
    button.dataset.category = sound.category;
    button.dataset.sound = sound.name;
    if (favorite) {
      button.className = "pad";
      const name = document.createElement("span");
      name.className = "pad-name";
      name.textContent = sound.name;
      const meta = document.createElement("span");
      meta.className = "pad-meta";
      meta.textContent = CATEGORY_LABELS[sound.category];
      button.append(name, meta);
    } else {
      button.className = "sound-button";
      button.textContent = sound.name;
    }
    button.addEventListener("click", () => {
      if (favorite) {
        button.classList.add("is-lit");
        setTimeout(() => button.classList.remove("is-lit"), 360);
      }
      send({ cmd: "play", sound: sound.name });
    });
    return button;
  }

  function buildSounds() {
    els.favoritePads.replaceChildren(...FAVORITES.map(sound => buildSoundButton(sound, true)));
    els.allSounds.replaceChildren();
    for (const [category, names] of Object.entries(SOUNDS)) {
      const group = document.createElement("section");
      group.className = "sound-group";
      const heading = document.createElement("h3");
      heading.textContent = CATEGORY_LABELS[category];
      const list = document.createElement("div");
      list.className = "sound-list";
      names.forEach(name => list.appendChild(buildSoundButton({ name, category })));
      group.append(heading, list);
      els.allSounds.appendChild(group);
    }
  }

  function canonicalCommand(command) {
    return [
      command.cmd,
      command.sound || "",
      command.category || "",
      command.random ? "1" : "0",
      command.enabled === undefined ? "" : (command.enabled ? "1" : "0"),
      command.preset || "",
      command.ts
    ].join("|");
  }

  function canonicalHeartbeat(heartbeat) {
    return [
      heartbeat.host,
      heartbeat.v,
      heartbeat.ts,
      heartbeat.timer ? "1" : "0",
      heartbeat.preset || ""
    ].join("|");
  }

  async function getHmacKey() {
    if (!window.crypto?.subtle) throw new Error("Abra o painel pela URL HTTPS do GitHub Pages.");
    if (!hmacKeyPromise) {
      hmacKeyPromise = crypto.subtle.importKey(
        "raw", new TextEncoder().encode(config.key),
        { name: "HMAC", hash: "SHA-256" }, false, ["sign", "verify"]
      );
    }
    return hmacKeyPromise;
  }

  async function signText(text) {
    const key = await getHmacKey();
    const bytes = await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(text));
    return Array.from(new Uint8Array(bytes), byte => byte.toString(16).padStart(2, "0")).join("");
  }

  async function verifyHeartbeat(heartbeat) {
    if (!heartbeat?.host || !heartbeat?.v || !heartbeat?.sig || !Number.isFinite(heartbeat.ts)) return false;
    const age = Math.abs(Math.floor(Date.now() / 1000) - heartbeat.ts);
    if (age > STALE_AFTER_SECONDS) return false;
    const expected = await signText(canonicalHeartbeat(heartbeat));
    return expected === heartbeat.sig.toLowerCase();
  }

  async function send(command, options = {}) {
    const { silent = false, forceAll = false, explicitTarget = null, explicitLabel = null } = options;
    if (!config) {
      openSettings();
      if (!silent) showToast("Configure a chave do grupo antes de usar.", "error");
      return false;
    }
    const destination = explicitTarget || (forceAll ? "all" : selectedTarget());
    const destinationLabel = explicitLabel || (destination === "all" ? "Todas as máquinas" : targetLabel());
    try {
      command.ts = Math.floor(Date.now() / 1000);
      command.sig = await signText(canonicalCommand(command));
      const topic = destination === "all" ? `${config.prefix}-all` : destination;
      const response = await fetch(`${config.baseUrl}/${encodeURIComponent(topic)}`, {
        method: "POST",
        headers: { "Content-Type": "text/plain;charset=UTF-8" },
        body: JSON.stringify(command)
      });
      if (!response.ok) throw new Error(`Servidor respondeu ${response.status}.`);
      if (!silent) {
        addActivity(command, destinationLabel);
        showToast(`${describeCommand(command)} enviado para ${destinationLabel}.`);
      }
      return true;
    } catch (error) {
      if (!silent) showToast(error.message || "Não foi possível enviar o comando.", "error");
      return false;
    }
  }

  function hostTopic(id) { return `${config.prefix}-${id}`; }
  function selectedHostInfo() {
    if (!config || selectedTarget() === "all") return null;
    const id = selectedTarget().slice((config.prefix + "-").length);
    return hosts.get(id) || null;
  }

  function renderPresetState() {
    const info = selectedHostInfo();
    document.querySelectorAll(".preset-button").forEach(button => {
      const preset = button.dataset.preset;
      const active = info ? (preset === "off" ? !info.timer : info.timer && info.preset === preset) : false;
      button.classList.toggle("is-active", active);
      button.setAttribute("aria-pressed", String(active));
    });
  }

  function ageLabel(seconds) {
    if (seconds < 60) return `${Math.max(0, seconds)} s atrás`;
    return `${Math.round(seconds / 60)} min atrás`;
  }

  function renderMachines() {
    els.machineList.replaceChildren();
    if (!hosts.size) {
      const empty = document.createElement("div");
      empty.className = "empty-state";
      empty.textContent = "Nenhuma máquina autenticada apareceu neste grupo.";
      els.machineList.appendChild(empty);
      return;
    }
    const now = Math.floor(Date.now() / 1000);
    const sorted = [...hosts.entries()].sort((a, b) => b[1].lastSeen - a[1].lastSeen);
    for (const [id, info] of sorted) {
      const online = now - info.lastSeen <= STALE_AFTER_SECONDS;
      const card = document.createElement("article");
      card.className = "machine-card" + (online ? " is-online" : "");

      const summary = document.createElement("div");
      summary.className = "machine-summary";
      const light = document.createElement("span");
      light.className = "machine-light";
      light.setAttribute("aria-hidden", "true");
      const name = document.createElement("span");
      name.className = "machine-name";
      name.textContent = id;
      const seen = document.createElement("span");
      seen.className = "machine-seen";
      seen.textContent = ageLabel(Math.max(0, now - info.lastSeen));
      summary.append(light, name, seen);

      const mode = document.createElement("p");
      mode.className = "machine-mode";
      mode.textContent = info.timer ? `Automático: ${PRESET_LABELS[info.preset] || info.preset}` : "Automático: desligado";

      const actions = document.createElement("div");
      actions.className = "machine-actions";
      const openPad = document.createElement("button");
      openPad.type = "button";
      openPad.className = "machine-action";
      openPad.textContent = "Abrir no Pad";
      openPad.disabled = !online;
      openPad.addEventListener("click", () => {
        els.target.value = hostTopic(id);
        renderPresetState();
        switchView("pad");
        window.scrollTo({ top: 0, behavior: "smooth" });
      });
      const remove = document.createElement("button");
      remove.type = "button";
      remove.className = "machine-action danger";
      remove.textContent = "Desinstalar";
      remove.disabled = !online;
      remove.addEventListener("click", () => {
        const confirmed = confirm(`Desinstalar Billie Jean Pad de “${id}”?\n\nO aplicativo será removido dessa máquina.`);
        if (!confirmed) return;
        send({ cmd: "uninstall" }, { explicitTarget: hostTopic(id), explicitLabel: id });
      });
      actions.append(openPad, remove);
      card.append(summary, mode, actions);
      els.machineList.appendChild(card);
    }
  }

  function renderHosts() {
    const now = Math.floor(Date.now() / 1000);
    const current = els.target.value;
    const sorted = [...hosts.entries()].sort((a, b) => b[1].lastSeen - a[1].lastSeen);
    const onlineCount = sorted.filter(([, info]) => now - info.lastSeen <= STALE_AFTER_SECONDS).length;
    els.onlineCount.textContent = String(onlineCount);

    els.target.replaceChildren();
    const allOption = document.createElement("option");
    allOption.value = "all";
    allOption.textContent = "Todas as máquinas";
    els.target.appendChild(allOption);
    for (const [id, info] of sorted) {
      const online = now - info.lastSeen <= STALE_AFTER_SECONDS;
      const option = document.createElement("option");
      option.value = hostTopic(id);
      option.textContent = `${id} ${online ? "●" : "◌"}`;
      els.target.appendChild(option);
    }
    if ([...els.target.options].some(option => option.value === current)) els.target.value = current;
    renderPresetState();
    renderMachines();
  }

  async function processRosterMessage(event) {
    try {
      const envelope = JSON.parse(event.data);
      if (envelope.event !== "message") return;
      const payload = JSON.parse(envelope.message);
      if (!(await verifyHeartbeat(payload))) return;
      hosts.set(payload.host, {
        lastSeen: payload.ts,
        timer: Boolean(payload.timer),
        preset: payload.preset || "discreto",
        version: payload.v
      });
      renderHosts();
    } catch (_) {}
  }

  function disconnect() {
    eventSource?.close();
    if (reconnectTimer) clearTimeout(reconnectTimer);
    eventSource = null;
    reconnectTimer = null;
    setConnection(false);
  }

  function connect() {
    disconnect();
    if (!config) return;
    eventSource = new EventSource(`${config.baseUrl}/${encodeURIComponent(config.prefix + "-roster")}/sse`);
    eventSource.addEventListener("open", () => {
      setConnection(true);
      send({ cmd: "ping" }, { silent: true, forceAll: true });
    });
    eventSource.addEventListener("message", processRosterMessage);
    eventSource.addEventListener("error", () => {
      setConnection(false, "Reconectando");
      eventSource?.close();
      reconnectTimer = setTimeout(connect, 5000);
    });
  }

  async function copyInvite() {
    const baseUrl = els.baseUrlInput.value.trim().replace(/\/+$/, "");
    const key = els.keyInput.value.trim();
    if (!baseUrl || key.length !== 64) {
      showToast("Informe a chave de 64 caracteres antes de copiar.", "error");
      return;
    }
    const fragment = new URLSearchParams({ key, server: baseUrl });
    const invite = `${location.origin}${location.pathname}#${fragment}`;
    try {
      await navigator.clipboard.writeText(invite);
      showToast("Convite copiado. Envie somente para quem pode controlar o grupo.");
    } catch (_) {
      showToast("O navegador não permitiu copiar o convite.", "error");
    }
  }

  function registerServiceWorker() {
    const localDev = location.hostname === "localhost" || location.hostname === "127.0.0.1";
    if ("serviceWorker" in navigator && (location.protocol === "https:" || localDev)) {
      navigator.serviceWorker.register("./sw.js").catch(() => {});
    }
  }

  els.settingsButton.addEventListener("click", openSettings);
  els.closeSettings.addEventListener("click", closeSettings);
  els.padTab.addEventListener("click", () => switchView("pad"));
  els.machinesTab.addEventListener("click", () => switchView("machines"));
  els.settingsForm.addEventListener("submit", async event => {
    event.preventDefault();
    const baseUrl = els.baseUrlInput.value.trim();
    const key = els.keyInput.value.trim().toLowerCase();
    if (!baseUrl || !/^[0-9a-f]{64}$/.test(key)) {
      showToast("A chave deve ter exatamente 64 caracteres hexadecimais.", "error");
      return;
    }
    await saveConfig({ baseUrl, key });
    hosts.clear();
    renderHosts();
    els.settingsDialog.close();
    connect();
    showToast("Grupo conectado.");
  });
  els.copyInviteButton.addEventListener("click", copyInvite);
  els.clearConfigButton.addEventListener("click", () => {
    if (!confirm("Apagar a chave salva neste aparelho?")) return;
    localStorage.removeItem(STORAGE_KEY);
    config = null;
    hosts.clear();
    disconnect();
    renderHosts();
    openSettings();
  });
  els.target.addEventListener("change", renderPresetState);
  els.randomButton.addEventListener("click", () => send({ cmd: "play", random: true }));
  els.stopButton.addEventListener("click", () => send({ cmd: "stop" }));
  document.querySelectorAll(".category-button").forEach(button => {
    button.addEventListener("click", () => send({ cmd: "play", category: button.dataset.category }));
  });
  document.querySelectorAll(".preset-button").forEach(button => {
    button.addEventListener("click", async () => {
      const preset = button.dataset.preset;
      const command = preset === "off" ? { cmd: "timer", enabled: false } : { cmd: "timer", enabled: true, preset };
      if (await send(command)) {
        const info = selectedHostInfo();
        if (info) {
          info.timer = preset !== "off";
          if (preset !== "off") info.preset = preset;
          renderHosts();
        }
      }
    });
  });

  async function initialize() {
    buildSounds();
    renderActivity();
    config = loadConfig();
    const imported = await importInviteFragment();
    if (imported) config = loadConfig();
    renderHosts();
    registerServiceWorker();
    if (!window.crypto?.subtle) {
      setConnection(false, "Requer HTTPS");
      openSettings();
    } else if (config) {
      connect();
      if (imported) showToast("Convite importado. Grupo conectado.");
    } else {
      openSettings();
    }
    setInterval(renderHosts, 15000);
  }

  initialize();
})();
