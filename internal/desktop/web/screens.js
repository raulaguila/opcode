// The dialogs and cards beyond the conversation: palette, models, sessions,
// the agent's question, the plan, settings, connections and their form.
"use strict";

// ---- dialog frame ---------------------------------------------------------

let dialog = null; // { close, key(event) → handled }

function openDialog({ title, meta = "", size = "", body, keys = [], onKey, list = null }) {
  closeDialog();
  const metaNode = el("span", { class: "edge right" }, meta);
  const keysNode = el("div", { class: "keys" });
  const panel = el("div", { class: `dlg ${size}`, role: "dialog", "aria-label": title },
    el("span", { class: "edge left" }, title), metaNode, el("div", { class: "dlg-body" }, body), keysNode);
  const veil = el("div", { class: "veil", onmousedown: (e) => { if (e.target === veil) closeDialog(); } }, panel);
  document.body.append(veil);
  $("input").blur();
  // Keys typed right after opening belong to the dialog's first field.
  panel.querySelector("input:not([type=radio]), select, textarea")?.focus();
  dialog = {
    veil, panel,
    meta: (text) => { metaNode.textContent = text; },
    keys: (pairs) => {
      keysNode.replaceChildren(...pairs.map(([k, d, end, cls]) => el("span", { class: [end ? "end" : "", cls || ""].join(" ") }, el("b", {}, k), d ? " " + d : "")));
    },
    key: onKey,
    list,
    close: () => veil.remove(),
  };
  dialog.keys(keys);
  return dialog;
}

function closeDialog() {
  if (dialog) dialog.close();
  dialog = null;
  $("input").focus();
}

// A searchable list with groups, a selection bar and optional marks.
function makeList({ items, grouped = true, onPick, onMove, cursor = "", chips = false, empty }) {
  const node = el("div", { class: "list" });
  const state = { query: "", selected: 0, items };
  const visible = () => {
    const q = state.query.trim().toLowerCase();
    if (!q) return state.items;
    const title = state.items.filter((i) => i.title.toLowerCase().includes(q));
    const rest = state.items.filter((i) => !i.title.toLowerCase().includes(q) && `${i.desc || ""} ${i.id || ""}`.toLowerCase().includes(q));
    return [...title, ...rest];
  };
  const marks = () => !!cursor || state.items.some((i) => i.mark);
  const label = (text) => {
    const q = state.query.trim();
    const at = q ? text.toLowerCase().indexOf(q.toLowerCase()) : -1;
    if (at < 0) return text;
    return [text.slice(0, at), el("u", {}, text.slice(at, at + q.length)), text.slice(at + q.length)];
  };
  const draw = () => {
    const list = visible();
    state.selected = Math.min(state.selected, Math.max(0, list.length - 1));
    node.replaceChildren();
    if (!list.length) { node.append(el("div", { class: "nothing" }, empty || t("Nada encontrado", "Nothing found"))); return; }
    let group = null;
    list.forEach((item, index) => {
      if (grouped && !state.query.trim() && item.group && item.group !== group) {
        group = item.group;
        node.append(el("div", { class: "grp" }, group));
      }
      const selected = index === state.selected;
      let mark = null;
      if (item.mark) mark = el("span", { class: "mk", style: `color:${item.markColor || "var(--ok)"}` }, item.mark);
      else if (selected && cursor) mark = el("span", { class: "mk", style: "color:var(--ask)" }, cursor);
      const right = item.right ? el("span", { class: chips ? "kbd" : "r" }, item.right) : null;
      const row = el("div", { class: `item${selected ? " sel" : ""}${mark || (selected && marks()) ? " marked" : ""}${item.faint ? " faint" : ""}`, onclick: () => { state.selected = index; draw(); onMove?.(current()); onPick?.(item); } },
        mark, el("span", { class: "t" }, label(item.title)), item.desc ? el("span", { class: "d" }, item.desc) : null, right);
      if (!mark && marks() && !selected) row.style.paddingLeft = "43px";
      node.append(row);
      if (selected) requestAnimationFrame(() => row.scrollIntoView({ block: "nearest" }));
    });
  };
  const current = () => visible()[state.selected];
  const api_ = {
    node, draw, current, visible,
    set items(list) { state.items = list; draw(); },
    get query() { return state.query; },
    setQuery(q) { state.query = q; state.selected = 0; draw(); onMove?.(current()); },
    move(step) {
      const n = visible().length;
      if (!n) return;
      state.selected = (state.selected + step + n) % n;
      draw();
      onMove?.(current());
    },
    select(predicate) {
      const index = visible().findIndex(predicate);
      if (index >= 0) { state.selected = index; draw(); onMove?.(current()); }
    },
  };
  draw();
  return api_;
}

function searchField(placeholder, list, prompt = "⌕") {
  const input = el("input", { type: "text", placeholder, "aria-label": placeholder, autocomplete: "off", spellcheck: "false" });
  input.addEventListener("input", () => list.setQuery(input.value));
  return { node: el("label", { class: "search" }, el("span", {}, prompt), input), input };
}

// ---- palette ----------------------------------------------------------------

function commands() {
  const main = t("Principais", "Main"), work = t("Trabalho", "Work"), settings = t("Configurações", "Settings");
  return [
    { title: t("Nova conversa", "New conversation"), desc: t("começar do zero", "start fresh"), right: "Ctrl+N", group: main, run: newConversation },
    { title: t("Modelo", "Model"), desc: t("escolher modelo e provedor", "choose model and provider"), right: "Ctrl+O", group: main, run: openModels },
    { title: t("Conversas", "Conversations"), desc: t("abrir, renomear, arquivar", "open, rename, archive"), right: "Ctrl+S", group: main, run: openSessions },
    { title: t("Configurações", "Settings"), desc: t("conexões e preferências", "connections and preferences"), right: "/settings", group: main, run: () => openSettings(0) },
    { title: t("Modo", "Mode"), desc: t("Perguntar, Planejar ou Executar", "Ask, Plan or Execute"), right: "Shift+Tab", group: work, run: cycleMode },
    { title: t("Perguntar", "Ask"), desc: t("lê e explica, sem alterar", "reads and explains, no changes"), right: "/ask", group: work, run: () => setMode("ask") },
    { title: t("Planejar", "Plan"), desc: t("prepara um plano, sem editar", "prepares a plan, no edits"), right: "/plan", group: work, run: () => setMode("plan") },
    { title: t("Executar", "Execute"), desc: t("altera arquivos com aprovação", "changes files with approval"), right: "/execute", group: work, run: () => setMode("execute") },
    { title: t("Tarefa", "Task"), desc: t("mostrar ou recolher o plano", "show or fold the plan"), right: "/task", group: work, run: togglePlan },
    { title: t("Retomar tarefa", "Resume task"), desc: t("abre a última conversa e o plano salvo", "opens the last conversation and its saved plan"), right: "/resume", group: work, run: resumeLast },
    { title: t("Responder pergunta", "Answer question"), desc: t("reabre a pergunta pendente do agente", "reopens the agent's pending question"), right: "Enter", group: work, run: () => { view.laterQuestion = ""; render(); } },
    { title: t("Cancelar execução", "Cancel run"), desc: t("para a tarefa em andamento", "stops the running task"), right: "Esc", group: work, run: () => view.session && api(`/api/sessions/${view.session}/cancel`, {}).catch(() => {}) },
    { title: t("Exportar conversa", "Export conversation"), desc: t("salva em Markdown", "saves as Markdown"), right: "/export", group: t("Conversa", "Conversation"), run: exportCurrent },
    { title: t("Conexões", "Connections"), desc: t("provedores e chaves", "providers and keys"), right: "/connections", group: settings, run: () => openSettings(0) },
    { title: t("Preferências", "Preferences"), desc: t("idioma, notificações, modo inicial", "language, notifications, initial mode"), right: "/preferences", group: settings, run: () => openSettings(1) },
    { title: t("Aprovação", "Approval"), desc: t("pedir · editar no projeto", "ask · project edits"), right: "/approval", group: settings, run: () => openSettings(3) },
    { title: t("Atalhos", "Shortcuts"), desc: t("teclas dos comandos", "command keys"), right: "/keys", group: settings, run: () => openSettings(4) },
  ];
}

