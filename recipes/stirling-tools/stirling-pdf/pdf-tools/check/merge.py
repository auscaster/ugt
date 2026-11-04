"""Merge PDFs through the running service, then open and check each resulting page."""
import re
import subprocess
from pathlib import Path
import sys
import time
import urllib.error
import urllib.request
from uuid import uuid4
def blank_page(width, height):
    # Minimal valid PDF with one page. Its distinct dimensions identify it after the merge.
    pdf = bytearray(b"%PDF-1.4\n")
    offsets = [0]
    for i, obj in enumerate(["<< /Type /Catalog /Pages 2 0 R >>",
                             "<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
                             f"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 {width} {height}] /Resources <<>> >>"], 1):
        offsets.append(len(pdf))
        pdf.extend(f"{i} 0 obj\n{obj}\nendobj\n".encode())
    xref = len(pdf)
    pdf.extend(b"xref\n0 4\n0000000000 65535 f \n")
    for offset in offsets[1:]:
        pdf.extend(f"{offset:010} 00000 n \n".encode())
    pdf.extend(f"trailer\n<< /Root 1 0 R /Size 4 >>\nstartxref\n{xref}\n%%EOF\n".encode())
    return bytes(pdf)

host = "http://app:8080"
# Startup can take a minute. A missing service (including after stop) fails at once.
deadline = time.monotonic() + 100
while True:
    try:
        with urllib.request.urlopen(host + "/", timeout=4) as response:
            if response.status == 200:
                break
    except (urllib.error.URLError, TimeoutError) as exc:
        if time.monotonic() >= deadline or "Name or service not known" in str(exc):
            raise
    time.sleep(2)

sizes = [(377,462),(513,692),(751,600)] if "sample" in sys.argv else [(320,400),(580,750)]
boundary = uuid4().hex
parts = []
for n, size in enumerate(sizes):
    part = (f'--{boundary}\r\nContent-Disposition: form-data; name="fileInput"; filename="{n}.pdf"\r\n'
            'Content-Type: application/pdf\r\n\r\n').encode()
    parts.extend([part, blank_page(*size), b"\r\n"])
parts.append(f"--{boundary}--\r\n".encode())
req = urllib.request.Request(host + "/api/v1/general/merge-pdfs", data=b"".join(parts),
                             headers={"Content-Type":"multipart/form-data; boundary="+boundary})
with urllib.request.urlopen(req, timeout=40) as response:
    Path("/tmp/merged.pdf").write_bytes(response.read())
count = int(subprocess.check_output(["qpdf","--show-npages","/tmp/merged.pdf"]))
if count != len(sizes):
    raise SystemExit("The merged PDF did not contain every supplied page")
info = subprocess.check_output(["pdfinfo","-f","1","-l",str(count),"/tmp/merged.pdf"],text=True)
actual = [(float(w),float(h)) for w,h in re.findall(r"Page\s+\d+ size:\s+([\d.]+) x ([\d.]+) pts",info)]
if actual != sizes:
    raise SystemExit(f"Wrong pages or order: expected {sizes}, got {actual}")
