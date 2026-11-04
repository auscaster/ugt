from PIL import Image
from rembg import new_session, remove

with Image.open("inputs/photo") as image:
    result = remove(image, session=new_session("u2net", providers=["CPUExecutionProvider"]))
    result.save("outputs/cutout.png")