// Recent commands lead the palette; the list lives only in this window's browser.
function recent() {
  try { return JSON.parse(localStorage.getItem("opcode-recent") || "[]"); } catch { return []; }
}
function remember(title) {
  try { localStorage.setItem("opcode-recent", JSON.stringify([title, ...recent().filter((r) => r !== title)].slice(0, 3))); } catch {}
}
async function resumeLast() {
  const list = await api("/api/conversations").catch(() => []);
  const last = list.find((s) => !s.archived && s.id !== view.session) || list[0];
  if (!last) return;
  view.session = last.id; view.raw = ""; view.showPlan = (await api(`/api/sessions/${last.id}`)).task?.steps?.length > 0;
  await loadConversation();
}
async function exportCurrent() {
  if (!view.session) { view.error = t("Nada para exportar ainda", "Nothing to export yet"); renderFrame(); return; }
  try { const r = await api(`/api/sessions/${view.session}/export`, {}); view.error = ""; view.info = r.path; flash(`${t("Exportada", "Exported")}: ${r.path}`); }
  catch (error) { view.error = error.message; renderFrame(); }
}
function flash(text) {
  const a = $("activity");
  a.textContent = text; a.className = ""; a.style.color = "var(--ok)";
  setTimeout(() => renderFrame(), 4000);
}

function openPalette(query = "") {
  const all = commands();
  const recents = recent().map((title) => all.find((c) => c.title === title)).filter(Boolean);
  const items = [...recents.map((c) => ({ ...c, group: t("Recentes", "Recent") })), ...all.filter((c) => !recents.includes(c))];
  const run = (item) => { closeDialog(); remember(item.title); item.run(); };
  const list = makeList({ items, cursor: "◆", chips: true, onPick: run });
  const search = searchField(t("Buscar comando", "Search commands"), list, "/");
  search.input.value = query;
  list.setQuery(query);
  const d = openDialog({
    title: t("Comandos", "Commands"), meta: String(list.visible().length), size: "", list,
    body: el("div", {}, search.node, list.node),
    keys: [["↑↓", t("escolher", "choose")], ["Enter", t("abrir", "open")], ["Esc", t("fechar", "close"), true]],
    onKey: (event) => {
      if (event.key === "Enter") { const item = list.current(); if (item) run(item); return true; }
      return false;
    },
  });
  search.input.addEventListener("input", () => d.meta(String(list.visible().length)));
  d.panel.style.width = "min(700px, 100%)";
}

function setMode(mode) { view.mode = mode; renderFrame(); }
function togglePlan() {
  const steps = view.conversation?.task?.steps || [];
  if (!steps.length) { view.error = t("Sem plano ainda · peça um em Planejar", "No plan yet · ask for one in Plan"); renderFrame(); return; }
  view.showPlan = !view.showPlan;
  renderWork();
}
function newConversation() {
  view.session = ""; view.conversation = null; view.raw = ""; view.error = ""; view.showPlan = false;
  render();
}

// ---- models -----------------------------------------------------------------

async function openModels(provider = "") {
  let data = { providers: [], models: [] };
  let details = false;
  // Which job the chosen model does; Ctrl+A cycles it, as in the terminal.
  const roles = ["coder", "task", "title", "summarizer"];
  const roleName = (r) => ({ task: t("tarefas", "tasks"), title: t("títulos", "titles"), summarizer: t("resumos", "summaries") })[r] || "";
  let role = "coder";
  const tabs = el("div", { class: "tabs" });
  const status = el("div", { class: "nothing" }, t("Carregando…", "Loading…"));
  const pane = el("div", { class: "pane" });
  const list = makeList({ items: [], onPick: (item) => use(item.model), onMove: () => drawPane() });
  const search = searchField(t("Buscar por nome ou ID", "Search by name or ID"), list);
  const wrap = el("div", {});
  const d = openDialog({
    title: t("Escolher modelo", "Choose model"), body: wrap, list,
    onKey: (event) => {
      // With nothing typed, ←→ walk the provider tabs.
      if (details && event.key === "ArrowRight") { event.preventDefault(); pane.querySelector("button")?.focus(); return true; }
      if (!details && (event.key === "ArrowLeft" || event.key === "ArrowRight") && !search.input.value) { event.preventDefault(); switchProvider(event.key === "ArrowLeft" ? -1 : 1); return true; }
      if (event.key === "Enter") { const item = list.current(); if (item) use(item.model); return true; }
      if (event.key === "Tab") { event.preventDefault(); switchProvider(event.shiftKey ? -1 : 1); return true; }
      if (event.ctrlKey && event.key.toLowerCase() === "d") { event.preventDefault(); details = !details; layout(); return true; }
      if (event.ctrlKey && event.key.toLowerCase() === "p") { event.preventDefault(); openSettings(0); return true; }
      if (event.ctrlKey && event.key.toLowerCase() === "a") { event.preventDefault(); role = roles[(roles.indexOf(role) + 1) % roles.length]; load(data.provider); return true; }
      return false;
    },
  });
  const facts = (m) => {
    const out = [];
    if (m.context) out.push((m.estimated ? "~" : "") + compact(m.context));
    if (m.tools) out.push(t("ferramentas", "tools"));
    if (m.images) out.push(t("imagens", "images"));
    if (m.id === data.active) out.push(t("em uso", "in use"));
    return out.join(" · ");
  };
  const fill = () => {
    const local = t("Locais", "Local"), cloud = t("Nuvem", "Cloud");
    let models = data.models.filter((m) => details || !m.embedding);
    models.sort((a, b) => (a.embedding - b.embedding) || (a.id === data.active ? -1 : b.id === data.active ? 1 : 0) || (a.cloud - b.cloud) || a.label.localeCompare(b.label));
    // The details sheet lists plainly, as the proposal draws it.
    list.items = models.map((m) => ({ id: m.id, title: m.label, model: m, group: details ? "" : m.cloud ? cloud : local, mark: m.id === data.active ? "✓" : "", faint: m.embedding, right: details ? (m.embedding ? "embeddings" : "") : facts(m) }));
    list.select((item) => item.id === data.active);
  };
  const drawTabs = () => {
    tabs.replaceChildren(...data.providers.map((p) => el("button", { type: "button", class: `tab${p.id === data.provider ? " on" : ""}`, onclick: () => load(p.id) }, p.name)));
  };
  const name = () => data.providers.find((p) => p.id === data.provider);
  const drawPane = () => {
    if (!details) return;
    const item = list.current();
    pane.replaceChildren();
    if (!item) return;
    const m = item.model;
    const tags = [];
    if (m.embedding) tags.push(el("span", { class: "tag", style: "color:var(--faint)" }, "embeddings"));
    else if (m.tools) tags.push(el("span", { class: "tag", style: "color:var(--ok);border-color:#2b4a3c" }, t("ferramentas", "tools")));
    if (m.reasoning) tags.push(el("span", { class: "tag", style: "color:var(--info)" }, t("raciocínio", "reasoning")));
    tags.push(m.images ? el("span", { class: "tag", style: "color:var(--user)" }, t("imagens", "images")) : el("span", { class: "tag", style: "color:var(--faint)" }, t("sem imagens", "no images")));
    const kv = el("dl", { class: "kv" });
    const pair = (k, v) => kv.append(el("dt", {}, k), el("dd", {}, v));
    const inUse = data.loaded?.[m.label]?.context;
    pair(t("Contexto", "Context"), inUse && m.context && inUse !== m.context
      ? `${compact(inUse)} ${t("em uso", "in use")} · ${compact(m.context)} ${t("suportado", "supported")}`
      : m.context ? `${m.estimated ? "~" : ""}${compact(m.context)}${m.id === data.active ? t(" · em uso", " · in use") : ""}` : t("não informado", "not reported"));
    if (m.output) pair(t("Saída", "Output"), `${m.output} ${t("tokens por resposta", "tokens per reply")}`);
    const held = data.loaded?.[m.label];
    if (data.loaded && !m.cloud) {
      pair(t("Carregado", "Loaded"), held ? `● ${t("sim", "yes")} · ${held.placement} · ${(held.bytes / 1e9).toFixed(1)} GB` : t("não", "no"));
    }
    pair(t("Execução", "Runs"), m.cloud ? t("nuvem", "cloud") : t("local", "local"));
    if (m.id === data.active && view.conversation?.speed) pair(t("Velocidade", "Speed"), `≈ ${Math.round(view.conversation.speed)} t/s ${t("na última resposta", "on the last reply")}`);
    if (m.api) pair("API", m.api);
    pane.append(el("div", {}, el("h3", {}, m.label), el("div", { style: "margin-top:6px" }, tags)), kv);
    if (m.embedding) pane.append(el("div", { class: "tip" }, t("Gera embeddings; não serve para conversar.", "Makes embeddings; not for chat.")));
    else {
      const actions = el("div", { class: "row" }, el("button", { type: "button", class: "act primary", onclick: () => use(m) }, t("Usar este modelo", "Use this model")));
      // The knobs belong to the model in use.
      if (m.id === data.active && role === "coder") {
        const windows = [0, 8192, 16384, 32768, 65536, 131072];
        const ctx = data.context || {};
        const ctxSelect = el("select", { class: "act knob", "aria-label": t("Contexto", "Context"), onchange: (e) => knob("/api/model/context", { window: Number(e.target.value) }) },
          windows.map((w) => { const o = el("option", { value: String(w) }, `${t("Contexto", "Context")}: ${w ? compact(w) : t("automático", "automatic")}`); if ((ctx.auto ? 0 : ctx.window) === w) o.selected = true; return o; }));
        if (!ctx.auto && !windows.includes(ctx.window)) ctxSelect.prepend(el("option", { value: String(ctx.window), selected: "" }, `${t("Contexto", "Context")}: ${compact(ctx.window)}`));
        const reasoning = data.reasoning || { choices: [] };
        const effortName = (e) => ({ low: t("baixo", "low"), medium: t("médio", "medium"), high: t("alto", "high"), minimal: t("mínimo", "minimal"), none: t("nenhum", "none") })[e] || e;
        const reasonSelect = el("select", { class: "act knob", "aria-label": t("Raciocínio", "Reasoning"), disabled: reasoning.choices?.length ? null : "", onchange: (e) => knob("/api/model/reasoning", { effort: e.target.value }) },
          reasoning.choices?.length
            ? reasoning.choices.map((c) => { const o = el("option", { value: c }, `${t("Raciocínio", "Reasoning")}: ${effortName(c)}`); if (c === reasoning.current) o.selected = true; return o; })
            : [el("option", {}, `${t("Raciocínio", "Reasoning")}: ${t("não suportado", "not supported")}`)]);
        actions.append(ctxSelect, reasonSelect);
      }
      pane.append(actions);
    }
  };
  const layout = () => {
    fill();
    const p = name();
    d.panel.className = `dlg${details ? " mid" : ""}`;
    d.panel.querySelector(".edge.left").textContent = [details ? t("Escolher modelo · detalhes", "Choose model · details") : t("Escolher modelo", "Choose model"), roleName(role)].filter(Boolean).join(" · ");
    const chat = data.models.filter((m) => !m.embedding).length;
    d.meta(details ? [p?.name, p?.host].filter(Boolean).join(" · ") : p ? `${p.name} · ${chat} ${t("de", "of")} ${data.models.length}` : "");
    d.keys(details
      ? [["Enter", t("usar", "use")], ["Ctrl+D", t("ocultar detalhes", "hide details")], ["Tab", t("provedor", "provider")], ["Esc", t("fechar", "close"), true]]
      : [["Enter", t("usar", "use")], ["Tab", t("provedor", "provider")], ["Ctrl+A", t("função", "role")], ["Ctrl+D", t("detalhes", "details")], ["Ctrl+P", t("conexão", "connection")], ["Esc", t("fechar", "close"), true]]);
    wrap.replaceChildren();
    if (details) { wrap.append(el("div", { class: "split" }, el("div", { class: "col" }, list.node), pane)); drawPane(); }
    else {
      wrap.append(tabs, search.node);
      if (data.error) wrap.append(el("div", { class: "nothing bad-t" }, `${t("Catálogo indisponível", "Catalog unavailable")}: ${data.error}`));
      wrap.append(list.node);
    }
    if (!data.providers.length) wrap.append(el("div", { class: "nothing" }, t("Nenhuma conexão ativa · Ctrl+P configura uma", "No active connection · Ctrl+P sets one up")));
  };
  const load = async (id) => {
    wrap.replaceChildren(status);
    try { data = await api(`/api/models?role=${role}${id ? "&provider=" + encodeURIComponent(id) : ""}`); }
    catch (error) { data = { providers: [], models: [], error: error.message }; }
    drawTabs();
    search.input.value = "";
    list.setQuery("");
    layout();
  };
  // knob changes the model in use, then reads the picker again.
  const knob = async (path, body) => {
    try { await api(path, body); await loadState(); renderFrame(); await load(data.provider); details = true; layout(); }
    catch (error) { pane.append(el("div", { class: "desc bad-t" }, error.message)); }
  };
  const switchProvider = (step) => {
    const ids = data.providers.map((p) => p.id);
    if (ids.length < 2) return;
    load(ids[(ids.indexOf(data.provider) + step + ids.length) % ids.length]);
  };
  const use = async (m) => {
    if (m.embedding) return;
    try {
      const chosen = await api("/api/model", { provider: m.provider, id: m.id, role });
      if (role === "coder") { view.state.model = chosen.label; view.state.context = chosen.context; }
      closeDialog();
      renderFrame();
    } catch (error) { status.textContent = error.message; wrap.prepend(el("div", { class: "nothing bad-t" }, error.message)); }
  };
  await load(provider);
}

