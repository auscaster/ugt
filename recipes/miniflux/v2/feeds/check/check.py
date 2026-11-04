import base64
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
from pathlib import Path
import socket
import sys
import threading
import time
from urllib.error import URLError, HTTPError
from urllib.request import Request, urlopen

base = 'http://miniflux:8080'
for attempt in range(60):
    try:
        with urlopen(base + '/', timeout=5) as response:
            assert b'name="password"' in response.read()
        break
    except URLError as error:
        if attempt == 59 or 'Name or service not known' in str(error):
            raise
        time.sleep(1)
if 'sample' in sys.argv:
    class Feed(BaseHTTPRequestHandler):
        def do_GET(self):
            content = b'<rss version="2.0"><channel><title>Dock reports</title><link>https://example.org/</link><description>Shipments</description><item><title>Seven anchors arrived</title><guid isPermaLink="false">dock-7</guid><link>https://example.org/anchors</link></item></channel></rss>'
            self.send_response(200)
            self.send_header('Content-Type','application/rss+xml')
            self.end_headers()
            self.wfile.write(content)
    http = HTTPServer(('0.0.0.0',0), Feed)
    threading.Thread(target=http.serve_forever,daemon=True).start()
    login = ('reader:'+Path('/fixture/password').read_text()).encode()
    headers = {'Authorization':'Basic '+base64.b64encode(login).decode(),'Content-Type':'application/json'}
    feed = 'http://'+socket.gethostbyname(socket.gethostname())+':'+str(http.server_port)+'/feed'
    with urlopen(Request(base+'/v1/feeds',data=json.dumps({'feed_url':feed,'category_id':1}).encode(),headers=headers),timeout=30) as response:
        feed_id=json.load(response)['feed_id']
    with urlopen(Request(base+'/v1/feeds/'+str(feed_id)+'/entries',headers=headers),timeout=15) as response:
        assert [x['title'] for x in json.load(response)['entries']] == ['Seven anchors arrived']
