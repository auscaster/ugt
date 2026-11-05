import os
from options import margin, threshold

os.execlp("auto-editor", "auto-editor", "inputs/video.mp4",
          "--edit", f"audio:threshold={threshold}", "--margin", f"{margin}sec",
          "--no-open", "-o", "outputs/edited.mp4")