function compact(n) {
  if (n >= 1024 && n % 1024 === 0) return n / 1024 + "k";
  if (n >= 1000) return Math.round(n / 1000) + "k";
  return String(n);
}

// ---- conversations ----------------------------------------------------------

function day(updated) {
  const now = new Date(); const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime() / 1000;
  if (!updated) return t("Sem data", "Undated");
  if (updated >= today) return t("Hoje", "Today");
  if (updated >= today - 86400) return t("Ontem", "Yesterday");
  if (updated >= today - 7 * 86400) return t("Esta semana", "This week");
  return t("Antes", "Earlier");
}
function ago(updated) {
  const s = Date.now() / 1000 - updated;
  if (!updated) return "";
  if (s < 60) return t("agora", "now");
  if (s < 3600) return Math.floor(s / 60) + " min";
  if (s < 86400) return Math.floor(s / 3600) + " h";
  return new Date(updated * 1000).toLocaleDateString(view.lang === "pt" ? "pt-BR" : "en", { day: "2-digit", month: "2-digit" });
}

async function openSessions() {
  let all = [], archived = false, renaming = false, info = "";
  const previews = new Map();
  const pane = el("div", { class: "pane" });
  const list = makeList({ items: [], onPick: (item) => open(item.session), onMove: () => drawPane() });
  const search = searchField(t("Buscar conversa", "Search conversations"), list);
  const d = openDialog({
    title: t("Conversas", "Conversations"), size: "wide", list,
    body: el("div", { class: "split sessions" }, el("div", { class: "col" }, search.node, list.node), pane),
    keys: [["Enter", t("abrir", "open")], ["Ctrl+R", t("renomear", "rename")], ["Ctrl+E", t("exportar", "export")], ["Ctrl+A", t("arquivar", "archive")], ["Ctrl+H", t("arquivadas", "archived")], ["Esc", t("fechar", "close"), true]],
    onKey: (event) => {
      const item = list.current();
      if (renaming) return false;
      // → steps from the list into the conversation's actions; ← steps back.
      if (event.key === "ArrowRight" && document.activeElement?.tagName !== "INPUT") { event.preventDefault(); pane.querySelector("button")?.focus(); return true; }
      if (event.key === "Enter" && item) { open(item.session); return true; }
      if (!event.ctrlKey) return false;
      const k = event.key.toLowerCase();
      if (k === "r" && item) { event.preventDefault(); renaming = true; drawPane(); return true; }
      if (k === "e" && item) { event.preventDefault(); exportIt(item.session); return true; }
      if (k === "a" && item) { event.preventDefault(); archiveIt(item.session); return true; }
      if (k === "h") { event.preventDefault(); archived = !archived; fill(); return true; }
      return false;
    },
  });
  const fill = () => {
    const shown = all.filter((s) => s.archived === archived);
    list.items = shown.map((s) => ({ id: s.id, title: s.title, right: ago(s.updated_at), group: day(s.updated_at), session: s,
      mark: s.archived ? "○" : s.busy ? "●" : s.failed ? "■" : s.planning ? "◐" : "✓",
      markColor: s.archived ? "var(--faint)" : s.busy ? "var(--execute)" : s.failed ? "var(--danger)" : s.planning ? "var(--plan)" : "var(--ok)" }));
    list.select((item) => item.id === view.session);
    const hidden = all.filter((s) => s.archived).length;
    d.meta(archived ? `${shown.length} ${t("arquivadas", "archived")}` : hidden ? `${shown.length} · ${hidden} ${t("arquivadas", "archived")}` : String(shown.length));
    drawPane();
  };
  const drawPane = async () => {
    const item = list.current();
    pane.replaceChildren();
    if (!item) return;
    const s = item.session;
    const title = renaming ? el("input", { class: "rename", value: s.title, "aria-label": t("Novo título", "New title") }) : el("h3", {}, s.title);
    if (renaming) {
      title.addEventListener("keydown", (event) => {
        if (event.key === "Enter") { event.preventDefault(); renameIt(s, title.value); }
        if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); renaming = false; drawPane(); }
      });
      requestAnimationFrame(() => { title.focus(); title.select(); });
    }
    if (!previews.has(s.id)) previews.set(s.id, api(`/api/sessions/${s.id}/preview`).catch(() => ({})));
    const preview = await previews.get(s.id);
    if (list.current()?.id !== s.id) return;
    // "Executar · nemotron · 14 mensagens · plano 2/5"
    const facts = [];
    if (preview.mode) facts.push(modeLabel(preview.mode));
    if (preview.model) facts.push(preview.model);
    facts.push(`${s.message_count} ${t("mensagens", "messages")}`);
    if (preview.plan_total) facts.push(`${t("plano", "plan")} ${preview.plan_done}/${preview.plan_total}`);
    if (s.cost > 0) facts.push(`$${s.cost.toFixed(2)}`);
    if (s.archived) facts.push(t("arquivada", "archived"));
    pane.append(el("div", {}, title, el("div", { class: "desc" }, facts.join(" · "))));
    if (preview.prompt) pane.append(el("div", { class: "quote" }, preview.prompt.slice(0, 400)));
    if (preview.steps?.length) pane.append(el("div", { class: "steps" }, stepRows(preview.steps).slice(-4)));
    if (preview.reply) pane.append(el("div", { class: "quote reply-q" }, preview.reply.replace(/[*#`]/g, "").slice(0, 500)));
    if (info) pane.append(el("div", { class: "desc ok-t" }, info));
    const buttons = renaming
      ? [el("button", { type: "button", class: "act primary", onclick: () => renameIt(s, pane.querySelector(".rename").value) }, t("Salvar", "Save")),
         el("button", { type: "button", class: "act", onclick: () => { renaming = false; drawPane(); } }, t("Cancelar", "Cancel"))]
      : [el("button", { type: "button", class: "act primary", onclick: () => open(s) }, t("Abrir", "Open")),
         el("button", { type: "button", class: "act", onclick: () => { renaming = true; drawPane(); } }, t("Renomear", "Rename")),
         el("button", { type: "button", class: "act", onclick: () => exportIt(s) }, t("Exportar", "Export")),
         el("button", { type: "button", class: "act quiet", onclick: () => archiveIt(s) }, s.archived ? t("Restaurar", "Restore") : t("Arquivar", "Archive"))];
    pane.append(el("div", { class: "row", style: "margin-top:auto" }, buttons));
  };
  const reload = async () => { all = await api("/api/conversations"); fill(); };
  const open = async (s) => {
    closeDialog();
    view.session = s.id; view.raw = ""; view.error = ""; view.showPlan = false;
    await loadConversation();
  };
  const renameIt = async (s, value) => {
    try { await api(`/api/sessions/${s.id}/rename`, { title: value }); renaming = false; info = t("Conversa renomeada", "Conversation renamed"); await reload(); if (s.id === view.session) loadConversation(); }
    catch (error) { info = ""; pane.append(el("div", { class: "desc bad-t" }, error.message)); }
  };
  const exportIt = async (s) => {
    try { const r = await api(`/api/sessions/${s.id}/export`, {}); info = `${t("Exportada", "Exported")}: ${r.path}`; drawPane(); }
    catch (error) { pane.append(el("div", { class: "desc bad-t" }, error.message)); }
  };
  const archiveIt = async (s) => {
    try { await api(`/api/sessions/${s.id}/archive`, { archived: !s.archived }); info = s.archived ? t("Conversa restaurada", "Conversation restored") : t("Conversa arquivada", "Conversation archived"); await reload(); }
    catch (error) { pane.append(el("div", { class: "desc bad-t" }, error.message)); }
  };
  await reload();
}

// ---- the agent's question -----------------------------------------------------

const asking = { id: "", index: 0, cursor: 0, checked: new Set(), text: "", answers: [] };

function questionItems(q) {
  return q.questions?.length ? q.questions : [{ id: "", question: q.question, explanation: q.explanation, type: q.choices?.length ? "single_choice" : "text", choices: q.choices || [], notes: q.notes || [] }];
}

function questionCard(q) {
  if (asking.id !== q.id) Object.assign(asking, { id: q.id, index: 0, cursor: 0, checked: new Set(), text: "", answers: [] });
  const items = questionItems(q);
  const item = items[asking.index];
  const multi = item.type === "multi_choice";
  const choices = item.choices || [];
  if (!choices.length) asking.cursor = 0;
  const rows = [];
  // highlight moves the selection bar without redrawing, so typing keeps its focus.
  const highlight = (index) => { asking.cursor = index; rows.forEach((row, i) => row.classList.toggle("sel", i === index)); };
  choices.forEach((choice, i) => rows.push(el("div", { class: `opt${asking.cursor === i ? " sel" : ""}`, onclick: () => { asking.cursor = i; pick(i, multi); renderWork(); } },
    el("span", { class: "num" }, String(i + 1)),
    el("span", { style: `color:${asking.checked.has(i) ? "var(--ok)" : "var(--muted)"}` }, multi ? (asking.checked.has(i) ? "☑" : "☐") : (asking.checked.has(i) ? "●" : "○")),
    el("span", { style: asking.cursor === i ? "color:var(--strong)" : "" }, choice),
    item.notes?.[i] ? el("span", { class: "d" }, item.notes[i]) : null)));
  // Your own answer is the last option: same row, same number, written in place.
  const free = el("input", { type: "text", placeholder: t("Outra resposta…", "Other answer…"), value: asking.text, "aria-label": t("Outra resposta", "Other answer"), id: "free-answer" });
  free.addEventListener("input", () => { asking.text = free.value; highlight(choices.length); });
  free.addEventListener("focus", () => highlight(choices.length));
  free.addEventListener("keydown", (event) => {
    if (event.key === "Enter") { event.preventDefault(); event.stopPropagation(); submitAnswer(q); }
    if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); answerLater(q); return; }
    if (event.key === "ArrowUp" && choices.length) { event.preventDefault(); event.stopPropagation(); free.blur(); highlight(choices.length - 1); }
  });
  rows.push(el("div", { class: `opt free${asking.cursor === choices.length ? " sel" : ""}`, onclick: () => free.focus() },
    el("span", { class: "num" }, String(choices.length + 1)), el("span", { style: "color:var(--muted)" }, "✎"), free));
  const count = choices.length + 1;
  const keys = !choices.length
    ? [[t("Escreva", "Type"), t("sua resposta", "your answer")], ["Enter", t("responder", "answer")]]
    : multi
      ? [[`1–${count}`, t("marcar", "check")], [t("Espaço", "Space"), t("alternar", "toggle")], ["Enter", `${t("responder", "answer")} (${asking.checked.size})`]]
      : [[`1–${count}`, t("escolher", "choose")], ["Enter", t("responder", "answer")]];
  if (items.length > 1 && asking.index + 1 < items.length) keys.push(["→", t("próxima pergunta", "next question")]);
  // A question with no choices is answered by writing: the answer row takes the keyboard.
  if (!choices.length) requestAnimationFrame(() => { if (!dialog && !$("input").value) free.focus(); });
  return el("div", { class: "card-wrap" }, el("div", { class: "card ask" },
    el("span", { class: "edge left" }, t("Pergunta do agente", "Agent question")),
    items.length > 1 ? el("span", { class: "edge right" }, `${asking.index + 1} ${t("de", "of")} ${items.length}`) : null,
    el("div", { class: "qtext" }, item.question),
    item.explanation ? el("div", { class: "qexp" }, item.explanation) : null,
    el("div", { style: "display:flex;flex-direction:column;gap:2px" }, rows),
    el("div", { class: "inkeys" }, keys.map(([k, d]) => el("span", {}, el("b", {}, k), " " + d)), el("span", { class: "end" }, el("b", {}, "Esc"), " " + t("responder depois", "answer later")))));
}

