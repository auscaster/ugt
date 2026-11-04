"""Read the output through a separate PDF renderer and inspect its text layer."""
import sys
import pypdfium2 as pdfium

source = pdfium.PdfDocument("inputs/scan.pdf")
result = pdfium.PdfDocument("outputs/searchable.pdf")
if not len(source) or len(source) != len(result):
    raise SystemExit("The output must preserve the input page count")

def text(pdf):
    return " ".join(page.get_textpage().get_text_bounded() for page in pdf).casefold()

searchable = text(result)
if not searchable.strip():
    raise SystemExit("The PDF has no selectable text")
if "sample" in sys.argv:
    if text(source).strip() or not all(phrase in searchable for phrase in ("seven lanterns", "friday morning")):
        raise SystemExit("OCR did not recover the words on the image-only sample")
