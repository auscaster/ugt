import hashlib
from pathlib import Path
import shutil
import tarfile
from tempfile import TemporaryDirectory
from urllib.request import urlretrieve
root = Path("/models")
target = root/"2stems"
if not (target/"model.index").is_file():
    with TemporaryDirectory(dir=root) as tmp:
        archive=Path(tmp)/"2stems.tar.gz"
        urlretrieve("https://github.com/deezer/spleeter/releases/download/v1.4.0/2stems.tar.gz",archive)
        with archive.open("rb") as f:
            if hashlib.file_digest(f,"sha256").hexdigest()!="f3a90b39dd2874269e8b05a48a86745df897b848c61f3958efc80a39152bd692":
                raise SystemExit("The model download did not match Deezer's checksum")
        out=Path(tmp)/"weights"
        with tarfile.open(archive) as t:
            t.extractall(out,filter="data")
        if not (out/"model.index").is_file():
            raise SystemExit("The archive did not contain the expected checkpoint")
        out.rename(target)
