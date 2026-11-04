"""Check dimensions, visible colour and alpha, plus the known subject on the sample."""
import sys
from PIL import Image, ImageChops

with Image.open("inputs/photo") as source, Image.open("outputs/cutout.png") as result:
    if result.format != "PNG" or result.size != source.size or "A" not in result.getbands():
        raise SystemExit("Expected a PNG with an alpha channel and the original dimensions")
    alpha = result.getchannel("A")
    low, high = alpha.getextrema()
    if low > 32 or high < 220:
        raise SystemExit("The cutout must contain a visible subject and transparent background")
    opaque = alpha.point(lambda a: 255 if a > 250 else 0)
    changed = ImageChops.difference(source.convert("RGB"), result.convert("RGB")).convert("L")
    if changed.getextrema()[1] and ImageChops.multiply(opaque, changed).getextrema()[1] > 12:
        raise SystemExit("Opaque foreground colours do not match the input")
    if "sample" in sys.argv:
        for point in [(0,0), (399,0), (0,399), (399,399)]:
            if alpha.getpixel(point) > 15:
                raise SystemExit("The sample's white background is still visible")
        if alpha.getpixel((200,195)) < 240:
            raise SystemExit("The sample apple was erased")