// answerLater puts the card away; the agent keeps waiting, and the footer
// or an Enter in the empty composer brings the card back.
function answerLater(q) {
  view.laterQuestion = q.id;
  renderFrame();
  renderWork();
  $("input").focus();
}

function pick(i, multi) {
  if (multi) { if (asking.checked.has(i)) asking.checked.delete(i); else asking.checked.add(i); }
  else asking.checked = new Set([i]);
}

async function submitAnswer(q) {
  const items = questionItems(q);
  const item = items[asking.index];
  const choices = item.choices || [];
  const answer = { question_id: item.id };
  if (asking.cursor === choices.length || !choices.length) answer.text = asking.text.trim();
  else {
    if (item.type !== "multi_choice") asking.checked = new Set([asking.cursor]);
    answer.choices = choices.filter((_, i) => asking.checked.has(i));
  }
  if (!answer.text && !answer.choices?.length) { view.error = t("Escolha uma opção ou escreva uma resposta", "Choose an option or write an answer"); renderFrame(); return; }
  view.error = "";
  if (!q.questions?.length) {
    await api(`/api/sessions/${view.session}/answer`, { question_id: q.id, text: answer.text || answer.choices[0] }).catch((e) => { view.error = e.message; });
    asking.id = "";
    return loadConversation();
  }
  asking.answers.push(answer);
  if (asking.index + 1 < items.length) {
    Object.assign(asking, { index: asking.index + 1, cursor: 0, checked: new Set(), text: "" });
    renderWork();
    return;
  }
  await api(`/api/sessions/${view.session}/answer`, { question_id: q.id, answers: asking.answers }).catch((e) => { view.error = e.message; });
  asking.id = "";
  loadConversation();
}

