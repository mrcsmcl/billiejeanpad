(() => {
  "use strict";

  const STORAGE_KEY = "billie-jean-pad.control.config.v2";
  const MAX_ACTIVITY = 6;
  const ONLINE_AFTER_SECONDS = 15 * 60;
  const ROSTER_HISTORY_SECONDS = 12 * 60 * 60;
  const ROSTER_MEMORY_SECONDS = 30 * 24 * 60 * 60;
  const MIN_PING_INTERVAL_MS = 60 * 1000;

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
    agendaTab: byId("agendaTab"),
    padPanel: byId("padPanel"),
    machinesPanel: byId("machinesPanel"),
    agendaPanel: byId("agendaPanel"),
    target: byId("target"),
    onlineCount: byId("onlineCount"),
    favoritePads: byId("favoritePads"),
    allSounds: byId("allSounds"),
    randomButton: byId("randomButton"),
    stopButton: byId("stopButton"),
    stopDock: document.querySelector(".stop-dock"),
    activityLog: byId("activityLog"),
    machineList: byId("machineList"),
    refreshMachines: byId("refreshMachines"),
    scheduleForm: byId("scheduleForm"),
    scheduleTarget: byId("scheduleTarget"),
    scheduleAction: byId("scheduleAction"),
    scheduleKind: byId("scheduleKind"),
    scheduleAt: byId("scheduleAt"),
    scheduleClock: byId("scheduleClock"),
    windowStart: byId("windowStart"),
    windowEnd: byId("windowEnd"),
    timezoneNote: byId("timezoneNote"),
    scheduleList: byId("scheduleList"),
    toast: byId("toast")
  };

  let config = null;
  let eventSource = null;
  let reconnectTimer = null;
  let hmacKeyPromise = null;
  let toastTimer = null;
  let lastScheduleRefresh = 0;
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
    const tabs = {
      pad: [els.padTab, els.padPanel],
      machines: [els.machinesTab, els.machinesPanel],
      agenda: [els.agendaTab, els.agendaPanel]
    };
    for (const [name, [tab, panel]] of Object.entries(tabs)) {
      const active = name === view;
      tab.setAttribute("aria-selected", String(active));
      panel.hidden = !active;
    }
    els.stopDock.hidden = view !== "pad";
    if (view === "agenda") renderSchedules();
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
    let base = [
      command.cmd,
      command.sound || "",
      command.category || "",
      command.random ? "1" : "0",
      command.enabled === undefined ? "" : (command.enabled ? "1" : "0"),
      command.preset || "",
      command.ts
    ].join("|");
    if (command.cmd.startsWith("schedule.")) {
      base += `|${command.scheduleId || ""}|${command.schedule ? canonicalSchedule(command.schedule) : ""}`;
    }
    return base;
  }

  function canonicalSchedule(item) {
    return [
      item.id || "",
      item.kind || "",
      item.sound || "",
      item.category || "",
      item.random ? "1" : "0",
      item.at || "",
      (item.weekdays || []).slice().sort((a, b) => a-b).join(","),
      item.clock || "",
      item.windowStart || "",
      item.windowEnd || "",
      item.timezone || "",
      item.enabled ? "1" : "0",
      item.nextRun || 0,
      item.lastRun || 0
    ].join("~");
  }

  function canonicalSchedules(items) {
    return (items || [])
      .slice()
      .sort((a, b) => (a.id || "").localeCompare(b.id || ""))
      .map(canonicalSchedule)
      .join(";");
  }

  function canonicalHeartbeat(heartbeat) {
    const timer = heartbeat.timer ? "1" : "0";
    if ((heartbeat.protocol || 0) >= 3) {
      return [heartbeat.host, heartbeat.v, heartbeat.protocol, heartbeat.ts, timer,
        heartbeat.preset || "", canonicalSchedules(heartbeat.schedules)].join("|");
    }
    // Compatibility with clients distributed before protocol 3.
    return [heartbeat.host, heartbeat.v, heartbeat.ts, timer, heartbeat.preset || ""].join("|");
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
    if (age > ROSTER_HISTORY_SECONDS) return false;
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
      if (response.status === 429) {
        setConnection(false, "Limite ntfy");
        throw new Error("O ntfy limitou este IP. Aguarde a liberação ou use outro servidor.");
      }
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
      const online = now - info.lastSeen <= ONLINE_AFTER_SECONDS;
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
      mode.textContent = `${info.timer ? `Automático: ${PRESET_LABELS[info.preset] || info.preset}` : "Automático: desligado"} · v${info.version || "?"}`;

      const actions = document.createElement("div");
      actions.className = "machine-actions";
      const openPad = document.createElement("button");
      openPad.type = "button";
      openPad.className = "machine-action";
      openPad.textContent = "Abrir no Pad";
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
    const onlineCount = sorted.filter(([, info]) => now - info.lastSeen <= ONLINE_AFTER_SECONDS).length;
    els.onlineCount.textContent = String(onlineCount);

    els.target.replaceChildren();
    const allOption = document.createElement("option");
    allOption.value = "all";
    allOption.textContent = "Todas as máquinas";
    els.target.appendChild(allOption);
    for (const [id, info] of sorted) {
      const online = now - info.lastSeen <= ONLINE_AFTER_SECONDS;
      const option = document.createElement("option");
      option.value = hostTopic(id);
      option.textContent = `${id} ${online ? "●" : "◌"}`;
      els.target.appendChild(option);
    }
    if ([...els.target.options].some(option => option.value === current)) els.target.value = current;

    const scheduleCurrent = els.scheduleTarget.value;
    els.scheduleTarget.replaceChildren();
    const scheduleAll = document.createElement("option");
    scheduleAll.value = "all";
    scheduleAll.textContent = "Todas as máquinas";
    els.scheduleTarget.appendChild(scheduleAll);
    for (const [id, info] of sorted) {
      const option = document.createElement("option");
      option.value = hostTopic(id);
      option.textContent = `${id} ${now-info.lastSeen <= ONLINE_AFTER_SECONDS ? "●" : "◌"}`;
      els.scheduleTarget.appendChild(option);
    }
    if ([...els.scheduleTarget.options].some(option => option.value === scheduleCurrent)) {
      els.scheduleTarget.value = scheduleCurrent;
    }

    renderPresetState();
    renderMachines();
    renderSchedules();
  }

  function scheduleActionLabel(item) {
    if (item.sound) return item.sound;
    if (item.category) return CATEGORY_LABELS[item.category] || item.category;
    return "Surpresa";
  }

  function scheduleTimeLabel(item) {
    const formatter = new Intl.DateTimeFormat("pt-BR", {
      dateStyle: "short", timeStyle: "short", timeZone: item.timezone || undefined
    });
    if (item.kind === "once") {
      return item.nextRun ? formatter.format(new Date(item.nextRun * 1000)) : "Concluído";
    }
    if (item.kind === "weekly") {
      return item.nextRun ? `Semanal · ${formatter.format(new Date(item.nextRun * 1000))}` : "Sem próxima execução";
    }
    return item.nextRun ? `Janela aleatória · ${formatter.format(new Date(item.nextRun * 1000))}` : "Sem próxima execução";
  }

  function renderSchedules() {
    if (!els.scheduleList) return;
    els.scheduleList.replaceChildren();
    const entries = [];
    for (const [host, info] of hosts) {
      for (const item of info.schedules || []) {
        if (item.enabled) entries.push({ host, item });
      }
    }
    entries.sort((a, b) => (a.item.nextRun || Number.MAX_SAFE_INTEGER) - (b.item.nextRun || Number.MAX_SAFE_INTEGER));
    if (!entries.length) {
      const empty = document.createElement("div");
      empty.className = "empty-state";
      empty.textContent = "Nenhum agendamento ativo foi informado pelas máquinas.";
      els.scheduleList.appendChild(empty);
      return;
    }
    for (const { host, item } of entries) {
      const card = document.createElement("article");
      card.className = "schedule-card";
      const head = document.createElement("div");
      head.className = "schedule-card-head";
      const text = document.createElement("div");
      text.className = "schedule-card-title";
      text.textContent = scheduleActionLabel(item);
      const machine = document.createElement("span");
      machine.className = "schedule-card-machine";
      machine.textContent = host;
      const remove = document.createElement("button");
      remove.type = "button";
      remove.className = "schedule-delete";
      remove.textContent = "Excluir";
      remove.addEventListener("click", async () => {
        if (!confirm(`Excluir este agendamento de “${host}”?`)) return;
        await send(
          { cmd: "schedule.delete", scheduleId: item.id },
          { explicitTarget: hostTopic(host), explicitLabel: host }
        );
      });
      const meta = document.createElement("p");
      meta.className = "schedule-card-meta";
      meta.textContent = scheduleTimeLabel(item);
      head.append(text, machine, remove);
      card.append(head, meta);
      els.scheduleList.appendChild(card);
    }
  }

  function setupScheduleForm() {
    const weekdays = ["D", "S", "T", "Q", "Q", "S", "S"];
    document.querySelectorAll("[data-weekday-group]").forEach(group => {
      weekdays.forEach((label, day) => {
        const wrapper = document.createElement("label");
        wrapper.className = "weekday-option";
        const input = document.createElement("input");
        input.type = "checkbox";
        input.value = String(day);
        input.checked = day >= 1 && day <= 5;
        const visual = document.createElement("span");
        visual.textContent = label;
        wrapper.append(input, visual);
        group.appendChild(wrapper);
      });
    });

    const options = [
      ["random", "Surpresa"],
      ["category:subtle", "Categoria · Sutil"],
      ["category:punchy", "Categoria · Médio"],
      ["category:scare", "Categoria · Susto"]
    ];
    for (const [category, names] of Object.entries(SOUNDS)) {
      for (const name of names) options.push([`sound:${name}`, `${CATEGORY_LABELS[category]} · ${name}`]);
    }
    for (const [value, label] of options) {
      const option = document.createElement("option");
      option.value = value;
      option.textContent = label;
      els.scheduleAction.appendChild(option);
    }

    const defaultAt = new Date(Date.now() + 60 * 60 * 1000);
    defaultAt.setSeconds(0, 0);
    const offset = defaultAt.getTimezoneOffset() * 60 * 1000;
    els.scheduleAt.value = new Date(defaultAt.getTime()-offset).toISOString().slice(0, 16);
    els.timezoneNote.textContent = `Fuso: ${Intl.DateTimeFormat().resolvedOptions().timeZone}`;
  }

  function selectedWeekdays(kind) {
    return [...document.querySelectorAll(`[data-weekday-group="${kind}"] input:checked`)]
      .map(input => Number(input.value));
  }

  function buildScheduleFromForm() {
    const kind = els.scheduleKind.value;
    const action = els.scheduleAction.value;
    const item = {
      id: "",
      kind,
      sound: "",
      category: "",
      random: action === "random",
      at: "",
      weekdays: [],
      clock: "",
      windowStart: "",
      windowEnd: "",
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "America/Sao_Paulo",
      enabled: true,
      nextRun: 0,
      lastRun: 0
    };
    if (action.startsWith("sound:")) item.sound = action.slice(6);
    if (action.startsWith("category:")) item.category = action.slice(9);
    if (kind === "once") {
      const date = new Date(els.scheduleAt.value);
      if (Number.isNaN(date.getTime()) || date <= new Date()) throw new Error("Escolha uma data futura.");
      item.at = date.toISOString();
    } else if (kind === "weekly") {
      item.weekdays = selectedWeekdays("weekly");
      item.clock = els.scheduleClock.value;
      if (!item.weekdays.length || !item.clock) throw new Error("Escolha dias e horário.");
    } else {
      item.weekdays = selectedWeekdays("window");
      item.windowStart = els.windowStart.value;
      item.windowEnd = els.windowEnd.value;
      if (!item.weekdays.length || !item.windowStart || !item.windowEnd || item.windowStart >= item.windowEnd) {
        throw new Error("Confira os dias e a janela de horário.");
      }
    }
    return item;
  }

  function rosterStorageKey() {
    return config ? `${STORAGE_KEY}.roster.${config.prefix}` : "";
  }

  function saveRosterCache() {
    if (!config) return;
    localStorage.setItem(rosterStorageKey(), JSON.stringify(Object.fromEntries(hosts)));
  }

  function loadRosterCache() {
    hosts.clear();
    if (!config) return;
    try {
      const cached = JSON.parse(localStorage.getItem(rosterStorageKey()) || "{}");
      const now = Math.floor(Date.now() / 1000);
      for (const [host, info] of Object.entries(cached)) {
        if (info?.lastSeen && now-info.lastSeen <= ROSTER_MEMORY_SECONDS) hosts.set(host, info);
      }
    } catch (_) {}
  }

  async function acceptHeartbeat(payload, render = true) {
    if (!(await verifyHeartbeat(payload))) return false;
    hosts.set(payload.host, {
      lastSeen: payload.ts,
      timer: Boolean(payload.timer),
      preset: payload.preset || "discreto",
      version: payload.v,
      protocol: payload.protocol || 0,
      schedules: Array.isArray(payload.schedules) ? payload.schedules : []
    });
    saveRosterCache();
    if (render) renderHosts();
    return true;
  }

  async function processRosterMessage(event) {
    try {
      const envelope = JSON.parse(event.data);
      if (envelope.event !== "message") return;
      await acceptHeartbeat(JSON.parse(envelope.message));
    } catch (_) {}
  }

  async function loadRecentRoster() {
    const topic = encodeURIComponent(config.prefix + "-roster");
    const response = await fetch(`${config.baseUrl}/${topic}/json?poll=1&since=12h`, {
      headers: { "Accept": "application/x-ndjson" }
    });
    if (response.status === 429) {
      setConnection(false, "Limite ntfy");
      throw new Error("O ntfy limitou este IP. As máquinas voltarão após a liberação.");
    }
    if (!response.ok) throw new Error(`Não foi possível carregar as máquinas (${response.status}).`);

    const lines = (await response.text()).split("\n").filter(Boolean);
    for (const line of lines) {
      try {
        const envelope = JSON.parse(line);
        if (envelope.event !== "message") continue;
        await acceptHeartbeat(JSON.parse(envelope.message), false);
      } catch (_) {}
    }
    renderHosts();
  }

  async function refreshPresence(manual = false) {
    const pingKey = `${STORAGE_KEY}.lastPing.${config.prefix}`;
    const now = Date.now();
    const lastPing = Number(localStorage.getItem(pingKey) || 0);
    if (!manual && now-lastPing < MIN_PING_INTERVAL_MS) return;
    localStorage.setItem(pingKey, String(now));
    const ok = await send({ cmd: "ping" }, { silent: !manual, forceAll: true });
    if (manual && ok) showToast("Atualização solicitada. Aguarde alguns segundos.");
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
    eventSource.addEventListener("open", async () => {
      setConnection(true);
      try {
        await loadRecentRoster();
        await refreshPresence(false);
      } catch (error) {
        showToast(error.message, "error");
      }
    });
    eventSource.addEventListener("message", processRosterMessage);
    eventSource.addEventListener("error", () => {
      setConnection(false, "Reconectando");
      eventSource?.close();
      reconnectTimer = setTimeout(connect, 15000);
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
  els.agendaTab.addEventListener("click", () => {
    switchView("agenda");
    const now = Date.now();
    if (config && now-lastScheduleRefresh >= MIN_PING_INTERVAL_MS) {
      lastScheduleRefresh = now;
      send({ cmd: "schedule.list" }, { silent: true, forceAll: true });
    }
  });
  els.scheduleKind.addEventListener("change", () => {
    document.querySelectorAll("[data-kind-fields]").forEach(group => {
      group.hidden = group.dataset.kindFields !== els.scheduleKind.value;
    });
  });
  els.scheduleForm.addEventListener("submit", async event => {
    event.preventDefault();
    if (!config) {
      openSettings();
      return;
    }
    try {
      const schedule = buildScheduleFromForm();
      const destination = els.scheduleTarget.value;
      if (!destination) throw new Error("Escolha uma máquina.");
      const label = destination === "all" ? "Todas as máquinas" : els.scheduleTarget.selectedOptions[0].textContent.replace(/\s+[●◌]$/, "");
      const sent = await send(
        { cmd: "schedule.upsert", schedule },
        { explicitTarget: destination, explicitLabel: label }
      );
      if (sent) {
        showToast("Agendamento enviado. A lista atualizará após a confirmação da máquina.");
      }
    } catch (error) {
      showToast(error.message, "error");
    }
  });
  els.refreshMachines.addEventListener("click", async () => {
    if (!config) {
      openSettings();
      return;
    }
    try {
      await loadRecentRoster();
      await refreshPresence(true);
    } catch (error) {
      showToast(error.message, "error");
    }
  });
  els.settingsForm.addEventListener("submit", async event => {
    event.preventDefault();
    const baseUrl = els.baseUrlInput.value.trim();
    const key = els.keyInput.value.trim().toLowerCase();
    if (!baseUrl || !/^[0-9a-f]{64}$/.test(key)) {
      showToast("A chave deve ter exatamente 64 caracteres hexadecimais.", "error");
      return;
    }
    await saveConfig({ baseUrl, key });
    loadRosterCache();
    renderHosts();
    els.settingsDialog.close();
    connect();
    showToast("Grupo conectado.");
  });
  els.copyInviteButton.addEventListener("click", copyInvite);
  els.clearConfigButton.addEventListener("click", () => {
    if (!confirm("Apagar a chave salva neste aparelho?")) return;
    const cacheKey = rosterStorageKey();
    localStorage.removeItem(STORAGE_KEY);
    if (cacheKey) localStorage.removeItem(cacheKey);
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
    setupScheduleForm();
    renderActivity();
    config = loadConfig();
    const imported = await importInviteFragment();
    if (imported) config = loadConfig();
    loadRosterCache();
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
