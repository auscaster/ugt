import csv
import json
from pathlib import Path
import tempfile

from simulate import check, run

root = Path("/work")
check(root)
report = json.loads((root / "outputs/simulation.json").read_text())
rows = list(csv.DictReader((root / "outputs/trajectory.csv").open()))
assert report["max_contacts"] > 0, "The ball never contacted the floor"
early = min(rows, key=lambda r: abs(float(r["time"]) - 0.1))
time = float(early["time"])
assert abs(float(early["qpos_2"]) - (1 - 9.81 * time * time / 2)) < 0.003, "Free fall does not match gravity"
assert min(float(r["qpos_2"]) for r in rows) > 0.07, "The ball fell through the floor"

with tempfile.TemporaryDirectory() as d:
    work = Path(d)
    (work / "inputs/scene").mkdir(parents=True)
    (work / "inputs/scene/model.xml").write_text('<mujoco><option timestep="0.002" gravity="0 0 0"/><worldbody><light pos="0 0 3"/><body pos="0 0 0.2"><joint name="slide" type="slide" axis="1 0 0" damping="1"/><geom type="sphere" size="0.1" mass="1"/></body></worldbody><actuator><motor name="push" joint="slide"/></actuator></mujoco>')
    (work / "inputs/scene/options.json").write_text(json.dumps({"duration": 0.5, "fps": 12, "width": 320, "height": 240}))
    (work / "inputs/scene/controls.csv").write_text("time,push\n0,1\n")
    run(work)
    check(work)
    controlled = list(csv.DictReader((work / "outputs/trajectory.csv").open()))
    assert float(controlled[-1]["qpos_0"]) > 0.05, "The actuator control did not move the body"
    (work / "inputs/scene/controls.csv").write_text("time,push\n0,-1\n")
    run(work)
    check(work)
    changed = list(csv.DictReader((work / "outputs/trajectory.csv").open()))
    assert float(changed[-1]["qpos_0"]) < -0.05, "Changed controls did not change the motion"
    (work / "inputs/scene/model.xml").write_text("not an MJCF scene")
    try:
        run(work)
    except Exception:
        pass
    else:
        raise AssertionError("Malformed scene was accepted")
