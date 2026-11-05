import json
import math
import re
import subprocess
import sys
from array import array
from options import margin, threshold
def info(path):
    obj=json.loads(subprocess.check_output(["ffprobe","-v","error","-show_streams","-show_format","-of","json",path]))
    return float(obj["format"]["duration"]),{stream["codec_type"] for stream in obj["streams"]}
source,_ = info("inputs/video.mp4")
out,kinds = info("outputs/edited.mp4")
if not {"video","audio"}<=kinds or out<=0 or out>source+.15:
    raise SystemExit("The edit must keep video and audio within the original duration")
# A video with no long pauses is allowed to keep its original duration.
# Use a threshold well below the editor's default, leaving room for edit margins.
def clear_pauses(path):
    scan = subprocess.run(["ffmpeg","-nostdin","-hide_banner","-nostats","-i",path,
                           "-vn","-af","silencedetect=noise=-60dB:d=1","-f","null","-"],
                          check=True,stderr=subprocess.PIPE,text=True)
    return sum(max(0.0,float(value)-2*margin-0.3) for value in
               re.findall(r"silence_duration: ([0-9.]+)",scan.stderr))
pauses = clear_pauses("inputs/video.mp4") if threshold >= 0.001 else 0.0
if out > source - pauses + 0.2:
    raise SystemExit("The export still contains the long pauses from the input")
if "sample" in sys.argv:
    if not 1.75<out<2.6:
        raise SystemExit("The three-second pause was not cut")
    for at,freq in [(0.2,440),(out-.5,880)]:
        data=subprocess.check_output(["ffmpeg","-v","error","-ss",str(at),"-i","outputs/edited.mp4","-t","0.25","-ac","1","-ar","8000","-f","s16le","-"])
        samples=array("h",data)
        crossings=sum(a<0<=b for a,b in zip(samples,samples[1:]))
        if abs(crossings*8000/max(1,len(samples))-freq)>35 or math.sqrt(sum(x*x for x in samples)/max(1,len(samples)))<120:
            raise SystemExit("The edit did not retain both original sounds in order")
