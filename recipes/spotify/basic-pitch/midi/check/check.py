import subprocess
import sys
import pretty_midi

duration = float(subprocess.check_output(["ffprobe","-v","error","-show_entries","format=duration","-of","default=nk=1:nw=1","inputs/recording"]))
midi = pretty_midi.PrettyMIDI("outputs/notes.mid")
notes = sorted((n for instrument in midi.instruments for n in instrument.notes),key=lambda n:n.start)
if not notes or any(n.end>duration+0.25 or n.start>=n.end or n.velocity<=0 for n in notes):
    raise SystemExit("The MIDI must contain playable notes within the recording")
if "sample" in sys.argv:
    played = [n.pitch for n in notes if n.end-n.start>.25]
    if played != [60,64,67]:
        raise SystemExit(f"Expected C4, E4, G4 in order; got {played}")
