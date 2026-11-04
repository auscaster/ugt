from pathlib import Path
from argostranslate.translate import translate

source = Path("inputs/english.txt").read_text(encoding="utf-8-sig")
Path("outputs/spanish.txt").write_text(translate(source, "en", "es") + "\n", encoding="utf-8")
