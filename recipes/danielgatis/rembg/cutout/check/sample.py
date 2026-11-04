"""An isolated red apple, drawn on a white background."""
from PIL import Image, ImageDraw

canvas = Image.new("RGB", (400, 400), "white")
ink = ImageDraw.Draw(canvas)
ink.ellipse((85, 110, 247, 304), fill=(202, 35, 38))
ink.ellipse((160, 109, 316, 305), fill=(202, 35, 38))
ink.line((201, 135, 210, 84), fill=(89, 54, 29), width=10)
ink.ellipse((209, 69, 269, 105), fill=(59, 133, 49))
canvas.save("inputs/photo", "PNG")
