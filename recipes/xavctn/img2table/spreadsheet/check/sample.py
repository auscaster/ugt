from PIL import Image, ImageDraw, ImageFont

rows = [
    ["Route", "Crates", "Boxes"],
    ["South", "37", "82"],
    ["North", "64", "25"],
    ["West", "19", "43"],
]
image = Image.new("RGB", (1020, 480), "white")
draw = ImageDraw.Draw(image)
font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 34)
for col in range(4):
    x = 45 + col * 306
    draw.line((x, 40, x, 424), fill="black", width=3)
for row in range(5):
    y = 40 + row * 96
    draw.line((45, y, 963, y), fill="black", width=3)
for row, cells in enumerate(rows):
    for col, text in enumerate(cells):
        draw.text((65 + col * 306, 65 + row * 96), text, font=font, fill="black")
image.save("inputs/table.png")
