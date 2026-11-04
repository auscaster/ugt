from pdf2docx import Converter
import pymupdf

source = "inputs/document.pdf"
with pymupdf.open(source) as pdf:
    if pdf.needs_pass:
        raise SystemExit("Unlock this PDF before converting it to Word.")
    text_pages = 0
    for index, page in enumerate(pdf):
        if page.get_text("text").strip():
            text_pages += 1
        elif page.get_image_info() or page.get_drawings():
            raise SystemExit(f"Page {index + 1} has no selectable text. Run OCR on the PDF first.")
    if not text_pages:
        raise SystemExit("The PDF has no selectable text to edit. Run OCR on it first.")

converter = Converter(source)
try:
    converter.convert("outputs/document.docx", ignore_page_error=False)
finally:
    converter.close()
