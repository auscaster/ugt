"""Opt-in catalogue → released helper → user files/tools proof; synthetic data only."""
import hashlib
import io
import json
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
    print("Running:", " ".join(map(str, args)), flush=True)
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
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise queue.Empty
                message = self.messages.get(timeout=remaining)
            except queue.Empty as error:
                raise TimeoutError(f"Released MCP did not answer {method}; see the MCP log") from error
            assert message is not None, "Released MCP server exited"
            if message.get("id") == key:
                assert "error" not in message, message
                return message["result"]

    def tool(self, name, arguments, *, error=False):
        for _ in range(20):
            tools, cursor = [], None
            for _page in range(100):
                page = self.rpc("tools/list", {"cursor": cursor} if cursor else {})
                tools.extend(page["tools"])
                cursor = page.get("nextCursor")
                if not cursor:
                    break
            assert not cursor, "Provided tool list exceeded one hundred pages"
            matches = [t for t in tools if t["name"] == recipe.split("/")[2] + "_" + name]
            if len(matches) == 1:
                break
            time.sleep(1)
        save("provided-tools.json", tools)
        assert len(matches) == 1, f"Could not find provided tool {name}: {[t['name'] for t in tools]}"
        result = self.rpc("tools/call", {"name": matches[0]["name"], "arguments": arguments})
        assert bool(result.get("isError")) == error, result
        return result

    def native(self, name, arguments):
        result = self.rpc("tools/call", {"name": name, "arguments": arguments})
        assert not result.get("isError"), result
        return result.get("structuredContent") or json.loads(result["content"][0]["text"])

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


def run(inputs, instance=None, label=None, values=None):
    tag = label or instance or "default"
    destination = work / ("results-" + label if label else "results")
    args = ugt + ["run", "--json", "--to", str(destination)]
    if instance:
        args += ["--instance", instance]
    for key, value in (values or {}).items():
        args += ["--set", key + "=" + value]
    for item in inputs:
        args += ["--input", item]
    args += ["/".join(recipe.split("/")[:2]), recipe.split("/")[2]]
    result = command(args, timeout=1500, check=False)
    (evidence / f"run-{tag}.log").write_bytes(result.stderr)
    (evidence / f"run-{tag}.json").write_bytes(result.stdout)
    value = json.loads(result.stdout)
    assert result.returncode == 0, value
    assert value["passed"] and value["source"] == "catalogue", value
    assert value["revision"] == entry["revision"], "Catalogue changed during this run"
    assert not any(s["name"] == "build" for s in value["steps"]), "An admitted prebuilt unexpectedly built locally"
    save(f"run-{tag}.json", value)
    return value


def pdf(text):
    content = f"BT /F1 18 Tf 72 720 Td ({text}) Tj ET\n".encode()
    objects = [b"<< /Type /Catalog /Pages 2 0 R >>", b"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
               b"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
               b"<< /Length " + str(len(content)).encode() + b" >>\nstream\n" + content + b"endstream",
               b"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"]
    data = bytearray(b"%PDF-1.4\n")
    offsets = [0]
    for number, obj in enumerate(objects, 1):
        offsets.append(len(data))
        data.extend(f"{number} 0 obj\n".encode() + obj + b"\nendobj\n")
    start = len(data)
    data.extend(f"xref\n0 {len(offsets)}\n0000000000 65535 f \n".encode())
    for offset in offsets[1:]:
        data.extend(f"{offset:010d} 00000 n \n".encode())
    data.extend(f"trailer\n<< /Size {len(offsets)} /Root 1 0 R >>\nstartxref\n{start}\n%%EOF\n".encode())
    return data


def decoded(result):
    return result.get("structuredContent") or json.loads(result["content"][0]["text"])


