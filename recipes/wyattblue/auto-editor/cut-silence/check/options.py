import math
import os

def number(name, default, minimum, maximum):
    try:
        value = float(os.environ.get(name, default))
    except ValueError:
        value = math.nan
    if not minimum <= value <= maximum:
        raise SystemExit(f"Choose {name} between {minimum} and {maximum}.")
    return value

margin = number("MARGIN_SECONDS", "0.1", 0, 3600)
threshold = number("AUDIO_THRESHOLD", "0.04", 0, 1)
