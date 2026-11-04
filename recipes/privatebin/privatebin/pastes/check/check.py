import base64, json, secrets, sys, time
from urllib.error import URLError
from urllib.request import Request, urlopen

url = "http://privatebin:8080/"
headers = {"X-Requested-With": "JSONHttpRequest","Content-Type": "application/json"}
def request(path="", data=None):
    req=Request(url+path, data=json.dumps(data).encode() if data else None, headers=headers)
    with urlopen(req, timeout=5) as response:
        return json.load(response)

deadline=time.monotonic()+30
while True:
    try:
        with urlopen(url,timeout=5) as home:
            assert 'PrivateBin' in home.read().decode()
        break
    except URLError as error:
        if "Name or service not known" in str(error):
            raise
        if time.monotonic() >= deadline:
            raise
        time.sleep(.5)
assert request('?'+secrets.token_hex(8))['status']==1
if 'sample' in sys.argv:
    def encoded(n): return base64.b64encode(secrets.token_bytes(n)).decode()
    # The server stores opaque client ciphertext. An independent browser tests encryption and decryption.
    payload={'v':2,'ct':encoded(64),'adata':[[encoded(16),encoded(8),100000,256,128,'aes','gcm','zlib'],'plaintext',0,0],'meta':{'expire':'5min'}}
    written=request(data=payload)
    assert written.get('status')==0,written
    saved=request('?'+written['id'])
    assert saved['ct']==payload['ct'] and saved['adata']==payload['adata']
