"""Use REA's own MCP workflows and export its authenticated investigation."""
import hashlib
import json
import os
from pathlib import Path
import queue
import subprocess
import sys
import threading
import time


class MCP:
    def __init__(self):
        self.process = subprocess.Popen(["rea", "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=None)
        self.messages = queue.Queue()
        self.sequence = 0
        threading.Thread(target=self.read, daemon=True).start()
        self.rpc("initialize", {"protocolVersion": "2025-11-25", "capabilities": {}, "clientInfo": {"name": "ugt-recipe", "version": "1"}})
        self.send({"jsonrpc": "2.0", "method": "notifications/initialized"})

    def read(self):
        for line in self.process.stdout:
            try:
                self.messages.put(json.loads(line))
            except json.JSONDecodeError:
                continue
        self.messages.put(None)

    def send(self, value):
        self.process.stdin.write((json.dumps(value) + "\n").encode())
        self.process.stdin.flush()

    def rpc(self, method, params):
        self.sequence += 1
        key = self.sequence
        self.send({"jsonrpc": "2.0", "id": key, "method": method, "params": params})
        seconds = int(os.environ.get("REA_TIMEOUT", "600"))
        assert 10 <= seconds <= 1800, "REA_TIMEOUT must be 10–1800 seconds"
        deadline = time.monotonic() + seconds
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise TimeoutError(f"REA did not complete {method} within {seconds} seconds")
            message = self.messages.get(timeout=remaining)
            assert message is not None, "REA exited before answering"
            if message.get("id") == key:
                assert "error" not in message, json.dumps(message)[:4000]
                return message["result"]

    def tool(self, name, arguments, *, error=False):
        result = self.rpc("tools/call", {"name": name, "arguments": arguments})
        if error:
            assert result.get("isError"), f"REA unexpectedly accepted {name}: {str(result)[:1000]}"
            return result
        assert not result.get("isError"), json.dumps(result)[:6000]
        value = result.get("structuredContent")
        if value is None:
            value = json.loads(next(c["text"] for c in result["content"] if c["type"] == "text"))
        return value

    def close(self):
        self.process.stdin.close()
        try:
            status = self.process.wait(timeout=60)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.wait()
            raise AssertionError("REA did not cleanly stop its owned process family")
        assert status == 0, f"REA exited with status {status}"


def inventory(source):
    files = sorted(p for p in source.rglob("*") if p.is_file()) if source.is_dir() else [source]
    assert files, "The input is empty"
    return [{"path": str(p.relative_to(source)) if source.is_dir() else source.name, "sha256": hashlib.sha256(p.read_bytes()).hexdigest()} for p in files]


def target(root):
    base = (root / "inputs/target.asar").resolve()
    assert base.exists(), "Supply a program, application folder or ASAR archive"
    entry = os.environ.get("REA_ENTRY", "")
    source = (base / entry).resolve() if entry else base
    assert source == base or base in source.parents, "REA_ENTRY must stay inside the supplied folder"
    assert source.exists(), "REA_ENTRY does not name an existing file or folder"
    return source


def authenticated_view(bundle, evidence_id):
    request = {"source": {"kind": "evidence-file", "path": str(bundle), "evidence_id": evidence_id}, "view": {"kind": "summary"}}
    result = subprocess.run(["rea", "inspect-analysis-view", json.dumps(request), "--json"], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=int(os.environ.get("REA_TIMEOUT", "600")))
    assert result.returncode == 0, result.stderr.decode(errors="replace") + result.stdout.decode(errors="replace")[:1500]
    return json.loads(result.stdout)


def native(client, source, procedure=None, *, open_target=True):
    if open_target:
        client.tool("open_binary", {"path": str(source), "provider_id": "ghidra"})
    listed = client.tool("list_procedures", {})
    rows = listed["normalized_result"]
    assert isinstance(rows, list) and rows, "Ghidra recovered no functions"
    local = [r for r in rows if r.get("procedure", {}).get("external") is False and r.get("procedure", {}).get("thunk") is False]
    assert local, "No local function is available for the first report"
    preferred = next((r for r in local if r["value"] in {"main", "_main"}), local[0])
    selected = procedure or os.environ.get("REA_PROCEDURE", "") or preferred["address"]
    code = client.tool("procedure_pseudo_code", {"procedure": selected})
    text = code["normalized_result"]
    assert isinstance(text, str) and text.strip(), f"Ghidra returned no pseudocode for {selected}: {json.dumps(code)[:4000]}; choose another REA_PROCEDURE"
    assert hashlib.sha256(source.read_bytes()).hexdigest() in json.dumps(code), "Native evidence does not identify the supplied bytes"
    return code, f"Ghidra recovered {len(rows)} functions. Initial procedure: {selected}\n\n{text}\n"


def javascript(client, source):
    # Keep the complete graph in REA's investigation and export it to a file;
    # large applications must not require one enormous MCP response frame.
    value = client.tool("analyze_javascript_application", {"input_path": str(source), "detail": "summary"})
    stats = value["normalized_result"]["summary"]["statistics"]
    assert stats["parsed_javascript_files"] > 0, "REA parsed no JavaScript; a partial error is not a completed analysis"
    return value, f"JavaScript application: {source.name}\n\n" + json.dumps(stats, indent=2) + "\n\nRead summary.json and the saved evidence for coverage and limitations.\n"


def run(root, mode=None, procedure=None):
    source = target(root)
    chosen = mode or os.environ.get("REA_MODE", "auto")
    assert chosen in {"auto", "native", "javascript"}, "REA_MODE must be auto, native or javascript"
    if chosen == "auto":
        chosen = "javascript" if source.is_dir() else "native"
    output = root / "outputs"
    output.mkdir(parents=True, exist_ok=True)
    for name in ["analysis.json", "summary.json", "report.txt", "manifest.json"]:
        (output / name).unlink(missing_ok=True)
    client = MCP()
    try:
        evidence, report = native(client, source, procedure) if chosen == "native" else javascript(client, source)
        evidence_id = evidence["normalized_result"]["parent_evidence_id"] if chosen == "javascript" else evidence["evidence_id"]
        client.tool("export_evidence_bundle", {"path": str(output / "analysis.json")})
        client.tool("close_binary", {})
    finally:
        client.close()
    summary = authenticated_view(output / "analysis.json", evidence_id)
    (output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    (output / "report.txt").write_text(report)
    (output / "manifest.json").write_text(json.dumps({"mode": chosen, "files": inventory(source), "evidence_id": evidence_id, "report_sha256": hashlib.sha256(report.encode()).hexdigest()}, indent=2) + "\n")


def check(root):
    output = root / "outputs"
    record = json.loads((output / "manifest.json").read_text())
    assert record["files"] == inventory(target(root)), "The analysis belongs to different input files"
    summary = authenticated_view(output / "analysis.json", record["evidence_id"])
    assert summary == json.loads((output / "summary.json").read_text()), "Saved summary does not describe the authenticated evidence"
    assert hashlib.sha256((output / "report.txt").read_bytes()).hexdigest() == record["report_sha256"], "The delivered report changed"


if __name__ == "__main__":
    try:
        (check if len(sys.argv) > 1 and sys.argv[1] == "check" else run)(Path("/work"))
    except Exception as error:
        import traceback
        traceback.print_exc()
        raise SystemExit(1)
