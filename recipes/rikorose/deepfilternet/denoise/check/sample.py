"""Generate speech between two noisy gaps, without downloading a recording."""
from array import array
import random
import subprocess
import wave

subprocess.run(["espeak-ng", "-w", "/tmp/clean.wav", "We recorded the river from the wooden bridge."], check=True)
raw = subprocess.check_output(["ffmpeg", "-loglevel", "error", "-i", "/tmp/clean.wav",
                               "-ac", "1", "-ar", "48000", "-f", "s16le", "-"])
samples = array("h", bytes(96000) + raw + bytes(96000))
rng = random.Random(902)
for i, x in enumerate(samples):
    samples[i] = max(-32768, min(32767, x + rng.randint(-1900, 1900)))
with wave.open("inputs/recording", "wb") as audio:
    audio.setparams((1, 2, 48000, 0, "NONE", "not compressed"))
    audio.writeframes(samples.tobytes())