// Keys for the question card, when the composer is empty or not focused.
function questionKey(event) {
  const q = view.conversation?.question;
  if (!q) return false;
  if (view.laterQuestion === q.id) {
    if (event.key === "Enter" && document.activeElement === $("input") && !$("input").value) { event.preventDefault(); view.laterQuestion = ""; render(); return true; }
    return false;
  }
  if (event.key === "Escape") { event.preventDefault(); answerLater(q); return true; }
  const active = document.activeElement;
  if (active?.id === "free-answer" || (active?.tagName === "INPUT" && active !== $("input")) || ($("input") === active && $("input").value)) return false;
  if (event.ctrlKey || event.altKey || event.metaKey) return false;
  const items = questionItems(q);
  const item = items[asking.index];
  const choices = item.choices || [];
  const multi = item.type === "multi_choice";
  const freeRow = () => document.getElementById("free-answer");
  const n = Number(event.key);
  if (n >= 1 && n <= choices.length) { event.preventDefault(); asking.cursor = n - 1; pick(n - 1, multi); renderWork(); return true; }
  if (n === choices.length + 1) { event.preventDefault(); freeRow()?.focus(); return true; }
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    event.preventDefault();
    asking.cursor = Math.max(0, Math.min(choices.length, asking.cursor + (event.key === "ArrowDown" ? 1 : -1)));
    renderWork();
    if (asking.cursor === choices.length) freeRow()?.focus();
    return true;
  }
  if (event.key === " " && asking.cursor < choices.length) { event.preventDefault(); pick(asking.cursor, multi); renderWork(); return true; }
  if (event.key === "Enter") { event.preventDefault(); submitAnswer(q); return true; }
  if (event.key === "ArrowRight" && asking.index + 1 < items.length) { event.preventDefault(); submitAnswer(q); return true; }
  // Any other letter starts your own answer, in its row rather than the composer.
  if (event.key.length === 1) { freeRow()?.focus(); return false; }
  return false;
}

// ---- plan card --------------------------------------------------------------

function planCard(task) {
  const steps = task.steps || [];
  const done = steps.filter((s) => s.status === "completed").length;
  return el("div", { class: "card-wrap" }, el("div", { class: "card plan" },
    el("span", { class: "edge left" }, `${t("Plano", "Plan")} · ${done} ${t("de", "of")} ${steps.length} ${t("concluídas", "completed")}`),
    el("span", { class: "edge right" }, `${modeLabel(task.mode || view.mode)} · ${t("tarefa salva", "task saved")}`),
    el("div", { class: "bar", role: "img", "aria-label": `${Math.round((done / Math.max(1, steps.length)) * 100)}%` }, el("div", { style: `width:${(done / Math.max(1, steps.length)) * 100}%` })),
    steps.map((s, i) => {
      const state = s.status === "completed" ? "done" : s.status === "in_progress" ? "now" : "";
      const icon = state === "done" ? el("span", { class: "ok" }, "✓") : state === "now" ? el("span", { style: "color:var(--plan)" }, "●") : el("span", { style: "color:var(--faint)" }, "○");
      return el("div", { class: `step ${state}` }, icon, el("span", {}, `${i + 1}. ${s.title}`), el("span", { class: "desc" }, state === "now" ? t("agora", "now") : ""));
    }),
    el("div", { class: "inkeys", style: "margin:10px 14px 0" },
      task.objective ? el("span", {}, `${t("Resultado esperado", "Expected result")}: ${task.objective}`) : null,
      el("span", { class: "end" }, el("b", {}, "Alt+↑↓"), " " + t("rolar", "scroll") + "  ", el("b", {}, "/task"), " " + t("recolher", "fold")))));
}

// ---- settings ---------------------------------------------------------------

const sections = () => [t("Conexões", "Connections"), t("Interface", "Interface"), t("Trabalho", "Work"), t("Aprovação", "Approval"), t("Atalhos", "Shortcuts"), t("Integrações", "Integrations"), t("Avançado", "Advanced")];
const sectionOf = (s) => s.category === "interface" || s.category === "sessions" ? 1 : s.category === "shortcuts" ? 4 : s.category === "execution" ? (s.advanced ? 6 : 2) : -1;

function options(setting) {
  const help = setting.help.split(" (")[0];
  const list = help.split(", ");
  if (list.length < 2 || list.some((o) => !o || /[ –+]/.test(o))) return [];
  return list;
}
function optionLabel(v) {
  return ({ dark: t("Escuro", "Dark"), light: t("Claro", "Light"), system: t("Sistema", "System"), true: t("Sim", "Yes"), false: t("Não", "No"), pt: "Português", en: "English", auto: "Auto", shown: t("Sempre", "Always"), hidden: t("Nunca", "Never"), off: t("Desligado", "Off"), bell: t("sino", "bell"), ask: modeLabel("ask"), plan: modeLabel("plan"), execute: modeLabel("execute") })[v] || v;
}
const helpText = {
  "app.theme": ["Escuro é o do protótipo; Sistema segue o seu computador", "Dark is the proposal's; System follows your computer"],
  "tui.language": ["Textos da interface e respostas", "Interface copy and replies"],
  "tui.proseWidth": ["Largura máxima das mensagens, em colunas · 0 usa a largura toda", "Widest a message runs, in columns · 0 uses the full width"],
  "tui.sidebar": ["Auto aparece a partir de 140 colunas", "Auto appears from 140 columns"],
  "tui.notifications": ["Ao terminar uma tarefa longa ou pedir aprovação", "When a long task ends or asks for approval"],
  "preferences.showProcess": ["Mostra cada ferramenta numa linha, sem agrupar", "Shows each tool on its own line, ungrouped"],
  "preferences.resumeLast": ["Abre a última conversa ao iniciar", "Opens the last conversation at start"],
  "runtime.mode": ["Modo das novas conversas", "Mode for new conversations"],
  "runtime.maxSteps": ["Etapas por resposta · 1–200", "Steps per reply · 1–200"],
  "runtime.maxSeconds": ["Segundos por resposta · 1–7200", "Seconds per reply · 1–7200"],
  "runtime.toolBudget": ["Bytes de saída por ferramenta · 4096–1048576", "Output bytes per tool · 4096–1048576"],
  "runtime.firstTokenTimeout": ["Segundos até o primeiro token · 1–1800", "Seconds to the first token · 1–1800"],
  "runtime.idleTimeout": ["Segundos sem resposta do modelo · 1–600", "Seconds without model output · 1–600"],
  "runtime.embeddingModel": ["Modelo de embeddings no Ollama local, ex.: nomic-embed-text · vazio desliga", "Embedding model in the local Ollama, e.g. nomic-embed-text · empty turns it off"],
  "runtime.webSearch": ["Busca externa; cada destino ainda pede aprovação de rede", "External search; each destination still asks for network approval"],
};

