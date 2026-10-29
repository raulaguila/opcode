#!/usr/bin/env python3
"""Opt-in real application adapters. Stdin/stdout implement evaluation.Job.

Codaro uses its public Python Agent API (the CLI chat is interactive-only).
Crush uses its official `run` CLI, with read-only tools and no --yolo.
No credential or raw provider exception is emitted. This file does no installs.
"""
from __future__ import annotations

import argparse
import contextlib
import importlib.metadata
import json
import os
from pathlib import Path
import re
import sqlite3
import subprocess
import sys

LIMIT = 2 << 20


def bounded_json(path: Path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size > LIMIT:
        raise ValueError("invalid bounded input")
    return json.loads(path.read_bytes())


def validate_job(job):
    root = Path(job["workspace"])
    base = root.parent
    if (
        root != Path.cwd() or root.name != "workspace"
        or not base.name.startswith("opcode-eval-")
        or root.is_symlink() or root.resolve() != root
        or Path(job["config"]) != base / "config.json"
        or not 4096 <= job["conditions"]["context_window"] <= 2_000_000
        or job["case"]["mode"] not in {"ask", "plan", "execute"}
        or job["conditions"]["mode"] != job["case"]["mode"]
        or job["case"]["mode"] == "execute" and not job.get("allow_execution")
    ):
        raise ValueError("adapter requires a disposable explicitly authorized job")
    return root, bounded_json(base / "config.json")


def connection(config, conditions):
    model = config.get("models", {}).get(conditions["model"], {})
    profile = model.get("provider_profile")
    provider = config.get("providerProfiles", {}).get(profile, {}) if profile else config.get("providers", {}).get(model.get("provider"), {})
    api_model = model.get("api_model")
    protocol = model.get("api") or provider.get("api") or "openai"
    endpoint = provider.get("baseURL")
    if not endpoint or not api_model or conditions.get("api_model") != api_model or conditions.get("protocol") != protocol:
        raise ValueError("adapter requires explicit matching endpoint/api/api_model")
    key = os.environ.get(provider.get("apiKeyEnv", ""), "") or provider.get("apiKey", "")
    reserve = conditions.get("output_reserve")
    if not isinstance(reserve, int) or reserve < 1:
        raise ValueError("comparison requires an explicit output reserve")
    # This alias denotes the same /chat/completions wire protocol, not a switch
    # from native Ollama. Keep the requested protocol unchanged in observations.
    adapter_protocol = "openai" if protocol == "chat_completions" else protocol
    return endpoint, api_model, adapter_protocol, key, reserve


def unknown(conditions, reason):
    return {"conditions": conditions, "response": "", "tools": [], "unavailable": reason}


def observed_trace_tools(trace):
    """Include real bootstrap reads without calling them model-generated calls.

    Codaro records local reads outside `turns` and may mirror them in a turn.
    Prefer the dedicated receipts, excluding that mirrored role from model calls.
    """
    calls, local, retrieved = [], [], []
    for record in trace.get("local_retrievals", []):
        if not isinstance(record, dict):
            continue
        receipts = [record] if "name" in record else record.get("calls", [])
        for receipt in receipts:
            name = receipt.get("name")
            if isinstance(name, str) and len(calls) < 2000:
                calls.append(name)
                local.append(name)
    for turn in trace.get("turns", []):
        for action in turn.get("tool_results", []):
            message = action.get("message", {})
            name = message.get("name")
            if message.get("role") != "local_retrieval" and isinstance(name, str) and len(calls) < 2000:
                calls.append(name)
            for hit in action.get("result", {}).get("results", []):
                if isinstance(hit, dict) and isinstance(hit.get("path"), str) and len(retrieved) < 200:
                    retrieved.append(hit["path"])
    return calls, local, retrieved


def codaro_provider(settings):
    from codaro.llm.factory import create_provider
    if settings.api_style != "ollama":
        return create_provider(settings)
    from codaro.llm.ollama import Ollama

    class ComparisonOllama(Ollama):
        # Normalize sampling through the public codec, keeping Agent/transport.
        def wire_payload(self, payload):
            wire = super().wire_payload(payload)
            wire["options"]["temperature"] = 0
            return wire

    return ComparisonOllama(settings)


def codaro(job, config):
    from codaro.agent import Agent
    from codaro.features import DEFAULTS
    from codaro.llm.config import Settings
    from codaro.repository import Repository
    from codaro.trace_metrics import evaluation_metrics

    conditions, case = job["conditions"], job["case"]
    endpoint, model, api, key, reserve = connection(config, conditions)
    if api not in {"ollama", "openai", "anthropic"}:
        return unknown(conditions, "provider protocol unsupported by adapter")
    if case.get("checks"):
        return unknown(conditions, "named shell checks have no exact Codaro argv binding; not translated")
    # The Agent's command executor requires bubblewrap; no host-execution fallback.
    os.environ["CODARO_SANDBOX"] = "required"
    settings = Settings(base_url=endpoint, model=model, api_key=key,
                        api_style=api, context_window=conditions["context_window"],
                        max_output_tokens=reserve, timeout=min(case.get("timeout_seconds") or 180, 300),
                        context_source="explicit shared Opcode evaluation")
    root = Path(job["workspace"])
    mutable = set(case["files"])

    def approve(proposals, *_):
        return job.get("allow_execution") is True and case["mode"] == "execute" and all(
            proposal.path in mutable and not (root / proposal.path).is_symlink()
            for proposal in proposals
        )

    agent = Agent(Repository(root), codaro_provider(settings), mode=case["mode"],
                  persist_memory=False, max_steps=20,
                  max_seconds=case.get("timeout_seconds") or 180,
                  features={**DEFAULTS, "mcp": {}, "plugins": {},
                            "lsp": {"enabled": False, "servers": {}},
                            "semantic_search": False, "semantic_compaction": False},
                  approve_edit=approve, approve_command=lambda *_: False,
                  approve_external=lambda *_: False)
    response, failure = "", ""
    calls, local_calls, retrieved = [], [], []
    input_total = output_total = 0
    all_input = all_output = True
    for prompt in case["prompts"]:
        try:
            response = agent.ask(prompt)
        except (OSError, ValueError, RuntimeError):
            failure = "application run failed; payload omitted"
        trace_path = root / ".codaro" / "prompt.json"
        if trace_path.exists():
            trace = bounded_json(trace_path)
            metrics = evaluation_metrics(trace)
            i, o = metrics.get("reported_input_tokens"), metrics.get("reported_output_tokens")
            all_input = all_input and type(i) is int and metrics.get("metrics_complete") is True and metrics.get("usage_requests_covered") == metrics.get("http_attempts")
            all_output = all_output and type(o) is int and metrics.get("metrics_complete") is True and metrics.get("output_usage_requests_covered") == metrics.get("http_attempts")
            input_total += i or 0
            output_total += o or 0
            observed, local, paths = observed_trace_tools(trace)
            calls.extend(observed[:max(0, 2000-len(calls))])
            local_calls.extend(local[:max(0, 2000-len(local_calls))])
            retrieved.extend(paths[:max(0, 200-len(retrieved))])
        else:
            all_input = all_output = False
        if failure:
            break
    task = agent.tasks.current() or {}
    unavailable = ["task_receipts", "prompt_profile", "context_budget"]
    if not all_input:
        unavailable.append("input_tokens")
    if not all_output:
        unavailable.append("output_tokens")
    version = importlib.metadata.version("codaro")
    import codaro as installed_codaro
    source = Path(installed_codaro.__file__).resolve()
    for parent in list(source.parents)[:4]:
        if (parent / ".git").exists():
            try:
                revision = subprocess.run(["git", "-C", str(parent), "rev-parse", "--short=12", "HEAD"],
                                          stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=3, check=False).stdout.decode().strip()
                if re.fullmatch(r"[0-9a-f]{7,12}", revision):
                    version += "+" + revision
            except (OSError, subprocess.SubprocessError):
                pass
            break
    return {"conditions": conditions, "response": response[-64000:], "error": failure,
	    "temperature": 0 if api == "ollama" else None,
	    "effective_context_window": agent.context_window,
	    "effective_output_reserve": agent.max_output_tokens,
            "tools": calls, "local_tools": local_calls, "tool_calls": len(calls), "retrieved_paths": retrieved,
            "input_tokens": input_total if all_input else None,
            "output_tokens": output_total if all_output else None,
            "task": {"status": task.get("state", ""), "revision": task.get("revision", 0)},
            "unsupported_criteria": ["check_passed"],
            "metrics_unavailable": unavailable, "version": version}


def crush(job, config, binary):
    conditions, case = job["conditions"], job["case"]
    if case["mode"] != "ask":
        return unknown(conditions, "Crush CLI adapter only supports read-only Ask; no automatic approvals")
    endpoint, model, protocol, key, reserve = connection(config, conditions)
    if protocol not in {"openai", "anthropic"}:
        return unknown(conditions, "Crush uses a different wire protocol; not an identical protocol comparison")
    if not binary or not Path(binary).is_absolute() or not os.access(binary, os.X_OK):
        return unknown(conditions, "Crush executable unavailable")
    if os.name != "posix":
        return unknown(conditions, "bounded Crush subprocess adapter requires POSIX")

    def output_limit():
        import resource
        resource.setrlimit(resource.RLIMIT_FSIZE, (128 << 10, 128 << 10))
    # A private synthetic config excludes user MCP/LSP/hooks/skills and mutations.
    disabled = ["bash", "job_output", "job_kill", "write", "edit", "multiedit", "download",
                "fetch", "sourcegraph", "web_search", "web_fetch", "question", "lsp_rename", "lsp_replace_symbol", "agentic_fetch", "agent"]
    cfg = {"providers": {"evaluation": {"id": "evaluation", "name": "Evaluation",
             "type": protocol, "base_url": endpoint, "api_key": "$OPCODE_EVALUATION_PROVIDER_KEY",
             "discover_models": False,
             "models": [{"id": model, "name": model, "context_window": conditions["context_window"],
                         "default_max_tokens": reserve, "can_reason": False, "supports_attachments": False}]}},
           "models": {name: {"provider": "evaluation", "model": model, "max_tokens": reserve} for name in ("large", "small")},
           "mcp": {}, "lsp": {}, "hooks": {},
           "options": {"disable_default_providers": True, "disable_metrics": True,
                       "data_directory": str(Path(job["workspace"]).parent / "crush-state"),
                       "disabled_tools": disabled, "skills_paths": [], "context_paths": [], "global_context_paths": []}}
    path = Path(job["workspace"]) / "crush.json"
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w") as out:
        json.dump(cfg, out)
    response, failure = "", ""
    environment = {name: os.environ[name] for name in ("PATH", "HOME", "XDG_CONFIG_HOME", "LANG", "LC_ALL", "SHELL", "TMPDIR", "SSL_CERT_FILE", "SSL_CERT_DIR") if name in os.environ}
    environment["OPCODE_EVALUATION_PROVIDER_KEY"] = key
    for index, prompt in enumerate(case["prompts"]):
        args = [binary, "run", "--quiet", "--model", "evaluation/" + model]
        if index:
            args.append("--continue")
        # Temporary files cap disk/output; the parent imposes the whole case deadline.
        with open(Path(job["workspace"]).parent / "crush-output", "w+b") as capture:
            proc = subprocess.run(args, input=prompt.encode(), stdout=capture, stderr=subprocess.DEVNULL,
                                  timeout=case.get("timeout_seconds") or 180, check=False, preexec_fn=output_limit, env=environment)
            capture.seek(0)
            response = capture.read(64001).decode("utf-8", errors="replace")
        if proc.returncode or len(response) > 64000:
            failure = "Crush CLI failed or output exceeded bounds; payload omitted"
            break
    with open(Path(job["workspace"]).parent / "crush-version", "w+b") as capture:
        subprocess.run([binary, "--version"], stdout=capture, stderr=subprocess.DEVNULL, timeout=10, check=False, preexec_fn=output_limit, env=environment)
        capture.seek(0)
        version = capture.read(150).decode("utf-8", errors="replace")
    matched = re.search(r"\bv?\d+\.\d+\.\d+(?:[-+][a-zA-Z0-9.-]{1,40})?", version)
    return {"conditions": conditions, "response": response[-64000:], "error": failure,
            "tools": [], "input_tokens": None, "output_tokens": None, "tool_calls": None,
            "retrieved_paths": [], "version": matched.group(0) if matched else "",
            "unsupported_criteria": ["tool_called", "check_passed", "task_status"],
            "metrics_unavailable": ["input_tokens", "output_tokens", "tool_calls", "retrieval", "task_receipts", "prompt_profile", "context_budget"]}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--application", choices=("codaro", "crush"), required=True)
    parser.add_argument("--crush-command")
    args = parser.parse_args()
    raw = sys.stdin.buffer.read(LIMIT + 1)
    if len(raw) > LIMIT:
        raise ValueError("job exceeds bounds")
    job = json.loads(raw)
    _, config = validate_job(job)
    # Third-party libraries may print status text. Only the final JSON is stdout.
    with open(os.devnull, "w") as discarded, contextlib.redirect_stdout(discarded):
        try:
            result = codaro(job, config) if args.application == "codaro" else crush(job, config, args.crush_command)
        except (ImportError, importlib.metadata.PackageNotFoundError):
            result = unknown(job["conditions"], "application dependency unavailable")
        except (OSError, ValueError, RuntimeError, subprocess.SubprocessError, sqlite3.Error):
            result = {"conditions": job["conditions"], "response": "", "error": "adapter failed; payload omitted", "tools": []}
    json.dump(result, sys.stdout, ensure_ascii=False)


if __name__ == "__main__":
    main()
