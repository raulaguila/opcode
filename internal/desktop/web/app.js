// The conversation screen. State comes from the local backend; every call
// carries the per-run key that the window received in its URL fragment.
"use strict";

const key = (() => {
  const match = location.hash.match(/k=([0-9a-f]+)/);
  if (match) {
    try { sessionStorage.setItem("opcode-key", match[1]); } catch {}
    history.replaceState(null, "", location.pathname); // the key leaves the address bar
    return match[1];
  }
  try { return sessionStorage.getItem("opcode-key") || ""; } catch { return ""; }
})();

const $ = (id) => document.getElementById(id);

// Theme: dark is the proposal's; light and system are this window's choice,
// kept only in its browser.
function themeChoice() { try { return localStorage.getItem("opcode-theme") || "dark"; } catch { return "dark"; } }
function applyTheme(choice = themeChoice()) {
  const light = choice === "light" || (choice === "system" && matchMedia("(prefers-color-scheme: light)").matches);
  document.documentElement.dataset.theme = light ? "light" : "dark";
}
applyTheme();
matchMedia("(prefers-color-scheme: light)").addEventListener("change", () => applyTheme());
const modes = ["ask", "plan", "execute"];
const view = { state: null, session: "", conversation: null, mode: "execute", policy: "sandboxed", started: 0, error: "", lang: "pt" };
const t = (pt, en) => (view.lang === "pt" ? pt : en);

async function api(path, body) {
  const options = { headers: { "X-Opcode-Key": key } };
  if (body !== undefined) {
    options.method = "POST";
    options.headers["Content-Type"] = "application/json";
    options.body = JSON.stringify(body);
  }
  const response = await fetch(path, options);
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || response.statusText);
  return data;
}

function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [name, value] of Object.entries(attrs)) {
    if (name === "class") node.className = value;
    else if (name === "html") node.innerHTML = value; // backend-rendered Markdown, raw HTML disabled
    else if (name.startsWith("on")) node.addEventListener(name.slice(2), value);
    else if (value != null && value !== false) node.setAttribute(name, value);
  }
  for (const child of children.flat()) if (child != null) node.append(child);
  return node;
}

const modeLabel = (mode) => ({ ask: t("Perguntar", "Ask"), plan: t("Planejar", "Plan"), execute: t("Executar", "Execute") })[mode];

// Steps fold into one line per run of the same tool and outcome:
// "✓ view store.go · main.go", "✗ edit store.go ×3 · 3 falhas".
function stepRows(steps) {
  const runs = [];
  for (const step of steps) {
    const last = runs[runs.length - 1];
    if (last && !view.state?.show_process && last.name === step.name && last.state === step.state) { last.targets.push(step.target); last.added += step.added || 0; last.removed += step.removed || 0; }
    else runs.push({ name: step.name, state: step.state, targets: [step.target], added: step.added || 0, removed: step.removed || 0 });
  }
  return runs.map((run) => {
    const names = [...new Set(run.targets.filter(Boolean))];
    let label = run.name + (names.length ? " " + names.join(" · ") : "");
    if (names.length === 1 && run.targets.length > 1) label += " ×" + run.targets.length;
    const icon = { ok: el("span", { class: "ok" }, "✓"), failed: el("span", { class: "bad" }, "✗"), running: el("span", { class: "run" }, "●") }[run.state];
    const row = el("div", {}, "› ", icon, " " + label);
    if (run.added || run.removed) row.append(` · ${run.added ? "+" + run.added : ""}${run.added && run.removed ? " " : ""}${run.removed ? "−" + run.removed : ""}`);
    if (run.state === "failed") row.append(el("span", { class: "bad" }, ` · ${run.targets.length} ${run.targets.length === 1 ? t("falha", "failure") : t("falhas", "failures")}`));
    if (run.state === "running" && view.conversation?.permissions?.length) row.append(t(" · aguardando sua aprovação", " · waiting for your approval"));
    return row;
  });
}

