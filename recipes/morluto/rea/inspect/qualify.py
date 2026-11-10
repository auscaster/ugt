"""Real native, checkpoint and JavaScript cases; no mock analysis provider."""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

from analysis import MCP, check, inventory, native, run


def native_code(client, path, count, name="plant_count", *, open_target=True):
    evidence, text = native(client, path, name, open_target=open_target)
    assert re.search(r"\+\s*(?:0x" + format(count, "x") + r"|" + str(count) + r")\b", text), text
    return evidence


def stopped():
    processes = subprocess.check_output(["ps", "-eo", "args"]).decode()
    assert "/opt/ghidra" not in processes, "An owned Ghidra process survived normal close"


check(Path("/work"))
print("Task output authenticates against the supplied input.", flush=True)
with tempfile.TemporaryDirectory(prefix="ugt-rea-qualification-") as directory:
    root = Path(directory)
    (root / "inputs").mkdir()
    source = root / "inputs/target.asar"
    shutil.copyfile("/samples/native-one.so", source)
    original_sha = hashlib.sha256(source.read_bytes()).hexdigest()

    first = MCP()
    try:
        tools = first.rpc("tools/list", {})["tools"]
        names = {t["name"] for t in tools}
        required = {"open_binary", "binary_session", "list_procedures", "procedure_pseudo_code", "annotate_native_function", "analyze_javascript_application", "inspect_analysis_view", "export_evidence_bundle", "import_evidence_bundle"}
        assert required <= names, sorted(required - names)
        print(json.dumps({"advertised_tools": len(names), "required_workflows_present": True}), flush=True)
        before = native_code(first, source, 7)
        first.tool("procedure_pseudo_code", {"procedure": "ugt_missing_procedure_8734"}, error=True)
        native_code(first, source, 7, open_target=False)
        annotated = first.tool("annotate_native_function", {"procedure": "plant_count", "name": "ugt_plant_count", "comment": "Synthetic checkpoint persistence check"})
        assert "ugt_plant_count" in json.dumps(annotated)
        bundle = root / "investigation.json"
        first.tool("export_evidence_bundle", {"path": str(bundle)})
        first.tool("close_binary", {})
        first.tool("list_procedures", {}, error=True)
    finally:
        first.close()
    stopped()
    assert hashlib.sha256(source.read_bytes()).hexdigest() == original_sha, "Annotation changed the executable"

    second = MCP()
    try:
        second.tool("open_binary", {"path": str(source), "provider_id": "ghidra"})
        listed = second.tool("list_procedures", {})
        assert any(r["value"] == "ugt_plant_count" for r in listed["normalized_result"]), "The native Program was not restored with its annotation"
        second.tool("import_evidence_bundle", {"path": str(bundle)})
        summary = second.tool("inspect_analysis_view", {"source": {"kind": "retained-evidence", "evidence_id": before["evidence_id"]}, "view": {"kind": "summary"}})
        assert summary["normalized_result"]["parent_evidence_id"] == before["evidence_id"]
        second.tool("annotate_native_function", {"procedure": "ugt_plant_count", "name": "plant_count", "comment": ""})
        second.tool("close_binary", {})
        shutil.copyfile("/samples/native-two.so", source)
        after = native_code(second, source, 11)
        assert after["evidence_id"] != before["evidence_id"], "Changed native input reused the old evidence"
        second.tool("close_binary", {})
        second.tool("open_binary", {"path": str(root / "missing"), "provider_id": "ghidra"}, error=True)
        native_code(second, source, 11)
        second.tool("close_binary", {})
    finally:
        second.close()
    stopped()
    print("Native decompilation, bad-request recovery, annotation/checkpoint restoration, evidence import, changed input and cleanup passed.", flush=True)

    # A real distribution library exercises native analysis beyond the tiny
    # controlled binaries. Its exported compressor must recover real calls.
    libraries = list(Path("/lib").glob("*-linux-gnu/libz.so.1"))
    assert len(libraries) == 1, libraries
    production = MCP()
    try:
        evidence, text = native(production, libraries[0].resolve(), "compress2")
        assert "deflate" in text, "The production compressor's native calls were not recovered"
        production.tool("close_binary", {})
    finally:
        production.close()
    stopped()
    print("The real distribution zlib compressor decompiled with its native deflate calls.", flush=True)

    # These use the same task/check interface as a person's subsequent run.
    source.unlink()
    shutil.copytree("/samples/simple-app", source)
    run(root, "javascript")
    check(root)
    counter = source / "counter.js"
    counter.write_text("export const count = plants => plants.length + 1;\n")
    try:
        check(root)
    except AssertionError:
        pass
    else:
        raise AssertionError("A changed application passed against stale evidence")
    run(root, "javascript")
    check(root)
    shutil.rmtree(source)
    shutil.copyfile("/samples/app.asar", source)
    run(root, "javascript")
    check(root)
    print("JavaScript directory, changed source and real ASAR archive passed.", flush=True)

    source.unlink()
    shutil.copytree("/samples/react-app", source)
    assert (source / "assets/app.js").stat().st_size > 100000
    run(root, "javascript")
    check(root)
    print("Minified React production bundle produced parsed JavaScript and authentic saved evidence.", flush=True)
    stopped()
