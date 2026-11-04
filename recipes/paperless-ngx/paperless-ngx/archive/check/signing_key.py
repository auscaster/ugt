"""One atomic signing key per local installation; restarts retain sessions."""
import os
import secrets
import tempfile
from pathlib import Path

key = Path("/config/signing-key")
if not key.exists():
    with tempfile.NamedTemporaryFile(dir=key.parent, delete=False) as temp:
        temp.write(secrets.token_urlsafe(64).encode("ascii"))
    try:
        os.link(temp.name, key)
    except FileExistsError:
        pass
    finally:
        os.unlink(temp.name)
