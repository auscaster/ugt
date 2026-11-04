import io
import json
import re
import sys
import time
import urllib.error
import urllib.request
import wave
from pathlib import Path

host="http://studio:8880"
deadline=time.monotonic()+120
while True:
    try:
        with urllib.request.urlopen(host+"/v1/models",timeout=3) as res:
            if json.load(res)["data"]: break
    except (urllib.error.URLError,TimeoutError) as exc:
        if time.monotonic()>=deadline or "Name or service not known" in str(exc): raise
    time.sleep(2)
sentence="The green train arrives at noon."
body=json.dumps({"model":"kokoro","input":sentence,"voice":"af_heart","response_format":"wav"}).encode()
req=urllib.request.Request(host+"/v1/audio/speech",data=body,headers={"Content-Type":"application/json"})
with urllib.request.urlopen(req,timeout=120) as res: data=res.read()
with wave.open(io.BytesIO(data)) as sound:
    if sound.getnframes()<1000 or sound.getframerate()!=24000:
        raise SystemExit("Speech API did not return a spoken WAV")
if "sample" in sys.argv:
    import whisper
    Path("/tmp/voice.wav").write_bytes(data)
    model=whisper.load_model("base",device="cpu",download_root="/models")
    transcript=model.transcribe("/tmp/voice.wav",language="en",temperature=0,fp16=False)["text"]
    if re.findall("[a-z]+",transcript.lower())!=re.findall("[a-z]+",sentence.lower()):
        raise SystemExit("Unexpected transcription: "+transcript)
