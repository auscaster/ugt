from http.cookiejar import CookieJar
import json
import sys
import time
from urllib.error import URLError
from urllib.request import build_opener, HTTPCookieProcessor, Request

base = 'http://grist:8484'
client = build_opener(HTTPCookieProcessor(CookieJar()))
def api(path, body=None):
    headers = {'Content-Type':'application/json','X-Requested-With':'XMLHttpRequest'}
    req = Request(base + path, data=json.dumps(body).encode() if body is not None else None, headers=headers)
    with client.open(req,timeout=30) as response:
        return json.load(response)
for attempt in range(90):
    try:
        with client.open(base+'/login?next=/',timeout=5) as page:
            page.read()
        workspaces=api('/api/orgs/docs/workspaces')
        assert isinstance(workspaces,list) and workspaces
        break
    except URLError as error:
        if attempt==89 or 'Name or service not known' in str(error):raise
        time.sleep(1)
if 'sample' in sys.argv:
    doc = api('/api/workspaces/'+str(workspaces[0]['id'])+'/docs',{'name':'Stock manifest'})
    table = api('/api/docs/'+doc+'/tables',{'tables':[{'id':'Inventory','columns':[{'id':'Quantity','fields':{'type':'Numeric'}},{'id':'Doubled','fields':{'type':'Numeric','isFormula':True,'formula':'$Quantity * 2'}}]}]})['tables'][0]['id']
    path='/api/docs/'+doc+'/tables/'+table+'/records'
    api(path,{'records':[{'fields':{'Quantity':17}}]})
    rows = api(path)['records']
    assert len(rows)==1 and rows[0]['fields']['Quantity']==17 and rows[0]['fields']['Doubled']==34,rows
