import os
import sys
import secrets
from urllib.request import Request, urlopen
import json
sys.path.insert(0,'/etc/linkding')
os.environ.setdefault('DJANGO_SETTINGS_MODULE','bookmarks.settings')
import django
django.setup()
from django.contrib.auth import get_user_model
from bookmarks.models import ApiToken
user = get_user_model().objects.create_user(username='sample-'+secrets.token_hex(4))
token = ApiToken.objects.create(user=user,name='verification').key
headers = {'Authorization':'Token '+token,'Content-Type':'application/json'}
url = 'http://linkding:9090/api/bookmarks/'
payload = {'url':'https://example.org/dock','title':'Dock inventory','tag_names':['harbour'],'description':'Seven anchors'}
with urlopen(Request(url,headers=headers,data=json.dumps(payload).encode()),timeout=30) as response:
    bookmark = json.load(response)
with urlopen(Request(url+str(bookmark['id'])+'/',headers=headers),timeout=10) as response:
    saved = json.load(response)
assert saved['url']==payload['url'] and saved['title']=='Dock inventory' and saved['tag_names']==['harbour']
