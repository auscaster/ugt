import re
import sys
from pathlib import Path
import pypdfium2 as pdfium

markdown = Path("outputs/document.md").read_text(encoding="utf-8")
words = lambda text: set(re.findall(r"\w+",text.casefold()))
source = set()
with pdfium.PdfDocument("inputs/document.pdf") as pdf:
    for page in pdf:
        source.update(words(page.get_textpage().get_text_bounded()))
if not source or len(source & words(markdown)) < 0.75*len(source):
    raise SystemExit("Could not recover the PDF text. Image-only pages need OCR first.")
if "sample" in sys.argv:
    rows = [[c.strip() for c in row.strip().strip("|").split("|")] for row in markdown.splitlines() if "|" in row]
    for wanted in [["Crossing","Distance","Lanterns"],["North jetty","37 metres","16"],["South pier","82 metres","25"]]:
        if wanted not in rows:
            raise SystemExit(f"Missing table row: {wanted}")
