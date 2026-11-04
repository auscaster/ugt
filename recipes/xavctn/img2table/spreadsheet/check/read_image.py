from io import BytesIO
from pathlib import Path
import subprocess

import cv2
import numpy as np
from img2table.document import Image as TableImage
from PIL import Image, ImageOps
import pypdfium2 as pdfium


def pages(path):
    with path.open("rb") as stream:
        is_pdf = stream.read(5) == b"%PDF-"
    if is_pdf:
        with pdfium.PdfDocument(path) as pdf:
            for page in pdf:
                bitmap = page.render(scale=200 / 72)
                try:
                    yield bitmap.to_pil().convert("RGB")
                finally:
                    bitmap.close()
                    page.close()
    else:
        with Image.open(path, formats=("JPEG", "PNG")) as image:
            yield image.convert("RGB")


def read_cell(image, box):
    # Exclude the border from OCR, then put back a small plain margin.
    x1, y1, x2, y2 = box
    inset = max(1, min(5, (x2 - x1) // 20, (y2 - y1) // 20))
    if x2 - x1 <= 2 * inset or y2 - y1 <= 2 * inset:
        return ""
    crop = image.crop((x1 + inset, y1 + inset, x2 - inset, y2 - inset))
    grey = crop.convert("L")
    _, ink = cv2.threshold(np.asarray(grey), 0, 255, cv2.THRESH_BINARY_INV | cv2.THRESH_OTSU)
    text_bounds = Image.fromarray(ink).getbbox()
    if text_bounds is None:
        return ""
    data = BytesIO()
    ImageOps.expand(grey.crop(text_bounds), border=10, fill=255).save(data, format="PNG")
    result = subprocess.run(
        ["tesseract", "stdin", "stdout", "-l", "eng", "--psm", "6"],
        input=data.getvalue(), stdout=subprocess.PIPE, check=True,
    )
    return result.stdout.decode("utf-8").strip()


def read_tables():
    result = []
    for number, image in enumerate(pages(Path("inputs/source")), 1):
        encoded = BytesIO()
        image.save(encoded, format="PNG")
        tables = TableImage(src=encoded).extract_tables()
        if not tables:
            raise SystemExit(f"No ruled table found on page {number}. Use a clear table with visible borders.")
        for table in tables:
            values = {}
            for cells in table.content.values():
                for cell in cells:
                    b = cell.bbox
                    box = (b.x1, b.y1, b.x2, b.y2)
                    if box not in values:
                        values[box] = read_cell(image, box)
                    cell.value = values[box] or None
            if not any(values.values()):
                raise SystemExit(f"The table on page {number} contains no readable text.")
        result.extend(tables)
    if not result:
        raise SystemExit("No table pages found.")
    return result
