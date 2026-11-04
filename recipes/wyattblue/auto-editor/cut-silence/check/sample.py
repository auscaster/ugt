import subprocess
# Two different audible passages separated by three seconds of silence.
subprocess.run(["ffmpeg","-v","error","-f","lavfi","-i","testsrc2=size=160x96:rate=20:duration=5",
                "-f","lavfi","-i",r"aevalsrc=if(lt(t\,1)\,0.4*sin(2*PI*440*t)\,if(gt(t\,4)\,0.4*sin(2*PI*880*t)\,0)):s=16000:d=5",
                "-c:v","libx264","-c:a","aac","-shortest","inputs/video.mp4"],check=True)
