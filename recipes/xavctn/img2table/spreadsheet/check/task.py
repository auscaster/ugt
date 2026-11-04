import re

from xlsxwriter import Workbook
from read_image import read_tables


def write_printed_value(sheet, row, col, text, style):
    # Whole counts can be totalled in Excel. Keep leading zeros and long IDs verbatim.
    if re.fullmatch(r"0|-?[1-9][0-9]{0,14}", text):
        return sheet.write_number(row, col, int(text), style)
    return sheet.write_string(row, col, text, style)


tables = read_tables()
with Workbook("outputs/tables.xlsx") as workbook:
    style = workbook.add_format({"border": 1, "valign": "vcenter", "text_wrap": True})
    for index, table in enumerate(tables, 1):
        sheet = workbook.add_worksheet(f"Table {index}")
        sheet.add_write_handler(str, write_printed_value)
        # Use the pinned release's worksheet renderer so merged cells retain their spans.
        table._to_worksheet(sheet, style)
