from array import array
import wave
with wave.open("outputs/speech.wav","rb") as audio:
    if audio.getparams()[:3] != (1,2,24000) or not audio.getnframes():
        raise SystemExit("Expected a 24 kHz mono recording")
    n = 0
    peak = 0
    while data := audio.readframes(8192):
        n += len(data)//2
        peak = max(peak,max(map(abs,array("h",data))))
    if n != audio.getnframes() or peak < 100:
        raise SystemExit("The recording is truncated or silent")
