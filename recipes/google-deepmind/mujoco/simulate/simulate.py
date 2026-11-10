import csv
import hashlib
import json
import math
from pathlib import Path
import subprocess
import sys

import mujoco
import numpy as np
from PIL import Image


def fingerprint(root):
    digest = hashlib.sha256()
    for file in sorted((root / "inputs/scene").rglob("*")):
        if file.is_file():
            digest.update(str(file.relative_to(root / "inputs/scene")).encode() + b"\0")
            digest.update(file.read_bytes())
    return digest.hexdigest()


def load(root):
    path = root / "inputs/scene/options.json"
    options = json.loads(path.read_text()) if path.exists() else {}
    if not isinstance(options, dict) or set(options) - {"duration", "fps", "width", "height", "camera"}:
        raise ValueError("options.json accepts duration, fps, width, height and camera")
    opts = {"duration": 2, "fps": 24, "width": 640, "height": 480, **options}
    if type(opts["duration"]) not in {int, float} or not math.isfinite(opts["duration"]) or not 0.1 <= opts["duration"] <= 600:
        raise ValueError("duration must be 0.1–600 seconds")
    if type(opts["fps"]) is not int or not 1 <= opts["fps"] <= 60:
        raise ValueError("fps must be 1–60")
    for key in ["width", "height"]:
        if type(opts[key]) is not int or not 64 <= opts[key] <= 1920 or opts[key] % 2:
            raise ValueError(f"{key} must be an even number from 64 to 1920")
    model = mujoco.MjModel.from_xml_path(str(root / "inputs/scene/model.xml"))
    if not 0 < model.opt.timestep or math.ceil(opts["duration"] / model.opt.timestep) > 1000000:
        raise ValueError("This duration and model timestep exceed one million steps")
    if model.nplugin:
        raise ValueError("External native plugins are outside this deterministic offline workflow")
    if opts.get("camera") is not None and (not isinstance(opts["camera"], str) or mujoco.mj_name2id(model, mujoco.mjtObj.mjOBJ_CAMERA, opts["camera"]) < 0):
        raise ValueError("camera must name a camera in the scene")
    actuators = [mujoco.mj_id2name(model, mujoco.mjtObj.mjOBJ_ACTUATOR, i) or f"ctrl_{i}" for i in range(model.nu)]
    controls = []
    file = root / "inputs/scene/controls.csv"
    if file.exists():
        with file.open(newline="") as f:
            reader = csv.DictReader(f)
            if reader.fieldnames != ["time", *actuators]:
                raise ValueError(f"controls.csv columns must be {['time', *actuators]}")
            for row in reader:
                values = [float(row[k]) for k in reader.fieldnames]
                if not all(math.isfinite(v) for v in values) or values[0] < 0 or (controls and values[0] <= controls[-1][0]):
                    raise ValueError("Control times must be increasing and values finite")
                controls.append(values)
        if not controls or controls[0][0] != 0:
            raise ValueError("The control table must begin at time zero")
    return model, opts, controls


def apply(data, controls, index):
    while index + 1 < len(controls) and controls[index + 1][0] <= data.time + 1e-12:
        index += 1
    if controls:
        data.ctrl[:] = controls[index][1:]
    return index


def state(data):
    return [float(data.time), *map(float, data.qpos), *map(float, data.qvel)]


