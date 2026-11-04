import sys
from openpyxl import load_workbook
from read_image import read_tables

book = load_workbook("outputs/tables.xlsx")
tables = read_tables()
if len(tables) != len(book.worksheets):
    raise SystemExit("The workbook does not have one sheet per detected table.")

for sheet, table in zip(book.worksheets, tables):
    spans = {coord: sheet.cell(area.min_row, area.min_col)
             for area in sheet.merged_cells.ranges for coord in area.cells}
    rows = list(table.content.values())
    if sheet.max_row != len(rows) or sheet.max_column != max(map(len, rows)):
        raise SystemExit("The number of rows or columns changed.")
    for y, row in enumerate(rows, 1):
        for x, original in enumerate(row, 1):
            cell = spans.get((y, x), sheet.cell(y, x))
            if cell.data_type == "f" or cell.hyperlink or (cell.value or "") != (original.value or ""):
                raise SystemExit(f"Cell {sheet.title}!{cell.coordinate} does not match the printed table.")

if "sample" in sys.argv:
    expected = [("Route", "Crates", "Boxes"), ("South", "37", "82"),
                ("North", "64", "25"), ("West", "19", "43")]
    if len(book.worksheets) != 1 or list(book.active.values) != expected:
        raise SystemExit("The sample headings or quantities are in the wrong cells.")
