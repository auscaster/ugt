import hashlib
from pathlib import Path
from urllib.request import urlretrieve
digest = "d9fa14133cfcc034a6758923bb3a8ca9f8dfd0b582134643bbf83f72c17576dd"
path = Path("/models/955717e8.safetensors")
def valid(file):
    with file.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest() == digest
if not path.exists() or not valid(path):
    temp = path.with_suffix(".part")
    urlretrieve("https://huggingface.co/adefossez/HTDemucs/resolve/cbc8a9b1a87023b7fd74e7b3412e6321c0eab003/955717e8.safetensors", temp)
    if not valid(temp):
        raise SystemExit("The downloaded model did not match its upstream digest")
    temp.replace(path)
