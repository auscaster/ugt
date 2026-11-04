import time
from urllib.request import urlopen
from urllib.error import URLError, HTTPError
for attempt in range(90):
    try:
        with urlopen('http://linkding:9090/login/',timeout=5) as page:
            assert b'csrfmiddlewaretoken' in page.read()
        break
    except URLError as error:
        if attempt == 89 or 'Name or service not known' in str(error):
            raise
        time.sleep(1)
try:
    urlopen('http://linkding:9090/api/bookmarks/',timeout=5)
except HTTPError as error:
    assert error.code in (401,403)
else:
    raise RuntimeError('The bookmark library accepted an anonymous request')
