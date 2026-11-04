import cv2
import torch
from basicsr.archs.rrdbnet_arch import RRDBNet
from utils import RealESRGANer  # realesrgan/utils.py in the pinned upstream source

model = RRDBNet(num_in_ch=3, num_out_ch=3, num_feat=64, num_block=23, num_grow_ch=32, scale=4)
upscaler = RealESRGANer(scale=4, model_path="/models/x4plus.pth", model=model,
                        tile=128, tile_pad=10, half=False, device=torch.device("cpu"))
image = cv2.imread("inputs/photo", cv2.IMREAD_COLOR)
if image is None:
    raise SystemExit("Could not open this photo")
result, _ = upscaler.enhance(image, outscale=4)
if not cv2.imwrite("outputs/enlarged.png", result):
    raise SystemExit("Could not save the enlarged image")
