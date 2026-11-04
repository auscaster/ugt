import sys
import cv2
import numpy as np
source = cv2.imread("inputs/photo", cv2.IMREAD_COLOR)
result = cv2.imread("outputs/enlarged.png", cv2.IMREAD_COLOR)
if result is None or source is None or result.shape != (source.shape[0]*4, source.shape[1]*4, 3):
    raise SystemExit("The PNG must be four times the size of this picture")
small = cv2.resize(result, source.shape[1::-1], interpolation=cv2.INTER_AREA)
if np.abs(small.astype(float)-source).mean() > 24:
    raise SystemExit("The enlarged picture no longer resembles the source")
if "sample" in sys.argv:
    def edges(img):
        return cv2.Laplacian(cv2.cvtColor(img,cv2.COLOR_BGR2GRAY), cv2.CV_64F).var()
    bicubic = cv2.resize(source, result.shape[1::-1], interpolation=cv2.INTER_CUBIC)
    if edges(result) <= edges(bicubic)*1.05:
        raise SystemExit("The sign's details were not sharper than bicubic enlargement")