function failureTitle(reason) {
  const lower = reason.toLowerCase();
  if (/connection refused|deadline exceeded|no such host/.test(lower)) return t("Provedor indisponível", "Provider unavailable");
  if (/401|unauthorized|api key/.test(lower)) return t("Autenticação necessária", "Authentication required");
  if (/out of memory/.test(lower)) return t("Memória insuficiente no servidor do modelo", "The model server ran out of memory");
  if (/repeated tool calls|failed 3 times/.test(lower)) return t("o modelo repetiu a mesma ação sem progresso", "the model repeated the same action without progress");
  if (/step limit|max steps/.test(lower)) return t("limite de etapas atingido", "step limit reached");
  return t("a execução parou com um erro", "the run stopped with an error");
}

// cause explains a known failure in a sentence; the raw error waits for Details.
function cause(reason) {
  const lower = reason.toLowerCase();
  if (/connection refused|deadline exceeded|no such host/.test(lower)) return t("O servidor do modelo não respondeu.", "The model server did not answer.");
  if (/401|unauthorized|api key/.test(lower)) return t("A chave da API foi recusada ou está ausente.", "The API key was refused or is missing.");
  if (/out of memory/.test(lower)) return t("O modelo não coube na memória disponível.", "The model did not fit in the available memory.");
  if (/repeated tool calls|failed 3 times/.test(lower)) return t("A mesma ação falhou várias vezes do mesmo jeito.", "The same action failed the same way several times.");
  if (/step limit|max steps/.test(lower)) return t("A tarefa usou todas as etapas permitidas.", "The task used every step it was allowed.");
  return "";
}

function failureCard(reason) {
  const detail = el("p", { class: "detail", hidden: "" }, reason);
  return el("div", { class: "failure" },
    el("b", {}, "■ " + t("Parado: ", "Stopped: ") + failureTitle(reason)),
    el("p", {}, `${cause(reason)} ${t("Rascunho e progresso foram preservados.", "Draft and progress were kept.")}`.trim()),
    detail,
    el("div", { class: "row" },
      el("button", { type: "button", class: "btn primary", onclick: retry }, t("/retry Retomar", "/retry Resume")),
      el("button", { type: "button", class: "btn", onclick: () => { detail.hidden = !detail.hidden; } }, t("/error Detalhes", "/error Details")),
      el("button", { type: "button", class: "btn", onclick: () => openModels() }, t("/models Trocar modelo", "/models Change model")),
    ));
}

