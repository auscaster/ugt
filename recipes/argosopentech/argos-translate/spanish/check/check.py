import re
import sys
import unicodedata
from pathlib import Path
source=Path("inputs/english.txt").read_text(encoding="utf-8-sig").strip()
result=Path("outputs/spanish.txt").read_text(encoding="utf-8").strip()
if not result or not source or len(result)<len(source)/10:
    raise SystemExit("The translation is empty or incomplete")
if "sample" in sys.argv:
    plain="".join(c for c in unicodedata.normalize("NFD",result.casefold()) if not unicodedata.combining(c))
    words=set(re.findall(r"\w+",plain))
    if not words & {"barco","bote","barca","embarcacion"} or not words & {"rojo","roja"} or "rio" not in words:
        raise SystemExit("Expected a red boat in a river; got: "+result)
