import subprocess
from basic_pitch.inference import predict
subprocess.run(["ffmpeg","-v","error","-i","inputs/recording","-vn","-ac","1","-ar","22050","/tmp/notes.wav"],check=True)
_, midi, _ = predict("/tmp/notes.wav")
midi.write("outputs/notes.mid")
