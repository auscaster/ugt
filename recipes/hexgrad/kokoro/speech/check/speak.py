import wave
from pathlib import Path
from kokoro import KModel, KPipeline

text = Path("inputs/text.txt").read_text(encoding="utf-8-sig")
if not text.strip():
    raise SystemExit("The text file is empty")
repo = "hexgrad/Kokoro-82M"
model = KModel(repo_id=repo, config="/models/config.json", model="/models/kokoro.pth").eval()
pipeline = KPipeline(lang_code="a", repo_id=repo, model=model, device="cpu")
with wave.open("outputs/speech.wav", "wb") as track:
    track.setparams((1,2,24000,0,"NONE","not compressed"))
    for result in pipeline(text, voice="/models/af_heart.pt"):
        if result.audio is not None:
            pcm = result.audio.detach().cpu().clamp(-1,1).mul(32767).short().numpy()
            track.writeframes(pcm.tobytes())
