import pymupdf

with pymupdf.open() as pdf:
    for heading, item, quantity in [
        ("Packing list", "Copper hinge", "28"),
        ("Dispatch note", "Brass latch", "73"),
    ]:
        page = pdf.new_page()
        page.insert_text((60, 64), heading, fontsize=20)
        page.insert_text((60, 101), "Please check the count before loading.", fontsize=12)
        for x in (60, 290, 440):
            page.draw_line((x, 140), (x, 212))
        for y in (140, 176, 212):
            page.draw_line((60, y), (440, y))
        for x, y, text in [(70, 164, "Part"), (300, 164, "Count"), (70, 200, item), (300, 200, quantity)]:
            page.insert_text((x, y), text, fontsize=12)
    pdf.save("inputs/document.pdf")
