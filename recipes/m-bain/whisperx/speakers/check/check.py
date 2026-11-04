import json
import re
import subprocess
import sys
from pathlib import Path

segments = json.loads(Path("outputs/segments.json").read_bytes())["segments"]
duration = float(subprocess.check_output(["ffprobe", "-v", "error", "-show_entries",
    "format=duration", "-of", "default=nw=1:nk=1", "inputs/conversation.wav"]))
lines = Path("outputs/transcript.txt").read_text(encoding="utf-8").splitlines()
speakers, visible, labelled = {}, [], []
previous = -1
for line in lines:
    match = re.fullmatch(r"\[(\d+):(\d{2})\] (SPEAKER_\d+|Unassigned): (\S.*)", line)
    if not match:
        raise SystemExit("A transcript line is missing its timestamp, speaker or text")
    minutes, secs, who, words = match.groups()
    start = 60 * int(minutes) + int(secs)
    if int(secs) >= 60 or not previous <= start <= duration:
        raise SystemExit("Transcript timestamps are out of order or outside the recording")
    previous = start
    visible.extend(words.split())
    labelled.extend((who, word) for word in words.split())
    if who != "Unassigned":
        speakers[who] = speakers.get(who, "") + " " + words.casefold()
if not speakers or not segments:
    raise SystemExit("The result has no speaker-labelled speech")
if any(not 0 <= s["start"] <= s["end"] <= duration + 0.5 for s in segments):
    raise SystemExit("Segment times are outside the recording")
if visible != [word for s in segments for word in s["text"].split()]:
    raise SystemExit("The readable and machine-readable transcripts disagree")
expected = [(w.get("speaker") or s.get("speaker") or "Unassigned", word)
            for s in segments for w in (s.get("words") or [{"word": s["text"]}])
            for word in w["word"].split()]
if labelled != expected:
    raise SystemExit("Speaker labels disagree between the two transcripts")
if "sample" in sys.argv:
    topics = [{"harbor", "book"}, {"station", "noon"}, {"blue", "flag"}]
    matches = [[who for who, words in speakers.items() if all(term in words for term in topic)]
               for topic in topics]
    if not all(len(names) == 1 for names in matches) or matches[0] != matches[2] or matches[0] == matches[1]:
        raise SystemExit("The two voices did not retain their labels across the three sample turns")
