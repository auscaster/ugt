from collections import Counter
import re
import sys
from unicodedata import normalize

from docx import Document
import pymupdf


def words(text):
    return re.findall(r"\w+", normalize("NFKC", text).replace("\u00ad", "").casefold())


def row_values(cells):
    return tuple(" ".join(normalize("NFKC", text).split()) for text in cells)


document = Document("outputs/document.docx")
paragraphs = [line.strip() for node in document.element.body.xpath(".//w:p") for line in node.text.splitlines()]
editable = Counter(words("\n".join(paragraphs)))
if not editable:
    raise SystemExit("The Word file contains no editable text.")

# Follow the source rows in order, including repeated tables on later pages.
rows = iter(row_values(cell.text for cell in row.cells) for table in document.tables for row in table.rows)
with pymupdf.open("inputs/document.pdf") as pdf:
    remaining = editable.copy()
    for number, page in enumerate(pdf, 1):
        source = Counter(words(page.get_text("text")))
        matched = source & remaining
        missing = source - matched
        if any(word.isdecimal() for word in missing) or matched.total() < 0.9 * source.total():
            raise SystemExit(f"The editable Word text is missing content from PDF page {number}.")
        remaining.subtract(matched)
        for table in page.find_tables(strategy="lines_strict").tables:
            grid = table.extract()
            # Check rectangular grids where every cell has an unambiguous position.
            if len(grid) < 2 or table.col_count < 2 or any(None in row for row in grid):
                continue
            for row in grid:
                expected = row_values(row)
                if expected not in rows:
                    raise SystemExit(f"A ruled table row from page {number} is missing or its cells changed.")

if "sample" in sys.argv:
    tables = [[[cell.text.strip() for cell in row.cells] for row in table.rows] for table in document.tables]
    expected = [
        [["Part", "Count"], ["Copper hinge", "28"]],
        [["Part", "Count"], ["Brass latch", "73"]],
    ]
    if tables != expected or not {"Packing list", "Dispatch note"}.issubset(paragraphs):
        raise SystemExit("The two sample headings and editable table cells did not survive conversion.")
