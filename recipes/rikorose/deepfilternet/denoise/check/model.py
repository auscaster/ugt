import hashlib
import platform
from pathlib import Path
from urllib.request import urlretrieve
assets = {
    "aarch64": ("aarch64-unknown-linux-gnu", "14e02a1c0028f3ca0bdf83b62b3336e56ba0556894ef295a95e8573f06557166"),
    "x86_64": ("x86_64-unknown-linux-musl", "70775e251eee44c0f2451a1e833326cf8bcbbe304d3e7cd12851e6fce72ef7da"),
}
asset, digest = assets[platform.machine()]
path = Path("/models/deep-filter")
def valid(file):
    with file.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest() == digest
if not path.exists() or not valid(path):
    temp = path.with_suffix(".part")
    urlretrieve(f"https://github.com/Rikorose/DeepFilterNet/releases/download/v0.5.6/deep-filter-0.5.6-{asset}", temp)
    if not valid(temp):
        raise SystemExit("The downloaded executable did not match its upstream digest")
    temp.chmod(0o755)
    temp.replace(path)
