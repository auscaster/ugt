import re
import sys
from collections import Counter
from openpyxl import load_workbook
import pypdfium2 as pdfium


def words(text):
    return re.findall(r"\w+", text.casefold())


sample = [["Crossing","Distance","Lanterns"],["North jetty","37 metres","16"],["South pier","82 metres","25"]]
book = load_workbook("outputs/tables.xlsx", read_only=True)
known = False
with pdfium.PdfDocument("inputs/document.pdf") as pdf:
    remaining = [Counter(words(page.get_textpage().get_text_bounded())) for page in pdf]
    for index, sheet in enumerate(book, 1):
        match = re.fullmatch(rf"Table {index} - page ([1-9]\d*)", sheet.title)
        if not match or int(match[1]) > len(pdf):
            raise SystemExit("The worksheet does not identify a page in this PDF")
        rows = []
        for row in sheet:
            if any(cell.data_type == "f" for cell in row):
                raise SystemExit("PDF text must not turn into an executable Excel formula")
            rows.append([str(cell.value if cell.value is not None else "").strip() for cell in row])
        cells = Counter(words(" ".join(value for row in rows for value in row)))
        page_words = remaining[int(match[1])-1]
        if not cells or max(map(len,rows)) < 2 or cells - page_words:
            raise SystemExit("The worksheet contains a value or table not found on its PDF page")
        page_words.subtract(cells)
        known |= rows == sample
if not book.sheetnames or ("sample" in sys.argv and not known):
    raise SystemExit("Workbook does not contain the expected table and cells")
