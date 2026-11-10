"""Opt-in catalogue → released helper → user files/tools proof; synthetic data only."""
import hashlib
import io
import json
import os
from pathlib import Path
import queue
import re
import subprocess
import sys
import threading
import tarfile
import time
import uuid

recipe, arch = sys.argv[1:]
assert re.fullmatch(r"[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*/[a-z0-9]+(-[a-z0-9]+)*", recipe)
assert arch in {"amd64", "arm64"}
work = Path(__file__).resolve().parent
evidence = work / "evidence"
evidence.mkdir(exist_ok=True)
prefix = ["docker", "exec", "--workdir", str(work), "--env", "HOME=/root", "--env", "UGT_RUNTIME=host", "layer"]
ugt = prefix + [str(work / "ugt")]


def command(args, *, cwd=None, timeout=1200, check=True):
    result = subprocess.run(args, cwd=cwd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    if check and result.returncode:
        raise AssertionError(f"{args}: {result.stderr.decode(errors='replace')[-6000:]}")
    return result


def fetch(url):
    return command(["curl", "-fsSL", "--max-time", "30", url], timeout=40).stdout


def save(name, value):
    (evidence / name).write_text(json.dumps(value, indent=2) + "\n")


class MCP:
    def __init__(self):
        self.log = (evidence / f"mcp-{uuid.uuid4().hex[:8]}.log").open("wb")
        self.process = subprocess.Popen(prefix[:2] + ["-i"] + prefix[2:] + [str(work / "ugt"), "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=self.log)
        self.messages = queue.Queue()
        self.next = 0
        threading.Thread(target=self.read, daemon=True).start()
        self.rpc("initialize", {"protocolVersion": "2025-11-25", "capabilities": {}, "clientInfo": {"name": "ugt-catalogue-check", "version": "1"}})
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
        self.next += 1
        key = self.next
        self.send({"jsonrpc": "2.0", "id": key, "method": method, "params": params})
        deadline = time.monotonic() + 150
        while True:
            try:
                message = self.messages.get(timeout=max(0.1, deadline - time.monotonic()))
            except queue.Empty as error:
                raise TimeoutError(f"Released MCP did not answer {method}; see the MCP log") from error
            assert message is not None, "Released MCP server exited"
            if message.get("id") == key:
                assert "error" not in message, message
                return message["result"]

    def tool(self, suffix, arguments):
        for _ in range(20):
            tools = self.rpc("tools/list", {})["tools"]
            matches = [t for t in tools if t["name"].endswith(suffix) and not t["name"].startswith("ugt_")]
            if len(matches) == 1:
                break
            time.sleep(1)
        assert len(matches) == 1, f"Could not find provided tool {suffix}: {[t['name'] for t in tools]}"
        result = self.rpc("tools/call", {"name": matches[0]["name"], "arguments": arguments})
        assert not result.get("isError"), result
        return result

    def close(self):
        self.process.stdin.close()
        try:
            self.process.wait(timeout=20)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.wait()
        self.log.close()


def local_http(url, *args):
    return command(prefix + ["curl", "-fsSL", "--max-time", "90", *args, url], timeout=100).stdout


def run(inputs, instance=None):
    args = ugt + ["run", "--json", "--to", str(work / "results")]
    if instance:
        args += ["--instance", instance]
    for item in inputs:
        args += ["--input", item]
    args += ["/".join(recipe.split("/")[:2]), recipe.split("/")[2]]
    result = command(args, timeout=1500)
    (evidence / f"run-{instance or 'default'}.log").write_bytes(result.stderr)
    value = json.loads(result.stdout)
    assert value["passed"] and value["source"] == "catalogue", value
    assert not any(s["name"] == "build" for s in value["steps"]), "An admitted prebuilt unexpectedly built locally"
    save(f"run-{instance or 'default'}.json", value)
    return value


started = time.monotonic()
try:
    project = "/".join(recipe.split("/")[:2])
    entry = next(r for r in json.loads(fetch("https://api.usegeekthing.com/v1/projects/" + project))["recipes"] if r["recipe"] == recipe)
    save("catalogue.json", entry)
    generated = work / "generated"
    folder = generated / "recipe"
    folder.mkdir(parents=True)
    for name in ["inputs", "outputs"]:
        (generated / name).mkdir()
    for file in entry["files"]:
        relative = Path(file["path"])
        assert not relative.is_absolute() and ".." not in relative.parts and file["mode"] in {"100644", "100755"}
        content = fetch("https://api.usegeekthing.com/v1/blobs/" + file["sha"])
        assert hashlib.sha1(b"blob " + str(len(content)).encode() + b"\0" + content).hexdigest() == file["sha"]
        target = folder / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(content)
        target.chmod(0o755 if file["mode"] == "100755" else 0o644)
    images = entry.get("images", {})
    image = images.get("", {}).get("ref", "ugt-smoke:upstream")
    for value in images.values():
        assert arch in value["arches"]
        exact = value["ref"] + "@" + value["digest"]
        command(prefix + ["docker", "pull", exact])
        command(prefix + ["docker", "tag", exact, value["ref"]])
    compose = prefix[:-1] + ["--env", f"UGT_IMAGE={image}", "--env", f"UGT_ARCH={arch}", "layer", "docker", "compose", "--project-name", "sample", "--project-directory", str(generated), "-f", str(folder / "compose.yaml")]
    config = json.loads(command(compose + ["config", "--format", "json"]).stdout)
    if any(s.get("build") for s in config["services"].values()):
        assert images, "Wait for this recipe's verified prebuilt images before running the smoke"
    command(compose + ["--profile", "sample", "run", "--rm", "--no-build", "sample"])
    definition = json.loads((folder / "recipe.json").read_text())
    inputs = [f"{i['path']}={generated / i['path']}" for i in definition.get("inputs", [])]
    assert all(Path(i.split("=", 1)[1]).exists() for i in inputs), "The sample does not supply the actual declared input"
    result = run(inputs)
    assert len(result.get("commands", [])) == len(definition.get("provides", {}).get("commands", []))
    assert len(result.get("mcp_servers", [])) == len(definition.get("provides", {}).get("mcp_servers", []))
    for provided in definition.get("provides", {}).get("commands", []):
        flag = "--version" if provided["name"] == "scrapling" else "--help"
        response = command(ugt + ["command", provided["name"], flag], check=False)
        assert response.returncode == 0 or (provided["name"] == "ghidra-headless" and b"analyzeHeadless" in response.stdout + response.stderr), response.stderr.decode(errors="replace")
    if definition.get("provides", {}).get("mcp_servers"):
        fixture = work / "fixture"
        fixture.mkdir()
        (fixture / "index.html").write_text('<title>UGT handoff</title><h1>UGT handoff works</h1><input id="name"><button onclick="document.querySelector(\'#result\').textContent=document.querySelector(\'#name\').value">Save</button><p id="result">Initial</p>')
        command(["docker", "exec", "-d", "layer", "python3", "-m", "http.server", "8321", "--bind", "0.0.0.0", "--directory", str(fixture)])
        network = json.loads(command(prefix + ["docker", "network", "inspect", "bridge"]).stdout)[0]
        url = "http://" + network["IPAM"]["Config"][0]["Gateway"] + ":8321/"
        client = MCP()
        try:
            if "agent-browser" in recipe:
                client.tool("agent_browser_open", {"url": url})
                client.tool("agent_browser_fill", {"selector": "#name", "text": "Orchid"})
                client.tool("agent_browser_click", {"selector": "button"})
                assert "Orchid" in json.dumps(client.tool("agent_browser_get_text", {"selector": "#result"}))
            elif "scrapling" in recipe:
                assert "UGT handoff works" in json.dumps(client.tool("_get", {"url": url}))
            elif "gbrain" in recipe:
                entity = "ugt-handoff-" + uuid.uuid4().hex
                saved = json.loads(client.tool("_remember", {"fact": "The greenhouse door is violet.", "provenance": "UGT synthetic catalogue check", "entity": entity})["content"][0]["text"])
                client.close()
                command(ugt + ["stop", recipe])
                client = MCP()
                assert saved["id"] in json.dumps(client.tool("_recall", {"entity": entity})), "Memory did not survive stop and a new agent connection"
                client.tool("_forget", {"id": saved["id"], "reason": "synthetic catalogue check cleanup"})
        finally:
            client.close()
    if result["kind"] == "service":
        page = result["pages"][0]["url"]
        origin = page.split("/", 3)[:3]
        base = "/".join(origin)
        path = work / "persistent.md"
        path.write_text("# Persistent reading\n\nThe greenhouse door is violet.\n")
        material = json.loads(local_http(base + "/api/reading/materials", "-F", f"file=@{path};type=text/markdown"))
        command(ugt + ["stop", recipe])
        other = run([], "separate")
        other_base = "/".join(other["pages"][0]["url"].split("/", 3)[:3])
        status = command(prefix + ["curl", "-sS", "-o", "/dev/null", "-w", "%{http_code}", other_base + "/api/reading/materials/" + material["material_id"]]).stdout
        assert status == b"404", "Named installation could read another installation's document"
        command(ugt + ["stop", recipe + "@separate"])
        reopened = run([])
        assert reopened["pages"][0]["url"] == page, "The app's address changed on reopening"
        persisted = json.loads(local_http(base + "/api/reading/materials/" + material["material_id"]))
        assert persisted["material_id"] == material["material_id"], "Saved document was lost on reopening"
        local_http(base + "/api/reading/materials/" + material["material_id"], "-X", "DELETE")
    results = work / "results"
    if results.exists():
        # The inner user owns its results. Read through that user and write
        # artifacts as the outer runner, without changing source permissions.
        raw = command(prefix + ["tar", "-C", str(results), "-cf", "-", "."]).stdout
        with tarfile.open(fileobj=io.BytesIO(raw)) as archive:
            total = 0
            for member in archive:
                relative = Path(member.name)
                assert not relative.is_absolute() and ".." not in relative.parts
                if member.isdir():
                    continue
                assert member.isfile() and member.size <= 20 * 1024 * 1024
                total += member.size
                assert total <= 100 * 1024 * 1024
                target = evidence / "results" / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(archive.extractfile(member).read())
    command(ugt + ["stop", recipe])
    save("result.json", {"recipe": recipe, "arch": arch, "passed": True, "seconds": time.monotonic() - started})
except Exception as error:
    save("result.json", {"recipe": recipe, "arch": arch, "passed": False, "error": str(error), "seconds": time.monotonic() - started})
    raise
