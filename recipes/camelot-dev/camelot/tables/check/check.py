import re
import sys
from openpyxl import load_workbook
import pypdfium2 as pdfium

def words(text):
    return set(re.findall(r"\w+",text.casefold()))

source = set()
with pdfium.PdfDocument("inputs/document.pdf") as pdf:
    for page in pdf:
        source |= words(page.get_textpage().get_text_bounded())
sample = [["Crossing","Distance","Lanterns"],["North jetty","37 metres","16"],["South pier","82 metres","25"]]
book = load_workbook("outputs/tables.xlsx",read_only=True)
count, known = 0, False
for sheet in book:
    rows = [[str(cell or "").strip() for cell in row] for row in sheet.values]
    cells = words(" ".join(value for row in rows for value in row))
    if not cells or not cells & source or max(map(len,rows)) < 2:
        raise SystemExit("A worksheet is empty or does not contain a table from the PDF")
    count += 1
    known |= rows == sample
if not count or ("sample" in sys.argv and not known):
    raise SystemExit("Workbook does not contain the expected table and cells")
