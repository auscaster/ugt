import itertools
import json
import os
from pathlib import Path
import subprocess

subprocess.run([
    "whisperx", "inputs/conversation.wav", "--model", "/models/asr",
    "--model_cache_only", "True", "--language", "en", "--device", "cpu",
    "--compute_type", "int8", "--batch_size", "1", "--align_model", "/models/alignment",
    "--diarize", "--diarize_model", "/models/speakers/config.yaml",
    "--output_format", "json", "--output_dir", "/tmp/whisperx",
], env=dict(os.environ, HF_HUB_OFFLINE="1"), check=True)
raw = Path("/tmp/whisperx/conversation.json").read_bytes()
segments = json.loads(raw)["segments"]
Path("outputs/segments.json").write_bytes(raw)
with Path("outputs/transcript.txt").open("w", encoding="utf-8") as stream:
    for seg in segments:
        words = seg.get("words") or [{"word": seg["text"]}]
        for label, group in itertools.groupby(words, lambda w: w.get("speaker") or seg.get("speaker") or "Unassigned"):
            turn = list(group)
            seconds = int(next((w["start"] for w in turn if "start" in w), seg["start"]))
            text = " ".join(w["word"].strip() for w in turn)
            stream.write(f"[{seconds//60:02}:{seconds%60:02}] {label}: {text}\n")
