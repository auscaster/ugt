"""Render text as pixels. The PDF has nothing to search until OCR runs."""
from PIL import Image, ImageDraw

image = Image.new("RGB", (1800, 650), "white")
draw = ImageDraw.Draw(image)
draw.text((110, 160), "Harbour survey records seven lanterns.", fill="black", font_size=65)
draw.text((110, 300), "Samples arrived on Friday morning.", fill="black", font_size=64)
image.save("inputs/scan.pdf", resolution=240)
