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

    def tool(self, name, arguments):
        for _ in range(20):
            tools, cursor = [], None
            for _page in range(10):
                page = self.rpc("tools/list", {"cursor": cursor} if cursor else {})
                tools.extend(page["tools"])
                cursor = page.get("nextCursor")
                if not cursor:
                    break
            assert not cursor, "Provided tool list exceeded ten pages"
            matches = [t for t in tools if t["name"] == recipe.split("/")[2] + "_" + name]
            if len(matches) == 1:
                break
            time.sleep(1)
        assert len(matches) == 1, f"Could not find provided tool {name}: {[t['name'] for t in tools]}"
        result = self.rpc("tools/call", {"name": matches[0]["name"], "arguments": arguments})
        assert not result.get("isError"), result
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


def run(inputs, instance=None, label=None):
    tag = label or instance or "default"
    destination = work / ("results-" + label if label else "results")
    args = ugt + ["run", "--json", "--to", str(destination)]
    if instance:
        args += ["--instance", instance]
    for item in inputs:
        args += ["--input", item]
    args += ["/".join(recipe.split("/")[:2]), recipe.split("/")[2]]
    result = command(args, timeout=1500)
    (evidence / f"run-{tag}.log").write_bytes(result.stderr)
    value = json.loads(result.stdout)
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
    if "sample" in config["services"]:
        command(compose + ["--profile", "sample", "run", "--rm", "sample"])
    definition = json.loads((folder / "recipe.json").read_text())
    inputs = [f"{i['path']}={generated / i['path']}" for i in definition.get("inputs", [])]
    assert all(Path(i.split("=", 1)[1]).exists() for i in inputs), "The sample does not supply the actual declared input"
    result = run(inputs)
    if "morluto/rea/" in recipe:
        # A shipped production bundle exercises substantially more than the
        # three-module installation sample, without executing its app code.
        build, app = work / "react-build", work / "bundled-app"
        build.mkdir()
        app.mkdir()
        (build / "app.jsx").write_text("import React,{useState} from 'react';import{createRoot}from'react-dom/client';function App(){const[n,setN]=useState(0);return <main><h1>Plant inventory</h1><button onClick={()=>setN(n+1)}>Count {n}</button></main>}createRoot(document.getElementById('root')).render(<App/>);")
        (app / "package.json").write_text(json.dumps({"name": "plant-inventory", "version": "1.0.0", "type": "module", "main": "assets/app.js"}))
        (app / "index.html").write_text('<!doctype html><title>Plant inventory</title><div id="root"></div><script type="module" src="assets/app.js"></script>')
        command(prefix + ["docker", "run", "--rm", "--volume", f"{work}:{work}", "--workdir", str(build), "--entrypoint", "sh", image,
                          "-ec", 'npm install --no-audit --no-fund react@19.2.0 react-dom@19.2.0 esbuild@0.25.11 && ./node_modules/.bin/esbuild app.jsx --bundle --minify --sourcemap --format=esm --legal-comments=linked --outfile="$1/assets/app.js"', "smoke", str(app)])
        size = len(command(prefix + ["cat", str(app / "assets/app.js")]).stdout)
        assert size > 100000, "The production bundle is not representative"
        real = run([definition["inputs"][0]["path"] + "=" + str(app)], label="bundled-react")
        view = {"source": {"kind": "evidence-file", "path": str(work / "results-bundled-react/app-evidence.json")}, "view": {"kind": "summary"}}
        summary = json.loads(command(ugt + ["command", "rea", "inspect-analysis-view", json.dumps(view), "--json"]).stdout)
        save("bundled-react.json", {"bundle_bytes": size, "result": real, "summary": summary})
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
                assert "UGT handoff works" in json.dumps(client.tool("get", {"url": url}))
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
