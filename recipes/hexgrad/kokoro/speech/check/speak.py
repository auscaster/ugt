import os
import wave
from pathlib import Path
from kokoro import KModel, KPipeline

text = Path("inputs/text.txt").read_text(encoding="utf-8-sig")
if not text.strip():
    raise SystemExit("The text file is empty")
voices = {p.stem: p for p in Path("/models/voices").glob("*.pt")}
voice = os.environ.get("VOICE", "af_heart")
if voice not in voices:
    raise SystemExit("Choose a supported VOICE: " + ", ".join(sorted(voices)))
try:
    speed = float(os.environ.get("SPEED", "1"))
except ValueError:
    speed = 0
if not 0.5 <= speed <= 2:
    raise SystemExit("Choose a SPEED multiplier between 0.5 and 2.")

repo = "hexgrad/Kokoro-82M"
model = KModel(repo_id=repo, config="/models/config.json", model="/models/kokoro.pth").eval()
pipeline = KPipeline(lang_code=voice[0], repo_id=repo, model=model, device="cpu")
with wave.open("outputs/speech.wav", "wb") as track:
    track.setparams((1,2,24000,0,"NONE","not compressed"))
    for result in pipeline(text, voice=str(voices[voice]), speed=speed):
        if result.audio is not None:
            pcm = result.audio.detach().cpu().clamp(-1,1).mul(32767).short().numpy()
            track.writeframes(pcm.tobytes())
