import subprocess
import numpy as np
import soundfile as sf
import torch
from demucs.apply import apply_model
from demucs.hf import load_safetensors_model

model = load_safetensors_model("/models/955717e8.safetensors").eval()
raw = subprocess.check_output(["ffmpeg","-loglevel","error","-i","inputs/song","-vn","-ar",str(model.samplerate),"-ac","2","-f","f32le","-"])
wav = torch.from_numpy(np.frombuffer(raw,dtype="<f4").reshape(-1,2).T.copy())
ref = wav.mean(0)
mean,std = ref.mean(),ref.std()+1e-8
with torch.inference_mode():
    separated = apply_model(model, ((wav-mean)/std)[None], shifts=0, device="cpu")[0]
for name, track in zip(model.sources,separated):
    sf.write("outputs/"+name+".wav",(track*std+mean).T.numpy(),model.samplerate,subtype="FLOAT")
