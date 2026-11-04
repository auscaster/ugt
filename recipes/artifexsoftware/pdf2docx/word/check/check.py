from collections import Counter
import re
import sys
from unicodedata import normalize

from docx import Document
import pymupdf


def words(text):
    return re.findall(r"\w+", normalize("NFKC", text).replace("\u00ad", "").casefold())


document = Document("outputs/document.docx")
paragraphs = ["".join(node.xpath(".//w:t/text()")) for node in document.element.body.xpath(".//w:p")]
editable = Counter(words("\n".join(paragraphs)))
if not editable:
    raise SystemExit("The Word file contains no editable text.")

with pymupdf.open("inputs/document.pdf") as pdf:
    remaining = editable.copy()
    for number, page in enumerate(pdf, 1):
        source = Counter(words(page.get_text("text")))
        matched = source & remaining
        missing = source - matched
        if any(word.isdecimal() for word in missing) or matched.total() < 0.9 * source.total():
            raise SystemExit(f"The editable Word text is missing content from PDF page {number}.")
        remaining.subtract(matched)

if "sample" in sys.argv:
    tables = [[[cell.text.strip() for cell in row.cells] for row in table.rows] for table in document.tables]
    expected = [
        [["Part", "Count"], ["Copper hinge", "28"]],
        [["Part", "Count"], ["Brass latch", "73"]],
    ]
    if tables != expected or not {"Packing list", "Dispatch note"}.issubset(paragraphs):
        raise SystemExit("The two sample headings and editable table cells did not survive conversion.")
