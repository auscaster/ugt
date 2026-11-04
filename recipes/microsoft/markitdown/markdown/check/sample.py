"""Write a small Office workbook with known cells, without any external data."""
from openpyxl import Workbook

book = Workbook()
sheet = book.active
sheet.title = "Field notes"
sheet.append(["Site", "Bird count"])
sheet.append(["Harbour", 37])
sheet.append(["Wetland", 82])
book.save("inputs/document")