// The transcript is a list of blocks, each with a key that says what it
// shows. A redraw keeps the blocks that did not change and rebuilds from the
// first that did, so a streaming reply leaves the rest of the page (and any
// text selected in it) alone.
function renderTranscript() {
  const box = $("transcript");
  const follow = box.scrollHeight - box.scrollTop - box.clientHeight < 40;
  const blocks = [];
  const conversation = view.conversation;
  if (!conversation || conversation.messages.length === 0) {
    const bare = view.state && !view.state.model;
    blocks.push({ key: `empty:${bare}:${view.lang}`, make: () => el("div", { class: "empty" },
      el("b", {}, bare ? t("Configure uma conexão para começar", "Set up a connection to begin") : t("O que vamos fazer neste projeto?", "What shall we do in this project?")),
      el("span", {}, bare ? t("Ctrl+K › Configurações › Conexões · depois escolha o modelo com Ctrl+O", "Ctrl+K › Settings › Connections · then pick the model with Ctrl+O") : t("Shift+Tab alterna Perguntar, Planejar e Executar", "Shift+Tab cycles Ask, Plan and Execute"))) });
  } else {
    let steps = [];
    let afterSteps = -1;
    const flush = () => {
      if (steps.length) {
        const run = steps;
        blocks.push({ key: `steps:${view.lang}:${JSON.stringify(run)}`, make: () => el("div", { class: "steps" }, stepRows(run)) });
        afterSteps = blocks.length;
        steps = [];
      }
    };
    // One reply per turn: a revised answer replaces the draft before it.
    const latest = new Map();
    let turn = 0;
    for (const m of conversation.messages) {
      if (m.role === "user") turn++;
      if (m.role === "assistant" && m.html) latest.set(turn, m.id);
    }
    turn = 0;
    for (const m of conversation.messages) {
      if (m.role === "user") {
        turn++;
        flush();
        blocks.push({ key: `you:${m.id}:${m.text}`, make: () => el("div", { class: "you" }, m.text) });
        continue;
      }
      if (m.html && latest.get(turn) === m.id) {
        flush();
        const meta = m.finished && m.model ? [m.model.replace(/^[^.]*\./, ""), `${m.seconds || "<1"}s`, m.cites].filter(Boolean).join(" · ") : "";
        blocks.push({ key: `reply:${m.id}:${meta}:${m.html}`, make: () => {
          const reply = el("div", { class: "reply" }, el("div", { html: m.html }));
          if (meta) reply.append(el("div", { class: "meta" }, meta));
          return reply;
        } });
      }
      steps.push(...(m.steps || []));
    }
    flush();
    // While the model thinks, its reasoning shows live; the answer replaces it.
    if (conversation.thinking) {
      blocks.push({ key: `thinking:${view.lang}:${conversation.thinking}`, make: () => el("div", { class: "thinking" },
        el("b", {}, t("Pensando…", "Thinking…")), el("div", {}, conversation.thinking)) });
    }
    if (conversation.failure) {
      blocks.splice(afterSteps >= 0 ? afterSteps : blocks.length, 0, { key: `failure:${view.lang}:${conversation.failure}`, make: () => failureCard(conversation.failure) });
    }
  }
  const kept = box.keys || [];
  let same = 0;
  while (same < blocks.length && same < kept.length && same < box.children.length && kept[same] === blocks[same].key) same++;
  while (box.children.length > same) box.lastChild.remove();
  for (const block of blocks.slice(same)) box.append(block.make());
  box.keys = blocks.map((block) => block.key);
  if (follow) box.scrollTop = box.scrollHeight;
}

function renderWork() {
  const work = $("work");
  work.replaceChildren();
  const conversation = view.conversation;
  const request = conversation?.permissions?.[0];
  if (!request) view.fullDiff = false;
  if (request) { work.append(approvalCard(request)); return; }
  if (conversation?.question && view.laterQuestion !== conversation.question.id) { work.append(questionCard(conversation.question)); return; }
  const steps = conversation?.task?.steps || [];
  if (steps.length && view.showPlan) { work.append(planCard(conversation.task)); return; }
  if (steps.length) {
    const done = steps.filter((s) => s.status === "completed").length;
    const index = steps.findIndex((s) => s.status !== "completed");
    const current = index >= 0 ? `→ ${index + 1}. ${steps[index].title}` : t("concluído", "done");
    work.append(el("div", { class: "strip-wrap" }, el("div", { class: "strip", onclick: togglePlan, title: "/task" },
      el("span", { class: "plan" }, `${t("Plano", "Plan")} ${done}/${steps.length}`), el("span", {}, current), el("span", { class: "end" }, "/task"))));
  }
}

function diffRows(diff, limit = 6) {
  const rows = [];
  let added = 0, removed = 0, newLine = 0, oldLine = 0, hidden = 0;
  for (const line of diff.split("\n")) {
    if (line.startsWith("+++") || line.startsWith("---") || line.startsWith("\\")) continue;
    if (line.startsWith("@@")) {
      const match = line.match(/-(\d+)(?:,\d+)? \+(\d+)/);
      if (match) { oldLine = +match[1]; newLine = +match[2]; }
      continue;
    }
    if (!line || (!newLine && !oldLine)) continue;
    let kind = "ctx", number = newLine;
    if (line[0] === "+") { kind = "add"; added++; newLine++; }
    else if (line[0] === "-") { kind = "del"; removed++; number = oldLine; oldLine++; }
    else { newLine++; oldLine++; }
    if (rows.length >= limit && !view.fullDiff) { hidden++; continue; }
    rows.push(el("div", { class: kind }, el("span", { class: "n" }, String(number)), el("span", {}, line)));
  }
  if (hidden) rows.push(el("div", { class: "ctx" }, el("span", { class: "n" }, "…"), el("span", {}, t(`${hidden} linhas · T mostra tudo`, `${hidden} lines · T shows all`))));
  return { rows, added, removed };
}

