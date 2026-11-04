import hashlib
from pathlib import Path
from urllib.request import urlretrieve
digest = "4fa0d38905f75ac06eb49a7951b426670021be3018265fd191d2125df9d682f1"
path = Path("/models/x4plus.pth")
def valid(file):
    with file.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest() == digest
if not path.exists() or not valid(path):
    temp = path.with_suffix(".part")
    urlretrieve("https://github.com/xinntao/Real-ESRGAN/releases/download/v0.1.0/RealESRGAN_x4plus.pth", temp)
    if not valid(temp):
        raise SystemExit("The downloaded model did not match its upstream digest")
    temp.replace(path)
