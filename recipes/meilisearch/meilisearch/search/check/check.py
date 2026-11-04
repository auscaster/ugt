import json
import secrets
import sys
import time
from urllib.request import Request, urlopen
from urllib.error import URLError

def call(path, payload=None):
    data = json.dumps(payload).encode() if payload is not None else None
    req = Request('http://meilisearch:7700' + path, data=data, headers={'Content-Type':'application/json'})
    with urlopen(req, timeout=10) as response:
        return json.load(response)

for attempt in range(45):
    try:
        assert 'results' in call('/indexes?limit=1')
        break
    except URLError as error:
        if attempt == 44 or 'Name or service not known' in str(error):
            raise
        time.sleep(1)
if 'sample' in sys.argv:
    index = '/indexes/probe-' + secrets.token_hex(6)
    task = call(index + '/documents?primaryKey=id', [{'id':7,'title':'Amber lighthouse'},{'id':8,'title':'Railway clock'}])
    for attempt in range(40):
        state = call('/tasks/' + str(task['taskUid']))
        if state['status'] == 'succeeded':
            break
        if state['status'] == 'failed':
            raise RuntimeError(state.get('error'))
        time.sleep(.25)
    result = call(index + '/search', {'q':'lighthouse'})
    assert [(row['id'], row['title']) for row in result['hits']] == [(7,'Amber lighthouse')]
