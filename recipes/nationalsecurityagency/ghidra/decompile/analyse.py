import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


def check(root):
    source, out = root / "inputs/program", root / "outputs"
    report = json.loads((out / "functions.json").read_text())
    assert report["input_sha256"].lower() == hashlib.sha256(source.read_bytes()).hexdigest()
    assert report["language"] and report["functions"]
    functions = report["functions"]
    assert len({f["address"] for f in functions}) == len(functions)
    for f in functions:
        assert f["name"] and f["signature"] and "{" in f["code"] and "}" in f["code"]
    assert (out / "functions.c").read_text() == "".join(f["code"] + "\n" for f in functions)
    assert isinstance(report["failures"], list)
    assert (out / "analysis.log").stat().st_size > 0


def run(root):
    source, out = root / "inputs/program", root / "outputs"
    if not source.is_file() or source.stat().st_size == 0:
        raise ValueError("Supply a nonempty executable or library as inputs/program")
    seconds = int(os.environ.get("GHIDRA_TIMEOUT", "900"))
    if not 30 <= seconds <= 3600:
        raise ValueError("GHIDRA_TIMEOUT must be 30–3600 seconds")
    out.mkdir(exist_ok=True)
    for name in ["functions.json", "functions.c"]:
        (out / name).unlink(missing_ok=True)
    with tempfile.TemporaryDirectory() as project, (out / "analysis.log").open("w") as log:
        result = subprocess.run(["/opt/ghidra/support/analyzeHeadless", project, "inspection", "-import", str(source),
                                 "-scriptPath", "/recipe", "-postScript", "ExportFunctions.java", str(out),
                                 "-analysisTimeoutPerFile", str(seconds), "-deleteProject"],
                                stdout=log, stderr=subprocess.STDOUT, timeout=seconds)
    if result.returncode or not (out / "functions.json").is_file():
        raise ValueError("Ghidra could not export decompiled functions; see outputs/analysis.log")
    check(root)


if __name__ == "__main__":
    try:
        mode = sys.argv[1] if len(sys.argv) > 1 else "run"
        root = Path("/work")
        if mode == "check":
            check(root)
        elif mode == "qualify":
            check(root)
            report = json.loads((root / "outputs/functions.json").read_text())
            function = next(f for f in report["functions"] if f["name"] == "greenhouse_count")
            assert "return" in function["code"] and "+" in function["code"]
            with tempfile.TemporaryDirectory() as d:
                work = Path(d)
                (work / "inputs").mkdir()
                shutil.copy("/samples/two", work / "inputs/program")
                run(work)
                alternate = json.loads((work / "outputs/functions.json").read_text())
                assert alternate["input_sha256"] != report["input_sha256"]
                assert next(f["code"] for f in alternate["functions"] if f["name"] == "greenhouse_count") != function["code"]
                (work / "inputs/program").write_bytes(b"")
                try:
                    run(work)
                except ValueError:
                    pass
                else:
                    raise AssertionError("Empty input was accepted")
        else:
            run(root)
    except Exception as error:
        sys.exit(f"Could not analyse the program: {error}")
