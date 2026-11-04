import subprocess
import sys
import numpy as np
import soundfile as sf

raw = subprocess.check_output(["ffmpeg","-v","error","-i","inputs/song","-ar","44100","-ac","2","-f","f32le","-"])
mix = np.frombuffer(raw,dtype="<f4").reshape(-1,2)
reconstructed = np.zeros_like(mix)
tracks = []
for name in ["vocals","drums","bass","other"]:
    stem, rate = sf.read("outputs/"+name+".wav",dtype="float32",always_2d=True)
    if rate != 44100 or stem.shape != mix.shape or not np.isfinite(stem).all():
        raise SystemExit("Incorrect duration, channels or samples in "+name)
    reconstructed += stem
    if "sample" in sys.argv:
        if np.sqrt(np.mean(stem**2)) < 0.0002:
            raise SystemExit("Missing audible "+name)
        tracks.append(stem[:,0])
signal, error = np.mean(mix**2), np.mean((mix-reconstructed)**2)
if error > max(1e-8, signal/10):
    raise SystemExit("The four stems do not reconstruct the recording within 10 dB")
if tracks and np.max(np.abs(np.corrcoef(tracks)-np.eye(4))) > 0.98:
    raise SystemExit("The outputs carry duplicated tracks")
