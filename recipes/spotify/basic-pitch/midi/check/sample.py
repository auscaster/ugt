import numpy as np
import soundfile as sf
rate = 22050
audio = [np.zeros(rate//4)]
for note in [60,64,67]:
    t = np.arange(rate)/rate
    f = 440*2**((note-69)/12)
    envelope = np.minimum(t/.025,1)*np.exp(-2.2*t)*np.minimum((1-t)/.08,1)
    audio += [envelope*(.45*np.sin(2*np.pi*f*t)+.1*np.sin(4*np.pi*f*t)),np.zeros(rate//3)]
sf.write("inputs/recording",np.concatenate(audio),rate,format="WAV")
