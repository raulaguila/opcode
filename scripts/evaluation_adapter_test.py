"""Driver contract/security tests. Scripted provider is not a model evaluation."""
import contextlib
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("adapter", Path(__file__).with_name("evaluation_adapter.py"))
adapter = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(adapter)


@contextlib.contextmanager
def fixture():
    with tempfile.TemporaryDirectory(prefix="opcode-eval-") as directory:
        base = Path(directory).resolve()
        root = base / "workspace"
        root.mkdir()
        (root / "counter.py").write_text("def add(a, b):\n    return a - b\n")
        config = {"providers": {"local": {"baseURL": "http://127.0.0.1:11434/v1", "apiKeyEnv": "EVALUATION_TEST_KEY"}},
                  "models": {"local.test": {"api": "ollama", "api_model": "test", "provider": "local"}}}
        path = base / "config.json"
        path.write_text(json.dumps(config))
        job = {"workspace": str(root), "config": str(path), "allow_execution": False,
               "conditions": {"model": "local.test", "api_model": "test", "protocol": "ollama", "output_reserve": 2048, "context_window": 16384, "mode": "ask", "fixture_sha256": "a" * 64},
               "case": {"mode": "ask", "files": {"counter.py": "def add(a, b):\n    return a - b\n"},
                        "prompts": ["Leia counter.py e explique o erro. Cite o arquivo e a linha."], "checks": {}}}
        previous = Path.cwd()
        os.chdir(root)
        try:
            yield job, config
        finally:
            os.chdir(previous)


class DriverTests(unittest.TestCase):
    def test_local_read_receipts_are_counted_without_mirrored_duplicates(self):
        trace={"local_retrievals":[{"name":"read_lines","result":{"error":"missing"}},{"kind":"overview_recovery","calls":[{"name":"read_lines","result":{"path":"README.md"}}]}],
               "turns":[{"tool_results":[{"message":{"role":"local_retrieval","name":"read_lines"}},{"message":{"role":"tool","name":"read_symbol"}}]}]}
        calls,local,_=adapter.observed_trace_tools(trace)
        self.assertEqual(["read_lines","read_lines","read_symbol"],calls)
        self.assertEqual(["read_lines","read_lines"],local)

    def test_codaro_native_sampling_is_controlled_without_changing_transport(self):
        from codaro.llm.config import Settings
        provider = adapter.codaro_provider(Settings(base_url="http://127.0.0.1:11434/v1", model="test", api_style="ollama", context_window=16384))
        payload = {"model":"test", "messages":[], "temperature":0.1, "max_tokens":2048}
        wire = provider.wire_payload(payload)
        self.assertEqual(0, wire["options"]["temperature"])
        self.assertEqual(16384, wire["options"]["num_ctx"])
        self.assertEqual(2048, wire["options"]["num_predict"])
        self.assertEqual(0.1, payload["temperature"])
        self.assertIsNone(provider.transport)

    def test_rejects_original_workspace_and_unauthorized_execution(self):
        with fixture() as (job, _):
            adapter.validate_job(job)
            job["case"]["mode"] = job["conditions"]["mode"] = "execute"
            with self.assertRaises(ValueError):
                adapter.validate_job(job)
            job["case"]["mode"] = job["conditions"]["mode"] = "ask"
            job["workspace"] = str(Path.cwd().parent)
            with self.assertRaises(ValueError):
                adapter.validate_job(job)

    def test_connection_does_not_change_protocol_or_model(self):
        with fixture() as (job, config):
            with patch.dict(os.environ, {"EVALUATION_TEST_KEY": "private-value"}):
                self.assertEqual("private-value", adapter.connection(config, job["conditions"])[3])
            job["conditions"]["api_model"] = "different"
            with self.assertRaises(ValueError):
                adapter.connection(config, job["conditions"])

    def test_crush_never_enables_execute_or_substitutes_protocol(self):
        with fixture() as (job, config):
            self.assertIn("different wire protocol", adapter.crush(job, config, None)["unavailable"])
            job["case"]["mode"] = "execute"
            self.assertIn("read-only", adapter.crush(job, config, None)["unavailable"])

    def test_crush_observes_text_without_inventing_receipts_or_usage(self):
        if os.name != "posix":
            self.skipTest("POSIX driver")
        with fixture() as (job, config):
            config["models"]["local.test"]["api"] = job["conditions"]["protocol"] = "openai"
            binary = Path.cwd().parent / "crush"
            binary.write_text("#!/bin/sh\nif [ \"$1\" = --version ]; then printf 'crush v1.2.3'; else cat >/dev/null; printf 'counter.py:2 subtracts'; fi\n")
            binary.chmod(0o700)
            result = adapter.crush(job, config, str(binary))
            self.assertIn("counter.py:2", result["response"])
            self.assertIsNone(result["input_tokens"])
            self.assertIsNone(result["tool_calls"])
            self.assertIn("tool_called", result["unsupported_criteria"])
            cfg = json.loads((Path.cwd() / "crush.json").read_text())
            self.assertIn("bash", cfg["options"]["disabled_tools"])
            self.assertEqual("$OPCODE_EVALUATION_PROVIDER_KEY", cfg["providers"]["evaluation"]["api_key"])
            self.assertEqual(0o600, (Path.cwd() / "crush.json").stat().st_mode & 0o777)

    def test_codaro_public_api_reads_real_source_with_scripted_provider(self):
        try:
            import codaro.llm.factory
        except ImportError:
            self.skipTest("run with Codaro's installed Python environment for public API compatibility")
        with fixture() as (job, config):
            class Scripted:
                def __init__(self, settings):
                    self.settings = settings
                    self.responses = iter([
                        {"role": "assistant", "content": None, "tool_calls": [{"id": "source-read", "type": "function", "function": {"name": "read_lines", "arguments": json.dumps({"path": "counter.py", "start": 1, "end": 2})}}]},
                        {"role": "assistant", "content": "counter.py:2 usa subtração em add."},
                    ])

                def complete(self, messages, tools=None):
                    return next(self.responses)

            with patch.object(adapter, "codaro_provider", side_effect=Scripted):
                result = adapter.codaro(job, config)
            self.assertIn("read_lines", result["tools"])
            self.assertIn("counter.py:2", result["response"])
            self.assertIsNone(result["input_tokens"])
            self.assertEqual("required", os.environ["CODARO_SANDBOX"])
            self.assertEqual(job["case"]["files"]["counter.py"], (Path.cwd() / "counter.py").read_text())


if __name__ == "__main__":
    unittest.main()