async function openSettings(section = 0) {
  let prefs = { settings: [], path: "" }, conns = { items: [], presets: [], integrations: [], detected: [] };
  let selected = 0, query = "", saved = "", problem = "";
  // Two levels: the section menu, then the section's items. ↑↓ move within a
  // level, → or Enter goes in, ← comes back out.
  let pane = "nav";
  const probes = new Map();
  const nav = el("nav", { "aria-label": t("Seções", "Sections") });
  const content = el("div", { style: "min-width:0;overflow-y:auto" });
  const d = openDialog({
    title: t("Configurações", "Settings"), size: "wide",
    body: el("div", { class: "split settings" }, el("div", { class: "col", style: "padding-top:0" }, nav), content),
    onKey: (event) => {
      // A value being typed keeps its keys; the search field only lends them.
      const active = document.activeElement;
      const typing = ["INPUT", "SELECT"].includes(active?.tagName) && !(active.closest(".search") && !(active.value && (event.key === "ArrowLeft" || event.key === "ArrowRight")));
      if (typing && event.key !== "Escape" && event.key !== "Tab") return false;
      if (event.key === "Tab") { event.preventDefault(); go((section + (event.shiftKey ? -1 : 1) + 7) % 7); return true; }
      const step = event.key === "ArrowDown" ? 1 : event.key === "ArrowUp" ? -1 : 0;
      if (pane === "nav") {
        if (step) { event.preventDefault(); go((section + step + 7) % 7); return true; }
        if (event.key === "ArrowRight" || event.key === "Enter") { event.preventDefault(); if (count()) { pane = "content"; selected = 0; draw(); } return true; }
        return false;
      }
      if (step) { event.preventDefault(); const n = count(); if (n) { selected = (selected + step + n) % n; draw(); } return true; }
      if (event.key === "ArrowLeft") { event.preventDefault(); pane = "nav"; draw(); return true; }
      if (section === 0) return connectionKey(event);
      if (section === 3 && ["ArrowRight", " ", "Enter"].includes(event.key)) { event.preventDefault(); view.policy = view.policy === "prompt" ? "project_edits" : "prompt"; renderFrame(); draw(); return true; }
      if ([1, 2, 4, 6].includes(section)) return prefKey(event);
      if (section === 5 && (event.key === "Enter" || event.key === " ")) { event.preventDefault(); const found = conns.detected[selected]; if (found) enableServer(found.name); return true; }
      return false;
    },
  });
  const theme = () => ({ path: "app.theme", label: t("Tema", "Theme"), category: "interface", help: "dark, light, system", value: themeChoice(), origin: "" });
  const rows = () => [theme(), ...prefs.settings].filter((s) => s.path !== "tui.sidebar" && sectionOf(s) === section && (!query || (s.label + " " + s.path).toLowerCase().includes(query.toLowerCase())));
  const count = () => section === 0 ? conns.items.length : section === 3 ? 1 : section === 5 ? conns.detected.length : rows().length;
  const go = (n) => { section = n; selected = 0; query = ""; saved = ""; problem = ""; draw(); };
  const legend = () => {
    if (pane === "nav") return [["↑↓", t("seção", "section")], ["→", t("abrir", "open")], ["Esc", t("fechar", "close"), true]];
    if (section === 0) return [["↑↓", t("conexão", "connection")], ["Enter", t("editar", "edit")], ["N", t("nova", "new")], ["T", t("testar", "test")], ["D", t("excluir", "delete")], ["←", t("menu", "menu")], ["Esc", t("fechar", "close"), true]];
    if (section === 3) return [["→", t("trocar", "switch")], ["←", t("menu", "menu")], ["Esc", t("fechar", "close"), true]];
    if (section === 5) return conns.detected.length
      ? [["↑↓", t("escolher", "choose")], ["Enter", t("ativar", "turn on")], ["←", t("menu", "menu")], ["Esc", t("fechar", "close"), true]]
      : [["←", t("menu", "menu")], ["Esc", t("fechar", "close"), true]];
    const out = [["↑↓", t("preferência", "preference")], ["→", t("valor", "value")], ["←", t("menu", "menu")]];
    out.push([t("Salva ao mudar", "Saves on change"), saved ? "· ✓ " + t("salvo", "saved") : "", false, saved ? "saved" : ""]);
    out.push(["Esc", t("fechar", "close"), true]);
    return out;
  };
  const draw = () => {
    const warn = 0;
    nav.replaceChildren(...sections().map((name, i) => el("div", { class: `nav${i === section ? " on" : ""}${i === section && pane === "content" ? " away" : ""}`, onclick: () => { pane = "content"; go(i); } },
      el("span", {}, name), i === 0 && conns.items.length ? el("span", { style: "color:var(--muted)" }, String(conns.items.length)) : i === 5 && warn ? el("span", { class: "warn" }, `${warn} ${t("aviso", "warning")}`) : null)));
    d.meta(prefs.path);
    d.keys(legend());
    content.replaceChildren();
    if (problem) content.append(el("div", { class: "nothing bad-t" }, problem));
    if (section === 0) drawConnections();
    else if (section === 3) drawApproval();
    else if (section === 5) drawIntegrations();
    else drawPrefs();
  };
  const drawPrefs = () => {
    const field = el("input", { type: "text", placeholder: t("Buscar preferência", "Search preferences"), value: query, "aria-label": t("Buscar preferência", "Search preferences") });
    field.addEventListener("input", () => { query = field.value; selected = 0; const pos = field.selectionStart; draw(); const f = content.querySelector(".search input"); f.focus(); f.setSelectionRange(pos, pos); });
    content.append(el("label", { class: "search", style: "margin:0 20px 6px;padding:7px 12px" }, el("span", {}, "⌕"), field));
    const list = rows();
    if (!list.length) content.append(el("div", { class: "nothing" }, t("Nada encontrado", "Nothing found")));
    list.forEach((s, i) => {
      const opts = options(s);
      let control;
      if (s.origin === "environment") control = el("span", { class: "env" }, `${s.value} · ${t("variável de ambiente", "environment variable")}`);
      else if (opts.length && opts.length <= 3) control = el("span", { class: "seg" }, opts.map((o) => el("button", { type: "button", class: o === s.value ? "on" : "", onclick: () => save(s, o) }, optionLabel(o))));
      else if (opts.length) {
        control = el("select", { "aria-label": s.label, onchange: (e) => save(s, e.target.value) }, opts.map((o) => { const opt = el("option", { value: o }, optionLabel(o)); if (o === s.value) opt.selected = true; return opt; }));
      } else {
        control = el("input", { type: "text", value: s.value, "aria-label": s.label });
        control.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); save(s, control.value.trim()); } });
        control.addEventListener("change", () => save(s, control.value.trim()));
      }
      const help = helpText[s.path] ? t(...helpText[s.path]) : s.category === "shortcuts" ? t("Ctrl ou Alt com uma letra", "Ctrl or Alt with a letter") : s.help;
      content.append(el("div", { class: `pref${pane === "content" && i === selected ? " sel" : ""}`, onclick: () => { pane = "content"; selected = i; d.keys(legend()); nav.querySelector(".on")?.classList.add("away"); content.querySelectorAll(".pref").forEach((p, j) => p.classList.toggle("sel", j === i)); } },
        el("span", { class: "name" }, s.label), control, el("span", { class: "desc" }, help), el("span", {})));
    });
  };
  const prefKey = (event) => {
    const s = rows()[selected];
    if (!s) return false;
    const opts = options(s);
    if (opts.length && ["ArrowRight", " ", "Enter"].includes(event.key)) {
      event.preventDefault();
      const i = opts.indexOf(s.value);
      save(s, opts[(i + 1) % opts.length]);
      return true;
    }
    if (event.key === "Enter" && !opts.length) { event.preventDefault(); content.querySelectorAll(".pref")[selected]?.querySelector("input")?.focus(); return true; }
    return false;
  };
  const save = async (s, value) => {
    if (s.path === "app.theme") {
      try { localStorage.setItem("opcode-theme", value); } catch {}
      applyTheme(value);
      saved = "1";
      draw();
      return;
    }
    try {
      await api("/api/settings", { path: s.path, value });
      saved = "1"; problem = "";
      prefs = await api("/api/settings");
      if (s.path === "tui.language") { view.lang = value === "en" ? "en" : "pt"; render(); }
      if (s.path === "runtime.mode" && !view.session) { view.mode = value; renderFrame(); }
      if (s.path === "tui.proseWidth" || s.path === "preferences.showProcess") { await loadState(); render(); }
    } catch (error) { problem = error.message; saved = ""; }
    draw();
  };
  const drawApproval = () => {
    const seg = el("span", { class: "seg" },
      el("button", { type: "button", class: view.policy === "prompt" ? "on" : "", onclick: () => { view.policy = "prompt"; renderFrame(); draw(); } }, t("Pedir", "Ask")),
      el("button", { type: "button", class: view.policy === "project_edits" ? "on" : "", onclick: () => { view.policy = "project_edits"; renderFrame(); draw(); } }, t("Editar no projeto", "Project edits")));
    content.append(el("div", { class: `pref${pane === "content" ? " sel" : ""}` }, el("span", { class: "name" }, t("Política desta conversa", "This conversation's policy")), seg,
      el("span", { class: "desc" }, t("Editar no projeto aprova edições de arquivos do projeto sem perguntar", "Project edits approves edits to project files without asking")), el("span", {})),
      el("div", { class: "nothing", style: "padding-top:12px" }, t("Comandos, rede, exclusões e caminhos fora do projeto continuam pedindo aprovação.", "Commands, network, deletions and outside paths still ask for approval.")));
  };
  const drawIntegrations = () => {
    content.append(el("div", { class: "pane" }, el("h3", {}, t("Servidores de linguagem e MCP", "Language servers and MCP")),
      conns.integrations.length ? el("div", {}, conns.integrations.map((name) => el("div", {}, el("span", { class: "ok-t" }, "● "), name))) : el("div", { class: "desc" }, t("Nenhuma integração configurada.", "No integrations configured.")),
      conns.detected.length ? el("div", { style: "margin-top:16px" }, el("h3", {}, t("Encontrados neste computador", "Found on this computer")),
        conns.detected.map((found, i) => el("div", { class: `pref${pane === "content" && i === selected ? " sel" : ""}` },
          el("span", { class: "name" }, found.name),
          el("button", { type: "button", class: "act", onclick: () => enableServer(found.name) }, t("Ativar", "Turn on")),
          el("span", { class: "desc" }, found.languages.join(", ")), el("span", {}))),
        el("div", { class: "desc" }, t("O servidor liga ao abrir um arquivo da linguagem.", "It starts when a file of its language is opened."))) : null,
      saved ? el("div", { class: "ok-t", style: "margin-top:12px" }, saved) : null,
      el("div", { class: "tip" }, t("Para adicionar ou editar integrações, use opcode no terminal: Configurações › Integrações.", "To add or edit integrations, use opcode in the terminal: Settings › Integrations."))));
  };
  const enableServer = async (name) => {
    try {
      await api("/api/integrations/lsp", { name });
      conns = await api("/api/connections");
      selected = Math.min(selected, Math.max(0, conns.detected.length - 1));
      saved = `✓ ${name} ${t("ativo", "on")}`; problem = "";
    } catch (error) { problem = error.message; }
    draw();
  };
  const drawConnections = () => {
    const cards = el("div", { class: "cards" });
    conns.items.forEach((c, i) => {
      const inUse = c.provider === conns.active;
      const icon = c.disabled ? el("span", { style: "color:var(--faint)" }, "○") : c.key_missing ? el("span", { class: "warn-t" }, "▲") : el("span", { class: "ok-t" }, "●");
      const state = c.disabled ? el("span", { class: "desc" }, t("desativada", "disabled")) : c.key_missing ? el("span", { class: "desc warn-t" }, t("chave ausente", "key missing")) : inUse ? el("span", { class: "desc" }, t("em uso", "in use")) : el("span", {});
      const card = el("div", { class: `card2${pane === "content" && i === selected ? " sel" : ""}${c.disabled ? " off" : ""}`, onclick: () => { pane = "content"; selected = i; draw(); } },
        icon, el("span", { class: "nm" }, c.name), state);
      if (!c.disabled) {
        const auth = { none: t("sem autenticação", "no authentication"), api_key: t("chave no arquivo de configuração", "key in config file"), vault: t("chave no cofre do sistema", "key in system vault"), environment: c.env + (c.key_missing ? t(" não definida", " not set") : "") }[c.auth];
        const probe = probes.get(c.provider);
        const detail = el("span", { class: "desc full" }, `${c.url || t("endereço padrão do provedor", "provider default address")} · ${auth}`);
        if (probe) detail.append(" · ", el("span", { class: probe.ok ? "" : "bad-t" }, probe.text));
        card.append(el("span", {}), detail, el("span", {}));
        const btn = (label, action) => el("button", { type: "button", class: "btn2", onclick: (e) => { e.stopPropagation(); action(); } }, label);
        const buttons = c.key_missing ? [btn(t("Editar", "Edit"), () => editConnection(c)), btn(t("Desativar", "Disable"), () => enable(c, false))]
          : [btn(t("Editar", "Edit"), () => editConnection(c)), btn(t("Testar", "Test"), () => probe_(c)), btn(t("Modelos", "Models"), () => { closeDialog(); openModels(c.provider); })];
        card.append(el("span", {}), el("span", { class: "btns" }, buttons));
      } else {
        card.addEventListener("dblclick", () => enable(c, true));
      }
      if (i === selected && confirmDelete) card.append(el("span", {}), el("span", { class: "full bad-t" }, t("Excluir esta conexão? Enter confirma · Esc cancela", "Delete this connection? Enter confirms · Esc cancels")));
      cards.append(card);
    });
    cards.append(el("button", { type: "button", class: "add", onclick: () => editConnection(null) }, t("+ Nova conexão · Ollama, OpenAI, Anthropic, Gemini, OpenRouter…", "+ New connection · Ollama, OpenAI, Anthropic, Gemini, OpenRouter…")));
    content.append(cards);
  };
  let confirmDelete = false;
  const connectionKey = (event) => {
    const c = conns.items[selected];
    const k = event.key.toLowerCase();
    if (confirmDelete) {
      if (event.key === "Enter" && c) { event.preventDefault(); remove(c); }
      if (event.key === "Escape") { confirmDelete = false; draw(); }
      return true;
    }
    if (event.ctrlKey || event.altKey || event.metaKey) return false;
    if (k === "n") { event.preventDefault(); editConnection(null); return true; }
    if (event.key === "Enter" && c) { event.preventDefault(); editConnection(c); return true; }
    if (k === "t" && c) { event.preventDefault(); probe_(c); return true; }
    if (k === "m" && c && !c.disabled) { event.preventDefault(); closeDialog(); openModels(c.provider); return true; }
    if (k === "a" && c) { event.preventDefault(); enable(c, c.disabled); return true; }
    if (k === "d" && c) { event.preventDefault(); confirmDelete = true; draw(); return true; }
    return false;
  };
  const probe_ = async (c) => {
    probes.set(c.provider, { ok: true, text: t("testando…", "testing…") });
    draw();
    try {
      const r = await api("/api/connections/test", { provider: c.provider });
      probes.set(c.provider, r.ok ? { ok: true, text: `${r.models} ${t("modelos", "models")} · ${r.ms} ms` } : { ok: false, text: t("sem resposta", "no response") });
    } catch { probes.set(c.provider, { ok: false, text: t("sem resposta", "no response") }); }
    if (dialog?.panel === d.panel) draw();
  };
  const enable = async (c, on) => {
    try { await api("/api/connections/enabled", { id: c.id, legacy: c.legacy, enabled: on }); problem = ""; await loadConnections(); }
    catch (error) { problem = error.message; draw(); }
  };
  const remove = async (c) => {
    confirmDelete = false;
    try { await api("/api/connections/delete", { id: c.id, legacy: c.legacy }); problem = ""; selected = 0; await loadConnections(); }
    catch (error) { problem = error.message; draw(); }
  };
  const loadConnections = async () => {
    conns = await api("/api/connections");
    draw();
    for (const c of conns.items) if (!c.disabled && !c.key_missing && !probes.has(c.provider)) probe_(c);
  };
  prefs = await api("/api/settings");
  await loadConnections();
  go(section);
}

