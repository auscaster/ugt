import os
from pathlib import Path
from huggingface_hub import snapshot_download
from huggingface_hub.errors import HfHubHTTPError
models=[("pyannote/speaker-diarization-community-1","3533c8cf8e369892e6b79ff1bf80f7b0286a54ee","speakers"),
        ("Systran/faster-whisper-base.en","3d3d5dee26484f91867d81cb899cfcf72b96be6c","asr")]
for repo,rev,name in models:
    dest=Path("/models")/name
    marker=dest/".ugt-revision"
    if marker.is_file() and marker.read_text()==rev: continue
    try:
        snapshot_download(repo, revision=rev, local_dir=dest, token=os.environ.get("HF_TOKEN") or None,
                          ignore_patterns=["*.git*"])
    except HfHubHTTPError as err:
        if err.response is not None and err.response.status_code in (401,403):
            raise SystemExit("Model access was refused. Review community-1's terms and update the read token in UGT.") from None
        raise
    marker.write_text(rev)