def rea_handoff(definition, image):
    def invoke(client, name, arguments):
        return decoded(client.tool(name, arguments))

    def code(client, expected):
        value = invoke(client, "procedure_pseudo_code", {"procedure": "plant_count"})
        text = value["normalized_result"]
        assert re.search(r"\+\s*(?:0x" + format(expected, "x") + r"|" + str(expected) + r")\b", text), text
        return value

    # All native tools here pass through the released helper's root gateway.
    client = MCP()
    try:
        guidance = invoke(client, "guided_workflows", {"action": "list"})
        assert guidance, "REA's workflow guidance was unavailable"
        invoke(client, "open_binary", {"path": "/work/inputs/target.asar", "provider_id": "ghidra"})
        first = code(client, 7)
        client.tool("procedure_pseudo_code", {"procedure": "ugt_missing_function_8734"}, error=True)
        code(client, 7)
        view = invoke(client, "inspect_analysis_view", {"source": {"kind": "retained-evidence", "evidence_id": first["evidence_id"]}, "view": {"kind": "summary"}})
        assert view["normalized_result"]["parent_evidence_id"] == first["evidence_id"]
        invoke(client, "annotate_native_function", {"procedure": "plant_count", "name": "ugt_handoff_count"})
        invoke(client, "export_evidence_bundle", {"path": "/work/outputs/handoff-evidence.json"})
        invoke(client, "close_binary", {})
    finally:
        client.close()
    command(ugt + ["stop", recipe])
    client = MCP()
    try:
        invoke(client, "open_binary", {"path": "/work/inputs/target.asar", "provider_id": "ghidra"})
        rows = invoke(client, "list_procedures", {})["normalized_result"]
        assert any(r["value"] == "ugt_handoff_count" for r in rows), "Native annotations did not survive a stop and new agent connection"
        invoke(client, "import_evidence_bundle", {"path": "/work/outputs/handoff-evidence.json"})
        imported = invoke(client, "inspect_analysis_view", {"source": {"kind": "retained-evidence", "evidence_id": first["evidence_id"]}, "view": {"kind": "summary"}})
        assert imported["normalized_result"]["parent_evidence_id"] == first["evidence_id"]
        invoke(client, "annotate_native_function", {"procedure": "ugt_handoff_count", "name": "plant_count"})
        invoke(client, "close_binary", {})
        second_input = work / "native-two.so"
        command(prefix + ["docker", "run", "--rm", "--volume", f"{work}:{work}", "--entrypoint", "cp", image, "/samples/native-two.so", str(second_input)])
        changed = run([definition["inputs"][0]["path"] + "=" + str(second_input)], label="changed-native")
        # Keep the same root MCP connection across replacing the staged input.
        invoke(client, "open_binary", {"path": "/work/inputs/target.asar", "provider_id": "ghidra"})
        second = code(client, 11)
        assert second["evidence_id"] != first["evidence_id"]
        invoke(client, "close_binary", {})
        library = work / "production-libz.so"
        extract = "import glob,shutil,sys;paths=glob.glob('/lib/*-linux-gnu/libz.so.1');assert len(paths)==1;shutil.copyfile(paths[0],sys.argv[1])"
        command(prefix + ["docker", "run", "--rm", "--volume", f"{work}:{work}", "--entrypoint", "python3", image, "-c", extract, str(library)])
        production = run([definition["inputs"][0]["path"] + "=" + str(library)], label="production-native", values={"REA_PROCEDURE": "compress2"})
        invoke(client, "open_binary", {"path": "/work/inputs/target.asar", "provider_id": "ghidra"})
        compressor = invoke(client, "procedure_pseudo_code", {"procedure": "compress2"})
        assert "deflate" in compressor["normalized_result"], "The real compressor's calls were not recovered through MCP"
        invoke(client, "close_binary", {})
        app = work / "bundled-app"
        command(prefix + ["docker", "run", "--rm", "--volume", f"{work}:{work}", "--entrypoint", "cp", image, "-a", "/samples/react-app", str(app)])
        size = len(command(prefix + ["cat", str(app / "assets/app.js")]).stdout)
        assert size > 100000
        bundled = run([definition["inputs"][0]["path"] + "=" + str(app)], label="bundled-react")
        manifest = json.loads(command(prefix + ["cat", str(work / "results-bundled-react/manifest.json")]).stdout)
        request = {"source": {"kind": "evidence-file", "path": str(work / "results-bundled-react/analysis.json"), "evidence_id": manifest["evidence_id"]}, "view": {"kind": "summary"}}
        cli = json.loads(command(ugt + ["command", "rea", "inspect-analysis-view", json.dumps(request), "--json"]).stdout)
        mcp = invoke(client, "inspect_analysis_view", {**request, "source": {**request["source"], "path": "/work/outputs/analysis.json"}})
        assert cli["normalized_result"]["parent_evidence_id"] == mcp["normalized_result"]["parent_evidence_id"] == manifest["evidence_id"]
        save("rea-workflows.json", {"guidance": guidance, "native_first": first, "native_changed": second, "changed_run": changed, "production_native_run": production, "production_native_evidence": compressor, "production_bundle_bytes": size, "production_run": bundled, "saved_evidence_cli": cli, "saved_evidence_mcp": mcp, "checkpoint_reopened": True, "failed_request_recovered_same_session": True, "root_connection_survived_new_inputs": True})
    finally:
        client.close()


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
    config = json.loads(command(compose + ["--profile", "*", "config", "--format", "json"]).stdout)
    if any(s.get("build") for s in config["services"].values()):
        assert images, "Wait for this recipe's verified prebuilt images before running the smoke"
    save("compose.json", config)
    sample = command(compose + ["--profile", "sample", "run", "--rm", "sample"])
    (evidence / "sample.log").write_bytes(sample.stdout + sample.stderr)
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
    if "google-deepmind/mujoco/" in recipe:
        script = work / "custom-simulation.py"
        script.write_text("import json,mujoco\nm=mujoco.MjModel.from_xml_string('<mujoco><worldbody><body pos=\"0 0 1\"><freejoint/><geom type=\"sphere\" size=\"0.1\"/></body></worldbody></mujoco>')\nd=mujoco.MjData(m)\nfor _ in range(100):mujoco.mj_step(m,d)\nprint(json.dumps({'time':float(d.time),'height':float(d.qpos[2])}))\n")
        state = json.loads(command(ugt + ["command", "mujoco-python", str(script)]).stdout)
        assert state["time"] > 0.1 and state["height"] < 1, "Custom Python controller could not use the installed API"
        save("custom-python.json", state)
    if recipe == "morluto/rea/inspect":
        rea_handoff(definition, image)
    if definition.get("provides", {}).get("mcp_servers") and "morluto/rea/" not in recipe:
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
                assert "UGT handoff works" in json.dumps(client.tool("make_request", {"url": url, "main_content_only": False}))
                output = work / "public-page.md"
                command(ugt + ["command", "scrapling", "extract", "get", "https://github.com/usegeekthing/ugt", str(output)])
                page_text = command(prefix + ["cat", str(output)]).stdout
                assert b"shows you how to use it again" in page_text
                save("public-page.json", {"url": "https://github.com/usegeekthing/ugt", "bytes": len(page_text), "own_project_text": True})
            elif "gbrain" in recipe:
                entity = "ugt-handoff-" + uuid.uuid4().hex
                saved = json.loads(client.tool("remember", {"fact": "The greenhouse door is violet.", "provenance": "UGT synthetic catalogue check", "entity": entity})["content"][0]["text"])
                client.close()
                command(ugt + ["stop", recipe])
                client = MCP()
                assert saved["id"] in json.dumps(client.tool("recall", {"entity": entity})), "Memory did not survive stop and a new agent connection"
                client.tool("forget", {"id": saved["id"], "reason": "synthetic catalogue check cleanup"})
        finally:
            client.close()
    if "scrapling" in recipe or "sherlock" in recipe:
        # Desktop AI apps can supply these requests without file-system access.
        client = MCP()
        try:
            request = (generated / definition["inputs"][0]["path"]).read_text()
            answer = client.native("ugt_run", {"project": project, "task": recipe.split("/")[2], "texts": {definition["inputs"][0]["path"]: request}, "to": str(work / "results-texts")})
            deadline = time.monotonic() + 300
            while answer["state"] in {"queued", "running"}:
                assert time.monotonic() < deadline, answer
                answer = client.native("ugt_status", {"job": answer["job"], "wait_seconds": 40})
            assert answer["state"] == "succeeded" and answer["result"]["passed"], answer
            assert answer["result"]["source"] == "catalogue" and answer["result"]["revision"] == entry["revision"]
            assert not any(s["name"] == "build" for s in answer["result"]["steps"])
            save("run-texts.json", answer)
        finally:
            client.close()
    if result["kind"] == "service":
        page = result["pages"][0]["url"]
        origin = page.split("/", 3)[:3]
        base = "/".join(origin)
        path = work / "persistent.pdf"
        path.write_bytes(pdf("The greenhouse door is violet."))
        material = json.loads(local_http(base + "/api/reading/materials", "-F", f"file=@{path};type=application/pdf"))
        unit = json.loads(local_http(base + "/api/reading/materials/" + material["material_id"] + "/units/1"))
        assert "greenhouse door is violet" in unit["text"], "Uploaded PDF was not readable"
        command(ugt + ["stop", recipe])
        other = run([], "separate")
        other_base = "/".join(other["pages"][0]["url"].split("/", 3)[:3])
        status = command(prefix + ["curl", "-sS", "-o", "/dev/null", "-w", "%{http_code}", other_base + "/api/reading/materials/" + material["material_id"]]).stdout
        assert status == b"404", "Named installation could read another installation's document"
        command(ugt + ["stop", recipe + "@separate"])
        reopened = run([], label="reopened")
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
