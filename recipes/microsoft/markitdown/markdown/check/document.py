"""Check that the Markdown carries text from the supplied Office document."""
import re
from pathlib import Path
from zipfile import ZipFile

from defusedxml import ElementTree

with ZipFile("inputs/document") as archive:
    parts = [name for name in archive.namelist() if re.fullmatch(
        r"word/document\.xml|ppt/slides/slide\d+\.xml|xl/sharedStrings\.xml|xl/worksheets/sheet\d+\.xml", name)]
    words = set()
    for name in parts:
        for elem in ElementTree.fromstring(archive.read(name)).iter():
            if elem.tag.rsplit("}", 1)[-1] in {"t", "v"} and elem.text:
                words.update(re.findall(r"\w+", elem.text.casefold()))
markdown = Path("outputs/document.md").read_text(encoding="utf-8")
output_words = set(re.findall(r"\w+", markdown.casefold()))
matched = words.intersection(output_words)
if not words or len(matched) < 0.8 * len(words):
    raise SystemExit(f"Only {len(matched)} of {len(words)} distinct source words and values survived conversion")
