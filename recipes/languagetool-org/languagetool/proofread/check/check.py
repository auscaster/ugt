from pathlib import Path
import re
import sys

if sys.argv[1] == 'sample':
    middle = ''.join(f'Order {i} was checked at the warehouse.\n' for i in range(470))
    Path('/inputs/english.txt').write_text('This is teh correct address.\n' + middle + 'Ship the boxes in a seperate crate.')
else:
    lines = Path('/outputs/suggestions.txt').read_text(encoding='utf-8').splitlines()
    count = int(re.fullmatch(r'.+ proofreading: (\d+) suggestions', lines[0]).group(1))
    assert len(lines) >= 1 + 4 * count
    if sys.argv[1] == 'sample-check':
        assert lines[0].startswith('US English proofreading:')
        assert count >= 1 and 'teh' in '\n'.join(lines) and any(line.startswith('Try: ') and 'the' in line[5:].split(', ') for line in lines)
        assert 'Line 1' in lines and 'Line 472' in lines
        assert any(line.startswith('Try: ') and 'separate' in line[5:].split(', ') for line in lines)