function approvalCard(request) {
  const edit = ["edit", "write", "patch"].includes(request.tool);
  const title = edit ? t("Aprovar edição", "Approve edit") : request.tool === "bash" ? t("Aprovar comando", "Approve command") : t("Aprovação necessária", "Approval required");
  const diff = request.diff ? diffRows(request.diff) : null;
  const card = el("div", { class: "card" },
    el("span", { class: "edge left" }, title),
    el("span", { class: "edge right" }, diff ? `1 ${t("arquivo", "file")} · +${diff.added} −${diff.removed}` : request.tool),
    el("div", { class: "head" }, el("b", {}, request.file || request.command || ""), el("span", {}, request.description)),
    diff ? el("div", { class: "diff" }, diff.rows) : null,
    el("div", { class: "row", style: "margin-top:14px" },
      el("button", { type: "button", class: "choice primary", onclick: () => decide("once") }, t("1 Permitir uma vez", "1 Allow once")),
      el("button", { type: "button", class: "choice", onclick: () => decide("session") }, edit ? t("2 Permitir edições no projeto nesta sessão", "2 Allow project edits this session") : t("2 Permitir este escopo na sessão", "2 Allow this scope for the session")),
      el("button", { type: "button", class: "choice deny", onclick: () => decide("deny") }, t("3 Recusar · Esc", "3 Deny · Esc"))),
    el("div", { class: "note" }, t("Comandos, rede, exclusões e caminhos fora do projeto continuam pedindo aprovação.", "Commands, network, deletions and outside paths still ask for approval.")));
  return el("div", { class: "card-wrap" }, card);
}

// doing names the step in progress, as the proposal's footer does:
// "Editando main.go", "Testando".
function doing() {
  const messages = view.conversation?.messages || [];
  for (let i = messages.length - 1; i >= 0; i--) {
    const step = (messages[i].steps || []).filter((s) => s.state === "running").pop();
    if (!step) continue;
    const verb = /^(edit|write|patch|multiedit)/.test(step.name) ? t("Editando", "Editing")
      : step.name === "bash" ? (/\btest/.test(step.target) ? t("Testando", "Testing") : t("Executando", "Running"))
      : /^(view|read)/.test(step.name) ? t("Lendo", "Reading")
      : /^(glob|grep|ls|search|find)/.test(step.name) ? t("Buscando", "Searching")
      : /fetch|web/.test(step.name) ? t("Consultando a web", "Browsing")
      : "";
    if (!verb) break;
    return step.name === "bash" ? verb : `${verb} ${step.target}`.trim();
  }
  return t("Trabalhando", "Working");
}

