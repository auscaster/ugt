import json
import secrets
import sys
import time
from urllib.error import URLError
from urllib.request import Request, urlopen

def call(path, data=None, token=None):
    headers = {'Content-Type':'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = Request('http://memos:5230' + path, headers=headers, data=json.dumps(data).encode() if data is not None else None)
    with urlopen(req, timeout=10) as response:
        return json.load(response)

for attempt in range(45):
    try:
        profile = call('/api/instance/profile')
        assert profile['version']
        break
    except URLError as error:
        if attempt == 44 or 'Name or service not known' in str(error):
            raise
        time.sleep(1)
if 'sample' in sys.argv:
    credentials = {'username':'reader','password':secrets.token_urlsafe(24)}
    call('/api/users', credentials)
    token = call('/api/auth/signin', {'passwordCredentials':credentials})['accessToken']
    note = 'Pier inspection ' + secrets.token_hex(6)
    result = call('/api/memos', {'content':note,'visibility':'PRIVATE'}, token)
    saved = call('/api/' + result['name'], token=token)
    assert saved['content'] == note and saved['visibility'] == 'PRIVATE'