// ---- connection form ----------------------------------------------------------

async function editConnection(existing) {
  const conns = await api("/api/connections");
  if (!existing) {
    const presetName = (name) => name === "OpenAI compatible" ? t("Compatível com OpenAI", "OpenAI compatible") : name;
    const list = makeList({ items: conns.presets.map((p) => ({ id: p.id, title: presetName(p.name), preset: p })), grouped: false, onPick: (item) => form(fromPreset(item.preset, conns)) });
    openDialog({
      title: t("Nova conexão", "New connection"),
      body: el("div", {}, el("div", { class: "nothing", style: "padding-bottom:10px" }, t("Escolha um ponto de partida; revise antes de salvar.", "Choose a starting point; review before saving.")), list.node),
      keys: [["↑↓", t("escolher", "choose")], ["Enter", t("configurar", "configure")], ["Esc", t("voltar", "back"), true]], list,
      onKey: (event) => {
        if (event.key === "Enter") { event.preventDefault(); const item = list.current(); if (item) form(fromPreset(item.preset, conns)); return true; }
        if (event.key === "Escape") { openSettings(0); return true; }
        return false;
      },
    }).panel.style.width = "min(760px, 100%)";
    return;
  }
  form({ original_id: existing.id, legacy: existing.legacy, id: existing.id, name: existing.name, backend: existing.backend, url: existing.url, api: existing.api, auth: existing.auth, key: "", env: existing.env, enabled: !existing.disabled, context: String(existing.context || 0), output: String(existing.output || 0), tls_insecure: existing.tls });
}

function fromPreset(p, conns) {
  let id = p.id, n = 2;
  while (conns.items.some((c) => c.id === id)) id = `${p.id}-${n++}`;
  return { original_id: "", legacy: false, id, name: p.name, backend: p.backend, url: p.url || "", api: p.api || "", auth: p.env ? "environment" : "none", key: "", env: p.env || "", enabled: true, context: "0", output: String(p.output || 0), tls_insecure: false };
}

