import json
import sys
import time
from pathlib import Path
from urllib.error import URLError
from urllib.request import Request,urlopen

base = 'http://changedetection:5000'
for attempt in range(60):
    try:
        with urlopen(base,timeout=5) as page:
            assert b'watch' in page.read().lower()
        break
    except URLError as error:
        if attempt==59 or 'Name or service not known' in str(error):raise
        time.sleep(1)
if 'sample' in sys.argv:
    token=json.loads(Path('/datastore/changedetection.json').read_text())['settings']['application']['api_access_token']
    def api(path, payload=None, method=None):
        req=Request(base+'/api/v1'+path, data=json.dumps(payload).encode() if payload else None,method=method,headers={'x-api-key':token,'Content-Type':'application/json'})
        with urlopen(req,timeout=10) as response:
            return response.read()
    # The public echo returns each request header as page content. Vary that
    # header to exercise two snapshots without allowing requests to private IPs.
    target = 'https://httpbingo.org/headers'
    watch = json.loads(api('/watch', {'url':target, 'title':'Stock check',
                           'headers':{'X-Stock-Edition':'Amber-lamps'}, 'fetch_backend':'html_requests'}))['uuid']
    for value in ['Amber-lamps','Cobalt-lamps']:
        if value == 'Cobalt-lamps':
            api('/watch/'+watch, {'headers':{'X-Stock-Edition':value}}, method='PUT')
        api('/watch/'+watch+'?recheck=1')
        for attempt in range(65):
            history=json.loads(api('/watch/'+watch+'/history'))
            if history and value.encode() in api('/watch/'+watch+'/history/'+max(history,key=int)):
                break
            time.sleep(1)
        else:
            detail=json.loads(api('/watch/'+watch))
            raise RuntimeError(f"The monitor did not record {value}: {detail.get('last_error')}")
