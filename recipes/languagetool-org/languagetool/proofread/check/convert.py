from pathlib import Path
import json
import os
import re
import time
from urllib.error import URLError
from urllib.parse import urlencode
from urllib.request import urlopen

# The pinned server accepts bounded requests. Split on text boundaries when
# possible, preserving every character and keeping UTF-16 surrogate pairs intact.
REQUEST_LIMIT = 15000
BASE = 'http://languagetool:8010/v2'

def sections(text):
    start = 0
    while start < len(text):
        block = text[start:start + REQUEST_LIMIT]
        encoded = block.encode('utf-16-le')
        if len(encoded) > REQUEST_LIMIT * 2:
            # A sliced UTF-16 buffer may end in the first half of a character.
            block = encoded[:REQUEST_LIMIT * 2].decode('utf-16-le', errors='ignore')
        if start + len(block) < len(text):
            paragraph = block.rfind('\n', len(block) // 2)
            if paragraph >= 0:
                block = block[:paragraph + 1]
            else:
                breaks = list(re.finditer(r'\s+', block))
                if breaks:
                    block = block[:breaks[-1].end()]
        yield block
        start += len(block)


def available_languages():
    for attempt in range(90):
        try:
            with urlopen(BASE + '/languages', timeout=5) as response:
                return {entry['longCode']:entry['name'] for entry in json.load(response)}
        except URLError:
            if attempt == 89:
                raise
            time.sleep(1)

try:
    text = Path('/inputs/english.txt').read_text(encoding='utf-8-sig')
except UnicodeDecodeError:
    raise SystemExit('Save the document as UTF-8 plain text and run it again.') from None
if not text.strip():
    raise SystemExit('The text file is empty.')

languages = available_languages()
requested = os.environ.get('PROOFREAD_LANGUAGE', 'en-US').strip().replace('_','-')
language = next((code for code in languages if code.casefold() == requested.casefold()), None)
if language is None:
    raise SystemExit('Choose a supported PROOFREAD_LANGUAGE. Available codes: ' + ', '.join(sorted(languages)))

lines = []
count = 0
line_number = 1
for section in sections(text):
    if not section.strip():
        line_number += section.count('\n')
        continue
    params = urlencode({'text':section, 'language':language}).encode()
    with urlopen(BASE+'/check', data=params, timeout=90) as response:
        checked = json.load(response)
    if checked.get('warnings', {}).get('incompleteResults'):
        raise SystemExit('LanguageTool returned an incomplete check. Try a shorter section.')
    encoded = section.encode('utf-16-le')
    for match in checked['matches']:
        count += 1
        before = encoded[:2 * match['offset']].decode('utf-16-le')
        line = line_number + before.count('\n')
        lines.extend(['', f'Line {line}', match['context']['text'], match['message']])
        replacements = [item['value'] for item in match['replacements'][:5]]
        if replacements:
            lines.append('Try: ' + ', '.join(replacements))
    line_number += section.count('\n')
label = 'US English' if language == 'en-US' else f'{languages[language]} ({language})'
lines.insert(0, f'{label} proofreading: {count} suggestions')
Path('/outputs/suggestions.txt').write_text('\n'.join(lines)+'\n', encoding='utf-8')
