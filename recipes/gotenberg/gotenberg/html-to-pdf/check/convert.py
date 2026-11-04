from pathlib import Path
import secrets
import time
from urllib.error import URLError
from urllib.request import Request, urlopen

url = 'http://gotenberg:3000'
for attempt in range(60):
    try:
        with urlopen(url + '/health', timeout=3) as response:
            if response.status == 200:
                break
    except URLError:
        if attempt == 59:
            raise
        time.sleep(1)
html = Path('/inputs/index.html').read_bytes()
if not html.strip():
    raise SystemExit('The HTML file is empty.')
boundary = secrets.token_hex(24)
header = f'--{boundary}\r\nContent-Disposition: form-data; name="files"; filename="index.html"\r\nContent-Type: text/html\r\n\r\n'
body = header.encode() + html + f'\r\n--{boundary}--\r\n'.encode()
request = Request(url + '/forms/chromium/convert/html', data=body, headers={'Content-Type':'multipart/form-data; boundary='+boundary})
with urlopen(request, timeout=90) as response:
    if response.headers.get_content_type() != 'application/pdf':
        raise RuntimeError('The renderer did not return a PDF.')
    Path('/outputs/document.pdf').write_bytes(response.read())
