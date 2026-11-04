"""An original spoken refrain over a synthesized bass line, beat and chords."""
import subprocess
import numpy as np
import soundfile as sf
rate = 44100
voice = subprocess.check_output(["espeak-ng","--stdout","Sailing to the harbour. Sailing home tonight."])
spoken = np.frombuffer(subprocess.check_output(["ffmpeg","-v","error","-i","pipe:","-ar",str(rate),"-ac","1","-f","f32le","-"],input=voice),dtype="<f4")
t = np.arange(rate*8)/rate
beat = t%0.5
notes = np.array([110,110,130.8128,98])[(t//2).astype(int)%4]
bass = .17*np.sin(2*np.pi*notes*t)*np.exp(-3*beat)
kick = .35*np.sin(2*np.pi*(48*beat+2*(1-np.exp(-25*beat))))*np.exp(-20*beat)
rng = np.random.default_rng(19)
snare = .16*rng.uniform(-1,1,len(t))*np.exp(-30*((t+.25)%.5))
chord = .07*(np.sin(2*np.pi*notes*5*t)+np.sin(2*np.pi*notes*6*t))
mix = bass+kick+snare+chord+np.resize(spoken,len(t))*.85
sf.write("inputs/song",np.column_stack([mix,mix]),rate,format="WAV")
