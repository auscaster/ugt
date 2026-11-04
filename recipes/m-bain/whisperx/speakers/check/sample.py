import subprocess
import wave
from array import array
parts=[]
for voice,text in [("en-us+m3","We counted seven boats in the harbour. We wrote every number in the book."),
                   ("en-sc+f4","The red train leaves the station at noon. I will meet you there after lunch.")]:
    data=subprocess.check_output(["espeak-ng","--stdout","-v",voice,"-s","140",text])
    audio=subprocess.check_output(["ffmpeg","-v","error","-i","pipe:","-ar","16000","-ac","1","-f","s16le","-"],input=data)
    parts += [audio,bytes(16000*2*2)]
with wave.open("inputs/conversation.wav","wb") as out:
    out.setparams((1,2,16000,0,"NONE","not compressed"))
    out.writeframes(b"".join(parts))
