"""usage: stl.py OUTPUTS

Fails unless OUTPUTS holds at least one STL and every STL is a printable mesh:
it parses, has triangles, is watertight and encloses a positive volume. Works for
any model; the known sample's dimensions are checked separately."""
import sys
from pathlib import Path

import trimesh

stls = sorted(Path(sys.argv[1]).glob("*.stl"))
assert stls, "no STL in outputs"
for path in stls:
    mesh = trimesh.load(path, force="mesh")
    assert len(mesh.faces) > 0, f"{path.name}: no triangles"
    assert mesh.is_watertight, f"{path.name}: not watertight, so not printable"
    assert mesh.volume > 0, f"{path.name}: no enclosed volume"
    extents = [round(float(x), 1) for x in mesh.bounding_box.extents]
    print(f"OK {path.name}: {len(mesh.faces)} triangles, {extents} mm, {mesh.volume:.0f} mm3")
