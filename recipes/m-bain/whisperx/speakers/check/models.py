import os
from pathlib import Path

from huggingface_hub import snapshot_download
from huggingface_hub.errors import HfHubHTTPError

# Only the speaker repository requires the user's token.
models = [
    ("speakers", "pyannote/speaker-diarization-community-1", "3533c8cf8e369892e6b79ff1bf80f7b0286a54ee"),
    ("asr", "Systran/faster-whisper-base.en", "3d3d5dee26484f91867d81cb899cfcf72b96be6c"),
    ("alignment", "facebook/wav2vec2-base-960h", "22aad52d435eb6dbaf354bdad9b0da84ce7d6156"),
]
for name, repo, revision in models:
    dest = Path("/models") / name
    marker = dest / ".ugt-revision"
    if marker.is_file() and marker.read_text() == revision:
        continue
    try:
        snapshot_download(
            repo, revision=revision, local_dir=dest,
            token=os.environ.get("HF_TOKEN") if name == "speakers" else False,
            ignore_patterns=["*.git*", "*.h5", "pytorch_model.bin"] if name == "alignment" else ["*.git*"],
        )
    except HfHubHTTPError as exc:
        if name == "speakers" and exc.response is not None and exc.response.status_code in (401, 403):
            raise SystemExit("Model access was refused. Review community-1's terms and update the read token in UGT.") from None
        raise
    marker.write_text(revision)