function renderFrame() {
  const state = view.state;
  if (!state) return;
  $("workspace").textContent = state.workspace;
  $("branch").textContent = state.branch;
  $("branch-wrap").hidden = !state.branch;
  const title = view.conversation?.session?.title || "";
  $("title").textContent = title && view.conversation.number ? `${title} · ${t("sessão", "session")} ${view.conversation.number}` : title;
  document.documentElement.style.setProperty("--mode", `var(--${view.mode})`);
  $("mode").textContent = modeLabel(view.mode);
  // While the agent asks, its card is where you answer, as in the proposal.
  const asking = view.conversation?.question && view.laterQuestion !== view.conversation.question.id;
  document.querySelector(".composer-wrap").hidden = !!asking;
  // While you decide on an approval, the composer only keeps your draft.
  const deciding = !!view.conversation?.permissions?.length;
  $("composer").classList.toggle("deciding", deciding);
  const busy = view.conversation?.busy;
  $("hint").innerHTML = "";
  if (busy) $("hint").append(t("Mensagens entram na fila · ", "Messages queue up · "), el("b", {}, "Esc"), t(" cancela", " cancels"));
  else $("hint").append(el("b", {}, "Shift+Tab"), t(" modo · ", " mode · "), el("b", {}, "/"), t(" comandos · ", " commands · "), el("b", {}, "@"), t(" arquivos · ", " files · "), el("b", {}, "Shift+Enter"), t(" nova linha", " new line"));
  $("input").placeholder = deciding ? t("Rascunho preservado enquanto você decide", "Draft kept while you decide") : busy ? t("Mensagem entra na fila", "Message joins the queue") : "";
  const narrow = window.innerWidth < 760; // the proposal's compact screen abbreviates
  $("policy").textContent = narrow
    ? (view.policy === "auto" ? "A:auto" : t("A:pedir", "A:ask"))
    : (view.policy === "auto" ? t("Aprovar por mim", "Approve for me") : t("Pedir aprovação", "Ask for approval"));
  const activity = $("activity");
  activity.className = "";
  activity.style.color = "";
  if (view.conversation?.permissions?.length) { activity.textContent = t("Aguardando sua aprovação", "Waiting for your approval"); activity.className = "busy"; }
  else if (view.conversation?.question) {
    activity.textContent = asking ? t("Aguardando sua resposta", "Waiting for your answer") : t("Pergunta pendente · Enter responde", "Question pending · Enter answers");
    activity.className = asking ? "" : "link";
    activity.style.color = "var(--ask)";
  }
  else if (busy) {
    const seconds = Math.max(0, Math.round((Date.now() - view.started) / 1000));
    const now = doing();
    activity.textContent = narrow ? `${now.split(" ")[0]} ${seconds}s` : `${now} · ${seconds}s · Esc ${t("cancela", "cancels")}`;
    activity.className = "busy";
  }
  else if (view.error) { activity.textContent = view.error; activity.className = "bad"; }
  else activity.textContent = "";
  const tokens = view.conversation?.session?.prompt_tokens || 0;
  $("meter").replaceChildren();
  if (state.context && tokens) {
    const used = Math.min(1, tokens / state.context);
    const filled = Math.min(5, Math.floor(used * 5));
    $("meter").append(el("span", { class: "meter" }, "█".repeat(filled)), el("span", { style: "color:var(--faint)" }, "░".repeat(5 - filled)), ` ${Math.round(used * 100)}%`);
  }
  const speed = view.conversation?.speed;
  if (speed && !narrow) $("meter").append(`${tokens && state.context ? " · " : ""}${view.conversation.speed_live ? "~" : ""}${Math.round(speed)} t/s`);
  $("model").textContent = (narrow && state.model.length > 10 ? state.model.slice(0, 9) + "…" : state.model) + " ▾";
}

function render() {
  renderFrame();
  renderTranscript();
  renderWork();
}

async function loadState() {
  view.state = await api("/api/state");
  view.lang = view.state.language === "en" ? "en" : "pt";
  document.documentElement.lang = view.lang === "pt" ? "pt-BR" : "en";
  document.documentElement.style.setProperty("--prose", view.state.prose_width > 0 ? `${view.state.prose_width}ch` : "none");
  if (modes.includes(view.state.mode)) view.mode = view.state.mode;
}

async function loadConversation() {
  if (!view.session) { view.conversation = null; view.raw = ""; render(); return; }
  const before = view.conversation?.busy;
  const data = await api(`/api/sessions/${view.session}`);
  view.lastLoad = Date.now();
  // Redraw the conversation only when it changed, so focus and hover survive polling.
  const raw = JSON.stringify(data);
  const changed = raw !== view.raw;
  view.raw = raw;
  view.conversation = data;
  if (data.busy && !before) view.started = view.started || Date.now();
  if (!data.busy) view.started = 0;
  if (changed) render();
  else renderFrame();
}

