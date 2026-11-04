import json
import os
from pathlib import Path
import subprocess
env=dict(os.environ, HF_HUB_OFFLINE="1")
subprocess.run(["whisperx","inputs/conversation.wav","--model","/models/asr","--model_cache_only","True",
                "--language","en","--device","cpu","--compute_type","int8","--batch_size","1","--no_align",
                "--diarize","--diarize_model","/models/speakers/config.yaml","--output_format","json",
                "--output_dir","/tmp/whisperx"],env=env,check=True)
raw=Path("/tmp/whisperx/conversation.json").read_bytes()
segments=json.loads(raw)["segments"]
Path("outputs/segments.json").write_bytes(raw)
with Path("outputs/transcript.txt").open("w",encoding="utf-8") as f:
    for s in segments:
        t=int(s["start"])
        f.write(f'[{t//60:02}:{t%60:02}] {s.get("speaker","Unassigned")}: {s["text"].strip()}\n')
