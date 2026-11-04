import camelot
from openpyxl import Workbook
from openpyxl.cell.cell import WriteOnlyCell

tables = camelot.read_pdf("inputs/document.pdf",pages="all",flavor="lattice")
if not len(tables):
    raise SystemExit("No ruled tables found. Make sure the PDF has selectable text and visible table borders.")
book = Workbook(write_only=True)
for index,table in enumerate(tables,1):
    sheet = book.create_sheet(f"Table {index} - page {table.page}")
    for values in table.data:
        cells = []
        for value in values:
            cell = WriteOnlyCell(sheet,value=value)
            cell.data_type = "s"  # PDF text beginning with '=' remains text, never an executable Excel formula.
            cells.append(cell)
        sheet.append(cells)
book.save("outputs/tables.xlsx")