// ↑ on the composer's first line brings back the message you sent before,
// ↓ on its last line the one after; past the newest, your draft returns.
const recall = { at: 0, draft: "" };
function recallKey(event) {
  if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return false;
  const input = $("input");
  const sent = (view.conversation?.messages || []).filter((m) => m.role === "user" && m.text).map((m) => m.text)
    .filter((text, i, all) => i === 0 || all[i - 1] !== text);
  recall.at = Math.min(recall.at, sent.length); // another conversation may have fewer
  const firstLine = !input.value.slice(0, input.selectionStart).includes("\n");
  const lastLine = !input.value.slice(input.selectionEnd).includes("\n");
  if (event.key === "ArrowUp" && firstLine && recall.at < sent.length) {
    if (recall.at === 0) recall.draft = input.value;
    recall.at++;
  } else if (event.key === "ArrowDown" && lastLine && recall.at > 0) {
    recall.at--;
  } else return false;
  event.preventDefault();
  input.value = recall.at ? sent[sent.length - recall.at] : recall.draft;
  input.setSelectionRange(input.value.length, input.value.length);
  grow();
  return true;
}

async function send() {
  const input = $("input");
  const text = input.value.trim();
  if (!text) return;
  view.error = "";
  try {
    if (!view.session) {
      const created = await api("/api/sessions", { mode: view.mode, policy: view.policy });
      view.session = created.id;
    }
    await api(`/api/sessions/${view.session}/prompt`, { text, mode: view.mode, policy: view.policy });
    view.lastText = text;
    input.value = "";
    recall.at = 0;
    grow();
    view.started = Date.now();
    await loadConversation();
  } catch (error) {
    view.error = error.message;
    renderFrame();
  }
}

async function retry() {
  if (!view.lastText || !view.session) return;
  $("input").value = view.lastText;
  await send();
}

async function decide(decision) {
  const request = view.conversation?.permissions?.[0];
  if (!request) return;
  try { await api(`/api/sessions/${view.session}/permissions/${request.id}`, { decision }); }
  catch (error) { view.error = error.message; }
  await loadConversation();
}

function cycleMode() {
  view.mode = modes[(modes.indexOf(view.mode) + 1) % modes.length];
  renderFrame();
}

function grow() {
  const input = $("input");
  input.style.height = "auto";
  input.style.height = Math.min(input.scrollHeight, 22 * 6) + "px";
}