function form(f) {
  let advanced = false, status = null, busy = false;
  const body = el("div", {});
  const d = openDialog({
    title: f.original_id ? t("Editar conexão", "Edit connection") : t("Nova conexão", "New connection"), meta: f.name,
    body,
    keys: [["Tab", t("campo", "field")], ["Ctrl+S", t("salvar", "save")], ["Ctrl+T", t("testar", "test")], ["Esc", t("voltar", "back"), true]],
    onKey: (event) => {
      if (event.ctrlKey && event.key.toLowerCase() === "s") { event.preventDefault(); save(); return true; }
      if (event.ctrlKey && event.key.toLowerCase() === "t") { event.preventDefault(); test(); return true; }
      if (event.ctrlKey && event.key.toLowerCase() === "a" && document.activeElement?.tagName !== "INPUT") { event.preventDefault(); advanced = !advanced; draw(); return true; }
      if (event.key === "Escape") { openSettings(0); return true; }
      if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); stepField(event.key === "ArrowDown" ? 1 : -1); return true; }
      const active = document.activeElement;
      if (active?.tagName === "SELECT" && (event.key === "ArrowLeft" || event.key === "ArrowRight")) {
        event.preventDefault();
        active.selectedIndex = Math.max(0, Math.min(active.options.length - 1, active.selectedIndex + (event.key === "ArrowLeft" ? -1 : 1)));
        active.dispatchEvent(new Event("change"));
        return true;
      }
      return false;
    },
  });
  // stepField moves between fields: a text box, a list, the chosen radio of a group, then the buttons.
  const stepField = (step) => {
    const stops = [...body.querySelectorAll(".field input[type=text], .field input[type=password], .field select, .field input[type=radio]:checked, .more, .form-actions button:not([disabled])")];
    const at = stops.indexOf(document.activeElement);
    stops[Math.max(0, Math.min(stops.length - 1, at < 0 ? 0 : at + step))]?.focus();
  };
  d.panel.style.width = "min(760px, 100%)";
  const text = (label, key, opts = {}) => {
    const input = el("input", { type: opts.secret ? "password" : "text", id: `cf-${key}`, value: f[key] ?? "", placeholder: opts.placeholder || "", autocomplete: "off", spellcheck: "false" });
    input.addEventListener("input", () => { f[key] = input.value; if (key === "name") d.meta(f.name); status = null; });
    return el("div", { class: "field" }, el("label", { class: "name", for: `cf-${key}` }, label), input);
  };
  const radios = (label, key, choices) => el("div", { class: "field", role: "radiogroup", "aria-label": label }, el("span", { class: "name" }, label),
    el("div", { class: "radios" }, choices.map(([value, name]) => {
      const input = el("input", { type: "radio", name: `cf-${key}`, value: String(value), id: `cf-${key}-${value}` });
      if (String(f[key]) === String(value)) input.checked = true;
      input.addEventListener("change", () => { f[key] = value; status = null; draw(); });
      return el("label", {}, input, " " + name);
    })));
  const select = (label, key, choices) => {
    const s = el("select", { id: `cf-${key}` }, choices.map(([value, name]) => { const o = el("option", { value }, name); if (f[key] === value) o.selected = true; return o; }));
    s.addEventListener("change", () => { f[key] = s.value; status = null; });
    return el("div", { class: "field" }, el("label", { class: "name", for: `cf-${key}` }, label), s);
  };
  const draw = () => {
    const focus = document.activeElement?.id;
    body.replaceChildren(...[
      text(t("Nome", "Name"), "name"),
      text(t("URL da API", "API URL"), "url", { placeholder: f.backend === "local" ? "https://server.example/v1" : t("Padrão do provedor", "Provider default") }),
      radios(t("Autenticação", "Authentication"), "auth", [["none", t("Nenhuma", "None")], ["api_key", t("Chave da API", "API key")], ["environment", t("Variável de ambiente", "Environment variable")], ["vault", t("Cofre", "Vault")]]),
      f.auth === "api_key" || f.auth === "vault" ? text(t("Chave", "Key"), "key", { secret: true, placeholder: f.original_id ? t("Vazio mantém a chave guardada", "Blank keeps the stored key") : t("Cole sua chave da API", "Paste your API key") }) : null,
      f.auth === "environment" ? text(t("Variável", "Variable"), "env", { placeholder: "OPENAI_API_KEY" }) : null,
      radios(t("Conexão", "Connection"), "enabled", [[true, t("Ativa", "On")], [false, t("Desativada", "Off")]]),
      ...(advanced ? [
        text("ID", "id"),
        select(t("Provedor", "Backend"), "backend", ["local", "openai", "anthropic", "gemini", "groq", "openrouter", "xai"].map((b) => [b, b])),
        select("API", "api", [["", t("Automática", "Automatic")], ["chat_completions", "chat_completions"], ["responses", "responses"], ["ollama", "ollama"]]),
        text(t("Janela de contexto", "Context window"), "context", { placeholder: t("0 usa o catálogo", "0 uses the catalog") }),
        text(t("Tokens de saída", "Output tokens"), "output", { placeholder: t("0 usa o catálogo", "0 uses the catalog") }),
        radios("TLS", "tls_insecure", [[false, t("Verificado", "Verified")], [true, t("Sem verificação", "Unverified")]]),
      ] : []),
      status ? el("div", { class: `status ${status.kind}` }, status.kind === "ok" ? el("span", { class: "ok-t" }, "✓") : status.kind === "bad" ? el("span", {}, "✗") : null, el("span", {}, status.text)) : null,
      f.auth === "api_key" ? el("div", { class: "status info" }, t("Ao salvar: chave no JSON local, sem criptografia.", "On save: key in local JSON, unencrypted.")) : null,
      f.tls_insecure === true ? el("div", { class: "status info warn-t" }, t("TLS sem verificação: certificado e identidade não são conferidos.", "Unverified TLS: certificate and identity are not checked.")) : null,
      el("button", { type: "button", class: "more", onclick: () => { advanced = !advanced; draw(); } }, advanced ? t("‹ Ocultar avançado", "‹ Hide advanced") : t("› Avançado: janela de contexto, tokens de saída, TLS", "› Advanced: context window, output tokens, TLS")),
      el("div", { class: "form-actions" },
        el("button", { type: "button", class: "act primary", disabled: busy ? "" : null, onclick: save }, t("Salvar · Ctrl+S", "Save · Ctrl+S")),
        el("button", { type: "button", class: "act", disabled: busy ? "" : null, onclick: test }, t("Testar catálogo · Ctrl+T", "Test catalog · Ctrl+T")),
        el("button", { type: "button", class: "act quiet right", onclick: () => openSettings(0) }, t("Cancelar · Esc", "Cancel · Esc"))),
    ].filter(Boolean));
    if (focus) document.getElementById(focus)?.focus();
    else requestAnimationFrame(() => document.getElementById("cf-name")?.focus());
  };
  const test = async () => {
    status = { kind: "info", text: t("Consultando o catálogo…", "Querying the catalog…") };
    draw();
    try {
      const r = await api("/api/connections/test", { form: f });
      status = r.ok ? { kind: "ok", text: t(`Catálogo respondeu: ${r.models} modelos, ${r.chat} de conversa · ${r.ms} ms`, `Catalog answered: ${r.models} models, ${r.chat} for chat · ${r.ms} ms`) } : { kind: "bad", text: r.error };
    } catch (error) { status = { kind: "bad", text: error.message }; }
    draw();
  };
  const save = async () => {
    busy = true; draw();
    try {
      await api("/api/connections/save", f);
      await loadState();
      renderFrame();
      openSettings(0);
    } catch (error) { busy = false; status = { kind: "bad", text: error.message }; draw(); }
  };
  draw();
}

// ---- shortcuts ----------------------------------------------------------------

// handleShortcut runs before the conversation's own keys; true means taken.
function handleShortcut(event) {
  if (buttonArrows(event)) return true;
  if (dialog) {
    if (event.key === "Enter" && document.activeElement?.tagName === "BUTTON") return false; // the button presses itself
    if (dialog.key && dialog.key(event)) return true;
    if (dialog.list && (event.key === "ArrowDown" || event.key === "ArrowUp")) { event.preventDefault(); dialog.list.move(event.key === "ArrowDown" ? 1 : -1); return true; }
    if (event.key === "Escape") { event.preventDefault(); closeDialog(); return true; }
    return true; // a dialog keeps the keyboard
  }
  if (questionKey(event)) return true;
  // From an empty composer, ←→ reach the buttons of the card above it.
  if ((event.key === "ArrowLeft" || event.key === "ArrowRight") && document.activeElement === $("input") && !$("input").value) {
    const buttons = [...$("work").querySelectorAll("button")];
    if (buttons.length) { event.preventDefault(); (event.key === "ArrowLeft" ? buttons[buttons.length - 1] : buttons[0]).focus(); return true; }
  }
  const k = event.key.toLowerCase();
  if (event.ctrlKey && !event.shiftKey && !event.altKey) {
    const run = { k: () => openPalette(), o: () => openModels(), s: () => openSessions(), n: newConversation, ",": () => openSettings(1) }[k];
    if (run) { event.preventDefault(); run(); return true; }
  }
  return false;
}

// buttonArrows moves focus between buttons that sit side by side; Enter
// then presses the focused one.
function buttonArrows(event) {
  const active = document.activeElement;
  if (active?.tagName !== "BUTTON" || !["ArrowLeft", "ArrowRight"].includes(event.key)) return false;
  const row = [...active.parentElement.querySelectorAll(":scope > button:not([disabled])")];
  if (row.length < 2 && !(row.length === 1 && event.key === "ArrowLeft" && active.closest(".pane"))) return false;
  event.preventDefault();
  const at = row.indexOf(active) + (event.key === "ArrowLeft" ? -1 : 1);
  // Past the first action of a side pane, ← returns to the list beside it.
  if (at < 0 && active.closest(".pane")) { active.blur(); return true; }
  row[(at + row.length) % row.length].focus();
  return true;
}
