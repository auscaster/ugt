from reportlab.lib import colors
from reportlab.lib.styles import getSampleStyleSheet
from reportlab.platypus import Paragraph, SimpleDocTemplate, Spacer, Table, TableStyle

doc = SimpleDocTemplate("inputs/document.pdf")
table = Table([["Crossing","Distance","Lanterns"],["North jetty","37 metres","16"],["South pier","82 metres","25"]], colWidths=[120,120,120], rowHeights=[34]*3)
table.setStyle(TableStyle([("GRID",(0,0),(-1,-1),1,colors.black),("BACKGROUND",(0,0),(-1,0),colors.lightgrey)]))
doc.build([Paragraph("Harbour survey",getSampleStyleSheet()["Heading1"]),Spacer(1,18),table])
