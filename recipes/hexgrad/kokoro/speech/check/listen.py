import re
import whisper
model = whisper.load_model("base", device="cpu", download_root="/models")
text = model.transcribe("outputs/speech.wav",language="en",fp16=False,temperature=0)["text"]
words = re.findall("[a-z]+",text.lower())
if words != "the red boat crossed the river before sunset".split():
    raise SystemExit("Independent transcription: " + text)
