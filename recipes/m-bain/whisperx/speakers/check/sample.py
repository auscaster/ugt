import subprocess
import wave

# Two stock Flite voices, rendered locally by the same FFmpeg shipped for audio decoding.
parts = []
for voice, text in [
    ("rms", "We counted seven boats in the harbor. We wrote every number in the book."),
    ("slt", "The red train leaves the station at noon. I will meet you there after lunch."),
    ("rms", "The last boat had a blue flag. Put that detail in the book too."),
]:
    audio = subprocess.check_output(["ffmpeg", "-v", "error", "-f", "lavfi", "-i",
        f"flite=voice={voice}:text={text}", "-ar", "16000", "-ac", "1", "-f", "s16le", "-"])
    parts += [audio, bytes(16000)]
with wave.open("inputs/conversation.wav", "wb") as output:
    output.setparams((1, 2, 16000, 0, "NONE", "not compressed"))
    output.writeframes(b"".join(parts))
