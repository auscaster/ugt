from pathlib import Path
import re
import sys

if sys.argv[1] == 'sample':
    Path('/inputs/english.txt').write_text('This is teh correct address.')
else:
    lines = Path('/outputs/suggestions.txt').read_text(encoding='utf-8').splitlines()
    count = int(re.fullmatch(r'US English proofreading: (\d+) suggestions', lines[0]).group(1))
    assert len(lines) >= 1 + 3 * count
    if sys.argv[1] == 'sample-check':
        assert count >= 1 and 'teh' in '\n'.join(lines) and any(line.startswith('Try: ') and 'the' in line[5:].split(', ') for line in lines)
