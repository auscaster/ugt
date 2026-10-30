"""usage: srt.py RECORDING SUBTITLES

Fails unless the subtitles are well formed and fit the recording: cues numbered
from 1, ordered timestamps that end within the recording, and text in every cue.
Works for any recording; the known sample's words are checked separately."""
import re
import subprocess
import sys

audio, srt = sys.argv[1], sys.argv[2]
duration = float(subprocess.run(
    ["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", audio],
    capture_output=True, text=True, check=True).stdout)
cues = re.findall(r"^(\d+)\n(\S+) --> (\S+)\n(.+?)(?:\n\n|\Z)",
                  open(srt, encoding="utf-8").read(), re.S | re.M)


def seconds(stamp):
    h, m, s = stamp.replace(",", ".").split(":")
    return int(h) * 3600 + int(m) * 60 + float(s)


assert cues, "no cues"
assert [int(c[0]) for c in cues] == list(range(1, len(cues) + 1)), "cues not numbered from 1"
end = 0.0
for number, start, stop, text in cues:
    assert end <= seconds(start) <= seconds(stop), f"cue {number} out of order"
    assert text.strip(), f"cue {number} has no text"
    end = seconds(stop)
assert end <= duration + 1, f"cues run to {end:.1f} s, past the {duration:.1f} s recording"
print(f"OK cues={len(cues)} last={end:.1f}s recording={duration:.1f}s")
