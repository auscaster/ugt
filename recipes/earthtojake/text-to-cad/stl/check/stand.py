"""usage: stand.py STL

The known sample (sample.py): a phone stand 70 mm wide, about 98 mm deep and
86 mm tall. Fails unless the mesh has those proportions."""
import sys

import trimesh

extents = sorted(float(x) for x in trimesh.load(sys.argv[1], force="mesh").bounding_box.extents)
want = sorted([70.0, 97.6, 86.1])
assert all(abs(got - expected) < 1.0 for got, expected in zip(extents, want)), \
    f"extents {extents} mm, expected {want} mm"
print(f"OK phone stand extents {[round(e, 1) for e in extents]} mm")
