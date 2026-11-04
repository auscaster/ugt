import json
import math
import subprocess
import sys
from array import array

def info(path):
    raw = subprocess.check_output(["ffprobe", "-v", "error", "-select_streams", "a:0", "-show_entries",
                                   "stream=codec_name,channels,sample_rate,duration:format=duration", "-of", "json", path])
    probe = json.loads(raw)
    stream = probe["streams"][0]
    return stream, float(stream.get("duration") or probe["format"]["duration"])

source = "inputs/recording"
output = "outputs/voice.wav"
_, duration = info(source)
stream, result = info(output)
if (stream["channels"] != 1 or stream["sample_rate"] != "48000" or not stream["codec_name"].startswith("pcm_")
        or result <= 0 or abs(result - duration) > max(0.08, duration * 0.003)):
    raise SystemExit("The output must contain the full recording as 48 kHz mono PCM")

def segment(path, start, length):
    data = subprocess.check_output(["ffmpeg", "-v", "error", "-ss", str(start), "-i", path, "-t", str(length),
                                    "-ac", "1", "-ar", "48000", "-f", "s16le", "-"])
    return array("h", data)

def rms(samples):
    return math.sqrt(sum(x*x for x in samples) / max(1, len(samples)))

if "sample" in sys.argv:
    before, after = (rms(segment(path, 0.4, 0.4)) for path in (source, output))
    speech = rms(segment(output, 1, result-2))
    if after > before*0.55 or speech < max(after*3, 100):
        raise SystemExit(f"Expected quieter gaps and retained speech; noise {before:.1f} -> {after:.1f}, speech {speech:.1f}")
else:
    # A silent replacement is not cleaned speech. Inspect the entire stream with bounded memory.
    def peak(path):
        p = subprocess.Popen(["ffmpeg", "-v", "error", "-i", path, "-ac", "1", "-ar", "8000", "-f", "s16le", "-"], stdout=subprocess.PIPE)
        highest = 0
        with p.stdout:
            while chunk := p.stdout.read(16384):
                highest = max(highest, max(map(abs, array("h", chunk))))
        if p.wait():
            raise SystemExit("Could not decode the complete recording")
        return highest
    if peak(source) > 100 and peak(output) == 0:
        raise SystemExit("The input was audible but the output is silent")