// The window refreshes whatever session a backend event names.
async function listen() {
  for (;;) {
    try {
      const response = await fetch("/api/events", { headers: { "X-Opcode-Key": key } });
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      let pending = null;
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        let cut;
        while ((cut = buffer.indexOf("\n\n")) >= 0) {
          const line = buffer.slice(0, cut);
          buffer = buffer.slice(cut + 2);
          const id = line.startsWith("data: ") ? line.slice(6) : "";
          if (id && id === view.session && !pending) pending = setTimeout(() => { pending = null; loadConversation().catch(() => {}); }, 120);
        }
      }
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
}

document.addEventListener("keydown", (event) => {
  if (handleShortcut(event)) return;
  if (event.altKey && (event.key === "ArrowUp" || event.key === "ArrowDown")) { event.preventDefault(); $("transcript").scrollBy({ top: event.key === "ArrowUp" ? -120 : 120 }); return; }
  if ((event.key === "t" || event.key === "T") && view.conversation?.permissions?.length && (document.activeElement !== $("input") || !$("input").value)) { event.preventDefault(); view.fullDiff = !view.fullDiff; renderWork(); return; }
  const request = view.conversation?.permissions?.[0];
  if (request && ["1", "2", "3"].includes(event.key) && document.activeElement !== $("input")) {
    event.preventDefault();
    decide({ 1: "once", 2: "session", 3: "deny" }[event.key]);
    return;
  }
  if (event.key === "Escape") {
    if (request) { decide("deny"); return; }
    if (view.conversation?.busy) api(`/api/sessions/${view.session}/cancel`, {}).catch(() => {});
    return;
  }
  if (event.key === "Tab" && event.shiftKey) { event.preventDefault(); cycleMode(); }
});

$("input").addEventListener("keydown", (event) => {
  if (dialog) return; // an open dialog owns the keyboard
  if (mentionKey(event)) return;
  const request = view.conversation?.permissions?.[0];
  if (request && $("input").value === "" && ["1", "2", "3"].includes(event.key)) {
    event.preventDefault();
    decide({ 1: "once", 2: "session", 3: "deny" }[event.key]);
    return;
  }
  if (event.key === "/" && $("input").value === "") { event.preventDefault(); openPalette(); return; }
  if (recallKey(event)) return;
  if (event.key === "Enter" && !event.shiftKey && !event.altKey && !event.isComposing) { event.preventDefault(); send(); }
});
$("input").addEventListener("input", () => { recall.at = 0; }); // an edited message is a new draft
$("input").addEventListener("input", grow);
$("input").addEventListener("input", mentionUpdate);
$("input").addEventListener("blur", () => setTimeout(mentionClose, 150));
$("activity").addEventListener("click", () => { if (view.conversation?.question && view.laterQuestion) { view.laterQuestion = ""; render(); } });
window.addEventListener("resize", renderFrame);
$("mode").addEventListener("click", cycleMode);
$("policy").addEventListener("click", () => openSettings(3));
$("model").addEventListener("click", () => openModels());
// The last event of a run can arrive before the run lets go of the session,
// so a busy conversation is read again until it settles.
setInterval(() => {
  if (!view.conversation?.busy) return;
  renderFrame();
  if (Date.now() - (view.lastLoad || 0) > 1500) loadConversation().catch(() => {});
}, 1000);

(async () => {
  try {
    await loadState();
    render();
    listen();
    // With no model yet, the first screen is setting up a connection.
    if (!view.state.model) openSettings(0).then(() => editConnection(null));
  } catch (error) {
    $("transcript").replaceChildren(el("div", { class: "empty" }, el("b", {}, "opcode"), el("span", {}, error.message)));
  }
})();

// @ mentions: typing @ and part of a path lists the workspace's files; Enter
// or Tab puts the chosen path in the message.
const mention = { node: null, items: [], selected: 0, start: -1, asked: "" };

function mentionToken() {
  const input = $("input");
  const before = input.value.slice(0, input.selectionStart);
  const match = before.match(/(^|\s)@([^\s@]*)$/);
  return match ? { start: before.length - match[2].length - 1, query: match[2] } : null;
}

async function mentionUpdate() {
  const token = mentionToken();
  if (!token) { mentionClose(); return; }
  mention.start = token.start;
  if (mention.asked === token.query && mention.node) return;
  mention.asked = token.query;
  const found = await api(`/api/files?q=${encodeURIComponent(token.query)}`).catch(() => []);
  if (mention.asked !== token.query) return; // a newer key already asked
  mention.items = found;
  mention.selected = 0;
  mentionDraw();
}

function mentionDraw() {
  if (!mention.items.length) { mentionClose(); return; }
  if (!mention.node) {
    mention.node = el("div", { class: "mention", role: "listbox", "aria-label": t("Arquivos", "Files") });
    $("composer").append(mention.node);
  }
  mention.node.replaceChildren(el("div", { class: "grp" }, t("Arquivos", "Files")), ...mention.items.map((path, i) =>
    el("div", { class: `item${i === mention.selected ? " sel" : ""}`, role: "option", onmousedown: (e) => { e.preventDefault(); mention.selected = i; mentionPick(); } },
      el("span", { class: "t" }, path))));
}

function mentionPick() {
  const path = mention.items[mention.selected];
  const input = $("input");
  if (!path || mention.start < 0) return;
  const after = input.value.slice(input.selectionStart);
  input.value = `${input.value.slice(0, mention.start)}@${path} ${after}`;
  const caret = mention.start + path.length + 2;
  input.setSelectionRange(caret, caret);
  mentionClose();
  grow();
}

function mentionClose() {
  mention.node?.remove();
  Object.assign(mention, { node: null, items: [], selected: 0, start: -1, asked: "" });
}

function mentionKey(event) {
  if (!mention.node) return false;
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    event.preventDefault();
    mention.selected = (mention.selected + (event.key === "ArrowDown" ? 1 : -1) + mention.items.length) % mention.items.length;
    mentionDraw();
    return true;
  }
  if (event.key === "Enter" || event.key === "Tab") { event.preventDefault(); event.stopPropagation(); mentionPick(); return true; }
  if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); mentionClose(); return true; }
  return false;
}
