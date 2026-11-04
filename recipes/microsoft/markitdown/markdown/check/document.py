"""Compare the Markdown to actual Office text, not shared-string indices or date serials."""
import re
from datetime import date, datetime
from pathlib import Path
from zipfile import ZipFile

from defusedxml import ElementTree

path = Path("inputs/document")
words = set()
def record(value):
    if isinstance(value, (date, datetime)):
        value = value.strftime("%Y-%m-%d")
    if value is not None:
        words.update(re.findall(r"\w+", str(value).casefold()))

with ZipFile(path) as archive:
    if "xl/workbook.xml" in archive.namelist():
        from openpyxl import load_workbook
        with path.open("rb") as file:
            book = load_workbook(file,read_only=True,data_only=True)
            for sheet in book.worksheets:
                for row in sheet.values:
                    for value in row:
                        record(value)
            book.close()
    else:
        for name in archive.namelist():
            if re.fullmatch(r"word/document\.xml|ppt/slides/slide\d+\.xml",name):
                for elem in ElementTree.fromstring(archive.read(name)).iter():
                    if elem.tag.rsplit("}",1)[-1] == "t":
                        record(elem.text)
markdown = Path("outputs/document.md").read_text(encoding="utf-8")
output_words = set(re.findall(r"\w+", markdown.casefold()))
matched = words & output_words
if not words or len(matched) < 0.8 * len(words):
    raise SystemExit(f"Only {len(matched)} of {len(words)} distinct source words and values survived conversion")
