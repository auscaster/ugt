from pathlib import Path
import json
import time
from urllib.error import URLError
from urllib.parse import urlencode
from urllib.request import urlopen

text = Path('/inputs/english.txt').read_text(encoding='utf-8-sig')
if not text.strip():
    raise SystemExit('The English text file is empty.')
if len(text.encode('utf-16-le')) // 2 > 15000:
    raise SystemExit('Split the text into sections of at most 15,000 UTF-16 code units.')
base = 'http://languagetool:8010/v2'
for attempt in range(90):
    try:
        with urlopen(base + '/languages', timeout=5) as response:
            if any(row['longCode'] == 'en-US' for row in json.load(response)):
                break
    except URLError:
        if attempt == 89:
            raise
        time.sleep(1)
with urlopen(base+'/check',data=urlencode({'text':text,'language':'en-US'}).encode(),timeout=90) as response:
    matches = json.load(response)['matches']
lines = [f'US English proofreading: {len(matches)} suggestions']
for match in matches:
    context = match['context']['text']
    suggestions = [r['value'] for r in match['replacements'][:5]]
    lines.extend(['',context,match['message']])
    if suggestions:
        lines.append('Try: ' + ', '.join(suggestions))
Path('/outputs/suggestions.txt').write_text('\n'.join(lines)+'\n',encoding='utf-8')
