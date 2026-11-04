"""The two counts must appear in their original rows and columns."""
from pathlib import Path

text = Path("outputs/document.md").read_text(encoding="utf-8")
rows = [[cell.strip() for cell in line.strip().strip("|").split("|")]
        for line in text.splitlines() if "|" in line]
for expected in [["Site", "Bird count"], ["Harbour", "37"], ["Wetland", "82"]]:
    if expected not in rows:
        raise SystemExit(f"Missing Markdown table row: {expected}")
