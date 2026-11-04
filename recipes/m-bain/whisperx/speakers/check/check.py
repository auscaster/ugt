import json
from pathlib import Path
import subprocess
import sys
segments=json.loads(Path("outputs/segments.json").read_bytes())["segments"]
text=Path("outputs/transcript.txt").read_text(encoding="utf-8")
duration=float(subprocess.check_output(["ffprobe","-v","error","-show_entries","format=duration","-of","default=nw=1:nk=1","inputs/conversation.wav"]))
speakers={}
for s in segments:
    speaker=s.get("speaker")
    words=s["text"].strip()
    if not 0<=s["start"]<=s["end"]<=duration+0.5 or words not in text:
        raise SystemExit("Segment times or transcript text do not match the recording")
    if speaker: speakers[speaker]=speakers.get(speaker,"")+" "+words.casefold()
if not speakers: raise SystemExit("The result has no speaker-labelled speech")
if "sample" in sys.argv and not any("boats" in a and "train" in b for n,a in speakers.items() for m,b in speakers.items() if n!=m):
    raise SystemExit("The two sample speakers were not distinguished at the change of topic")
