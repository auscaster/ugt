from PIL import Image, ImageDraw
image = Image.new("RGB", (160, 100), (183, 207, 221))
draw = ImageDraw.Draw(image)
draw.rectangle((14, 20, 145, 81),fill=(56, 60, 62))
draw.rectangle((17, 23, 142, 78),outline=(234, 229, 209),width=2)
draw.text((34, 39), "NORTH PIER 27", fill=(245,245,234))
image.save("inputs/photo", "PNG")
