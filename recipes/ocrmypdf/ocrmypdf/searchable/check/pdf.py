"""Use a separate renderer to check the visible pages and the new text layer."""
import sys

import pypdfium2 as pdfium
from PIL import Image, ImageChops, ImageOps, ImageStat

def thumb(page):
    scale = 160 / max(page.get_size())
    return page.render(scale=scale).to_pil().convert("L").resize((128,128),Image.Resampling.BOX)

with pdfium.PdfDocument("inputs/scan.pdf") as source, pdfium.PdfDocument("outputs/searchable.pdf") as result:
    if not len(source) or len(source) != len(result):
        raise SystemExit("The output must preserve the input page count")
    searchable = False
    recovered = set()
    for i in range(len(source)):
        a, b = source[i], result[i]
        if any(abs(x-y)>1 for x,y in zip(a.get_size(),b.get_size())):
            raise SystemExit(f"Page {i+1} changed size")
        before, after = thumb(a), thumb(b)
        ink = ImageStat.Stat(ImageOps.invert(before)).mean[0]
        difference = ImageStat.Stat(ImageChops.difference(before,after)).mean[0]
        if difference > 0.25*ink + 0.35:
            raise SystemExit(f"Page {i+1} no longer resembles the original")
        text = b.get_textpage().get_text_bounded().casefold()
        searchable |= bool(text.strip())
        if "sample" in sys.argv:
            if a.get_textpage().get_text_bounded().strip():
                raise SystemExit("The sample must start as pixels, without searchable text")
            recovered.update(phrase for phrase in ("seven lanterns","friday morning") if phrase in text)
    if not searchable or ("sample" in sys.argv and len(recovered)!=2):
        raise SystemExit("OCR did not recover selectable text from the pages")
