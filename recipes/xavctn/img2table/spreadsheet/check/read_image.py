from img2table.document import Image
from img2table.ocr import TesseractOCR


def read_tables():
    image = Image(src="inputs/table.png")
    tables = image.extract_tables(ocr=TesseractOCR(n_threads=1, lang="eng"))
    if not tables:
        raise SystemExit("No ruled table was found. Use a clear image with visible row and column borders.")
    for table in tables:
        if not any(cell.value for cells in table.content.values() for cell in cells):
            raise SystemExit("A table was detected but no text could be read. Try a sharper image.")
    return tables
