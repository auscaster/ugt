import re

from xlsxwriter import Workbook
from read_image import read_tables

def write_integer(sheet, row, col, text, style):
    # Whole counts can be totalled in Excel. Keep leading zeros and long IDs verbatim.
    if re.fullmatch(r"0|-?[1-9][0-9]{0,14}", text):
        return sheet.write_number(row, col, int(text), style)
    return None


tables = read_tables()
# OCR can produce a formula or URL as ordinary printed text.
options = {"strings_to_formulas": False, "strings_to_urls": False}
with Workbook("outputs/tables.xlsx", options) as workbook:
    style = workbook.add_format({"border": 1, "valign": "vcenter", "text_wrap": True})
    for index, table in enumerate(tables, 1):
        sheet = workbook.add_worksheet(f"Table {index}")
        sheet.add_write_handler(str, write_integer)
        # Use the pinned release's worksheet renderer so merged cells retain their spans.
        table._to_worksheet(sheet, style)