def header(model):
    return ["time", *[f"qpos_{i}" for i in range(model.nq)], *[f"qvel_{i}" for i in range(model.nv)]]


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def run(root):
    model, opts, controls = load(root)
    out = root / "outputs"
    out.mkdir(exist_ok=True)
    (out / "simulation.json").unlink(missing_ok=True)
    data = mujoco.MjData(model)
    mujoco.mj_forward(model, data)
    frames = math.ceil(opts["duration"] * opts["fps"])
    # MJCF's offscreen framebuffer defaults to 640x480; honour the capture size.
    model.vis.global_.offwidth = max(model.vis.global_.offwidth, opts["width"])
    model.vis.global_.offheight = max(model.vis.global_.offheight, opts["height"])
    rendered = maximum_contacts = index = 0
    with mujoco.Renderer(model, height=opts["height"], width=opts["width"]) as renderer, (out / "trajectory.csv").open("w", newline="") as trajectory, (out / "render.log").open("w") as log:
        encoder = subprocess.Popen(["ffmpeg", "-v", "error", "-f", "rawvideo", "-pixel_format", "rgb24", "-video_size", f"{opts['width']}x{opts['height']}",
                                    "-framerate", str(opts["fps"]), "-i", "pipe:0", "-an", "-c:v", "libx264", "-threads", "2", "-pix_fmt", "yuv420p",
                                    "-movflags", "+faststart", "-y", str(out / "simulation.mp4")], stdin=subprocess.PIPE, stderr=log)
        writer = csv.writer(trajectory)
        writer.writerow(header(model))
        writer.writerow(state(data))
        try:
            while data.time < opts["duration"] - 1e-12:
                index = apply(data, controls, index)
                while rendered < frames and data.time + 1e-12 >= rendered / opts["fps"]:
                    renderer.update_scene(data, camera=opts.get("camera", -1))
                    pixels = renderer.render()
                    if rendered == 0:
                        Image.fromarray(pixels).save(out / "frame.png")
                    encoder.stdin.write(pixels.tobytes())
                    rendered += 1
                mujoco.mj_step(model, data)
                values = state(data)
                if not all(math.isfinite(v) for v in values):
                    raise ValueError("The model's simulation became non-finite")
                writer.writerow(values)
                maximum_contacts = max(maximum_contacts, int(data.ncon))
        finally:
            encoder.stdin.close()
            if encoder.wait(timeout=60):
                raise ValueError("Video encoding failed; see outputs/render.log")
    if rendered != frames:
        raise ValueError("The model timestep skipped requested capture frames; choose a smaller model timestep")
    report = {"input_sha256": fingerprint(root), "options": opts, "nq": model.nq, "nv": model.nv,
              "frames": frames, "max_contacts": maximum_contacts,
              "files": {name: sha(out / name) for name in ["trajectory.csv", "simulation.mp4", "frame.png"]}}
    (out / "simulation.json").write_text(json.dumps(report, indent=2) + "\n")


def check(root):
    model, opts, controls = load(root)
    out = root / "outputs"
    report = json.loads((out / "simulation.json").read_text())
    assert report["input_sha256"] == fingerprint(root) and report["options"] == opts
    assert report["nq"] == model.nq and report["nv"] == model.nv
    for name in ["trajectory.csv", "simulation.mp4", "frame.png"]:
        assert sha(out / name) == report["files"][name]
    data = mujoco.MjData(model)
    mujoco.mj_forward(model, data)
    index = rows = 0
    with (out / "trajectory.csv").open(newline="") as file:
        reader = csv.reader(file)
        assert next(reader) == header(model)
        for row in reader:
            values = np.array([float(v) for v in row])
            assert np.all(np.isfinite(values)) and np.allclose(values, state(data), rtol=1e-8, atol=1e-9), "Trajectory does not follow the supplied model and controls"
            rows += 1
            if data.time >= opts["duration"] - 1e-12:
                assert next(reader, None) is None, "Extra simulation rows"
                break
            index = apply(data, controls, index)
            mujoco.mj_step(model, data)
    assert rows > 1 and data.time >= opts["duration"] - 1e-12, "Incomplete trajectory"
    info = json.loads(subprocess.check_output(["ffprobe", "-v", "error", "-select_streams", "v:0", "-count_frames", "-show_entries", "stream=width,height,nb_read_frames", "-of", "json", str(out / "simulation.mp4")]))["streams"][0]
    assert info["width"] == opts["width"] and info["height"] == opts["height"]
    assert int(info["nb_read_frames"]) == math.ceil(opts["duration"] * opts["fps"]) == report["frames"]
    with Image.open(out / "frame.png") as frame:
        frame.load()
        assert frame.size == (opts["width"], opts["height"]) and len(set(frame.getdata())) > 8


if __name__ == "__main__":
    try:
        (check if len(sys.argv) > 1 else run)(Path("/work"))
    except Exception as error:
        sys.exit(f"Could not simulate the model: {error}")
