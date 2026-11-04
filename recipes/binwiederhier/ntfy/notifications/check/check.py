import json
import secrets
import sys
from urllib.request import Request, urlopen
from urllib.error import URLError
import time

base = 'http://ntfy:80'
for attempt in range(30):
    try:
        with urlopen(base + '/v1/health', timeout=3) as response:
            assert json.load(response)['healthy']
        break
    except URLError as error:
        if attempt == 29 or 'Name or service not known' in str(error):
            raise
        time.sleep(1)
topic = 'check-' + secrets.token_hex(10)
if 'sample' in sys.argv:
    message = 'Delivery ' + secrets.token_hex(8)
    with urlopen(Request(base + '/' + topic, data=message.encode()), timeout=8) as response:
        posted = json.load(response)
    with urlopen(base + '/' + topic + '/json?poll=1', timeout=8) as response:
        messages = [json.loads(line) for line in response]
    assert any(obj['id'] == posted['id'] and obj['message'] == message for obj in messages)
else:
    with urlopen(base + '/' + topic + '/json?poll=1', timeout=8) as response:
        assert response.read().strip() == b''
