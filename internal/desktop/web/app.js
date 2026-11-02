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
const modes = ["ask", "plan", "execute"];
const view = { state: null, session: "", conversation: null, mode: "execute", policy: "prompt", started: 0, error: "", lang: "pt" };
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
    if (last && last.name === step.name && last.state === step.state) last.targets.push(step.target);
    else runs.push({ name: step.name, state: step.state, targets: [step.target] });
  }
  return runs.map((run) => {
    const names = [...new Set(run.targets.filter(Boolean))];
    let label = run.name + (names.length ? " " + names.join(" · ") : "");
    if (names.length === 1 && run.targets.length > 1) label += " ×" + run.targets.length;
    const icon = { ok: el("span", { class: "ok" }, "✓"), failed: el("span", { class: "bad" }, "✗"), running: el("span", { class: "run" }, "●") }[run.state];
    const row = el("div", {}, "› ", icon, " " + label);
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

function failureCard(reason) {
  const detail = el("p", { class: "detail", hidden: "" }, reason);
  return el("div", { class: "failure" },
    el("b", {}, "■ " + t("Parado: ", "Stopped: ") + failureTitle(reason)),
    el("p", {}, t("Rascunho e progresso foram preservados.", "Draft and progress were kept.")),
    detail,
    el("div", { class: "row" },
      el("button", { type: "button", class: "btn primary", onclick: retry }, t("/retry Retomar", "/retry Resume")),
      el("button", { type: "button", class: "btn", onclick: () => { detail.hidden = !detail.hidden; } }, t("/error Detalhes", "/error Details")),
    ));
}

function renderTranscript() {
  const box = $("transcript");
  const follow = box.scrollHeight - box.scrollTop - box.clientHeight < 40;
  box.replaceChildren();
  const conversation = view.conversation;
  if (!conversation || conversation.messages.length === 0) {
    box.append(el("div", { class: "empty" },
      el("b", {}, t("O que vamos fazer neste projeto?", "What shall we do in this project?")),
      el("span", {}, t("Shift+Tab alterna Perguntar, Planejar e Executar", "Shift+Tab cycles Ask, Plan and Execute"))));
    return;
  }
  let steps = [];
  let lastSteps = null;
  const flush = () => {
    if (steps.length) {
      lastSteps = el("div", { class: "steps" }, stepRows(steps));
      box.append(lastSteps);
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
      box.append(el("div", { class: "you" }, m.text));
      continue;
    }
    if (m.html && latest.get(turn) === m.id) {
      flush();
      const reply = el("div", { class: "reply" }, el("div", { html: m.html }));
      if (m.finished && m.model) reply.append(el("div", { class: "meta" }, `${m.model.replace(/^[^.]*\./, "")} · ${m.seconds || "<1"}s`));
      box.append(reply);
    }
    steps.push(...(m.steps || []));
  }
  flush();
  if (conversation.failure) {
    const card = failureCard(conversation.failure);
    if (lastSteps) lastSteps.after(card); else box.append(card);
  }
  if (follow) box.scrollTop = box.scrollHeight;
}

function renderWork() {
  const work = $("work");
  work.replaceChildren();
  const conversation = view.conversation;
  const request = conversation?.permissions?.[0];
  if (request) { work.append(approvalCard(request)); return; }
  if (conversation?.question) { work.append(questionCard(conversation.question)); return; }
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

function diffRows(diff) {
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
    if (rows.length >= 6) { hidden++; continue; }
    rows.push(el("div", { class: kind }, el("span", { class: "n" }, String(number)), el("span", {}, line)));
  }
  if (hidden) rows.push(el("div", { class: "ctx" }, el("span", { class: "n" }, "…"), el("span", {}, t(`mais ${hidden} linhas`, `${hidden} more lines`))));
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

function renderFrame() {
  const state = view.state;
  if (!state) return;
  $("workspace").textContent = state.workspace;
  $("branch").textContent = state.branch;
  $("branch-wrap").hidden = !state.branch;
  $("title").textContent = view.conversation?.session?.title || "";
  document.documentElement.style.setProperty("--mode", `var(--${view.mode})`);
  $("mode").textContent = modeLabel(view.mode);
  // While the agent asks, its card is where you answer, as in the proposal.
  document.querySelector(".composer-wrap").hidden = !!view.conversation?.question;
  const busy = view.conversation?.busy;
  $("hint").innerHTML = "";
  if (busy) $("hint").append(t("Mensagens entram na fila · ", "Messages queue up · "), el("b", {}, "Esc"), t(" cancela", " cancels"));
  else $("hint").append(el("b", {}, "Shift+Tab"), t(" modo · ", " mode · "), el("b", {}, "Alt+Enter"), t(" nova linha", " new line"));
  $("input").placeholder = busy ? t("Mensagem entra na fila", "Message joins the queue") : "";
  const narrow = window.innerWidth < 760; // the proposal's compact screen abbreviates
  $("policy").textContent = narrow
    ? (view.policy === "project_edits" ? t("A:editar", "A:edits") : t("A:pedir", "A:ask"))
    : (view.policy === "project_edits" ? t("Aprov.: editar no projeto", "Approval: project edits") : t("Aprov.: pedir", "Approval: ask")) + " ▾";
  const activity = $("activity");
  activity.className = "";
  activity.style.color = "";
  if (view.conversation?.permissions?.length) { activity.textContent = t("Aguardando sua aprovação", "Waiting for your approval"); activity.className = "busy"; }
  else if (view.conversation?.question) { activity.textContent = t("Aguardando sua resposta", "Waiting for your answer"); activity.className = ""; activity.style.color = "var(--ask)"; }
  else if (busy) {
    const seconds = Math.max(0, Math.round((Date.now() - view.started) / 1000));
    activity.textContent = narrow ? `${t("Trabalhando", "Working")} ${seconds}s` : `${t("Trabalhando", "Working")} · ${seconds}s · Esc ${t("cancela", "cancels")}`;
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
  const request = view.conversation?.permissions?.[0];
  if (request && $("input").value === "" && ["1", "2", "3"].includes(event.key)) {
    event.preventDefault();
    decide({ 1: "once", 2: "session", 3: "deny" }[event.key]);
    return;
  }
  if (event.key === "/" && $("input").value === "") { event.preventDefault(); openPalette(); return; }
  if (event.key === "Enter" && !event.shiftKey && !event.altKey && !event.isComposing) { event.preventDefault(); send(); }
});
$("input").addEventListener("input", grow);
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
  } catch (error) {
    $("transcript").replaceChildren(el("div", { class: "empty" }, el("b", {}, "opcode"), el("span", {}, error.message)));
  }
})();
