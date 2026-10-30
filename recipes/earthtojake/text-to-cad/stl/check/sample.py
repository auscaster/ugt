"""A phone stand: base, front lip and a back rest tilted to 65 degrees."""
from pathlib import Path

import build123d as bd
from cadgen import export, model

W = 70.0      # stand width (X)
D = 90.0      # base depth (Y)
T = 5.0       # wall thickness
H = 90.0      # back rest length
ANGLE = 65.0  # back rest angle from horizontal
LIP_H = 16.0  # front lip height


@model
def phone_stand():
    base = bd.Box(W, D, T, align=(bd.Align.CENTER, bd.Align.MIN, bd.Align.MIN))
    lip = bd.Box(W, T, LIP_H, align=(bd.Align.CENTER, bd.Align.MIN, bd.Align.MIN))
    back = bd.Box(W, T, H, align=(bd.Align.CENTER, bd.Align.MIN, bd.Align.MIN))
    # tilt the back rest backwards about its bottom edge, then place it at the rear of the base
    back = bd.Rot(X=-(90 - ANGLE)) * back
    back = bd.Pos(0, D - 35, T - 0.5) * back
    body = base + lip + back
    body.label = "phone_stand"
    return body


if __name__ == "__main__":
    export(phone_stand(), Path("/work/outputs/phone_stand.stl"))
