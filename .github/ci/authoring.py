"""A client uses only UGT's MCP tools to prepare an uncovered document task.

No model or provider key is needed. This proves the tool path, not a model's
ability to choose these calls. The delivered Word file is inspected separately.
"""
import json
import os
from pathlib import Path
import queue
import re
import shutil
import subprocess
import threading
import time
import xml.etree.ElementTree as ET
from zipfile import ZipFile

if (os.getenv('GITHUB_REPOSITORY') != 'auscaster/ugt'
        or os.getenv('GITHUB_ACTIONS') != 'true'
        or os.getenv('RUNNER_ENVIRONMENT') != 'github-hosted'):
    raise SystemExit('Run only on disposable public hosted runners.')

exe = os.environ['UGT_CI_EXE']
evidence = Path('evidence'); evidence.mkdir(exist_ok=True)
proof = {'head': os.environ['GITHUB_SHA'], 'calls': [], 'passed': False}


def save():
    (evidence / 'authoring.json').write_text(json.dumps(proof, indent=2), encoding='utf-8')


messages = queue.Queue()
log = (evidence / 'mcp.log').open('w', encoding='utf-8')
proc = subprocess.Popen([exe, 'mcp'], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                        stderr=log, text=True, encoding='utf-8')


def read():
    for line in proc.stdout:
        messages.put(json.loads(line))
    messages.put({'ended': True})


threading.Thread(target=read, daemon=True).start()
serial = 0


def rpc(method, params):
    global serial
    serial += 1
    proc.stdin.write(json.dumps({'jsonrpc': '2.0', 'id': serial, 'method': method, 'params': params}) + '\n')
    proc.stdin.flush()
    end = time.monotonic() + 75
    while time.monotonic() < end:
        message = messages.get(timeout=max(1, end-time.monotonic()))
        assert not message.get('ended'), 'MCP server closed'
        if message.get('id') == serial:
            assert 'error' not in message, message
            return message['result']
    raise TimeoutError(method)


def tool(name, args):
    proof['calls'].append(name); save()
    result = rpc('tools/call', {'name': name, 'arguments': args})
    assert not result.get('isError'), result
    return result.get('structuredContent') or json.loads(result['content'][0]['text'])


def complete(answer):
    deadline = time.monotonic()+600
    while answer['state'] in ('queued', 'running'):
        assert time.monotonic() < deadline, answer
        answer = tool('ugt_status', {'job': answer['job'], 'wait_seconds': 40})
    assert answer['state'] == 'succeeded', answer
    return answer['result']


try:
    rpc('initialize', {'protocolVersion': '2025-11-25', 'capabilities': {},
                       'clientInfo': {'name': 'first-use-check', 'version': '1'}})
    proc.stdin.write('{"jsonrpc":"2.0","method":"notifications/initialized"}\n'); proc.stdin.flush()
    offered = {t['name'] for t in rpc('tools/list', {})['tools']}
    assert {'ugt_find', 'ugt_new', 'ugt_verify', 'ugt_run', 'ugt_status'} <= offered
    found = tool('ugt_find', {'query': 'jgm/pandoc'})
    assert not found['recipes'], 'The fixture must exercise a project without a catalogue recipe'
    draft = complete(tool('ugt_new', {'project': 'jgm/pandoc', 'task': 'word',
        'what': 'Turn my Markdown notes into an editable Word document', 'kind': 'job', 'blank': True}))
    manifest = json.loads(draft['contents']['recipe.json'])
    manifest.update(commit='30b135468435dc4fbccfc2694508fa8e3e9ddc33',
        release={'tag':'3.13', 'artifact':'pandoc-3.13-linux-amd64.tar.gz',
                 'sha256':'38aa33b325a2bc96a4c39409631212db55702f564250bcdbead180b3bf646389'},
        inputs=[{'path':'inputs/notes.md', 'what':'Markdown notes with headings and paragraphs'}],
        outputs=[{'path':'outputs/notes.docx', 'what':'an editable Word document'}],
        known_issues=['This qualification fixture covers headings and paragraphs on Linux x64.'],
        packaging='adapted', packaging_note='Use the pinned official release executable.')
    # Checks use the source text and the Word XML, not just a file's existence.
    check = '''import re,sys,zipfile,xml.etree.ElementTree as ET
from pathlib import Path
source=Path('inputs/notes.md').read_text(encoding='utf-8')
root=ET.fromstring(zipfile.ZipFile('outputs/notes.docx').read('word/document.xml'))
ns={'w':'http://schemas.openxmlformats.org/wordprocessingml/2006/main'}
text=' '.join(t.text or '' for t in root.findall('.//w:t',ns))
assert set(re.findall(r'\\w+',source)) <= set(re.findall(r'\\w+',text))
for heading in re.findall(r'^# (.+)$',source,re.M):
 assert any(heading==''.join(t.text or '' for t in p.findall('.//w:t',ns)) and p.find('w:pPr/w:pStyle',ns).attrib.get('{'+ns['w']+'}val')=='Heading1' for p in root.findall('.//w:p',ns) if p.find('w:pPr/w:pStyle',ns) is not None)
if len(sys.argv)>1: assert 'Sample heading' in text
'''
    base = {'image':'${UGT_IMAGE}', 'platform':'linux/${UGT_ARCH}', 'network_mode':'none',
            'working_dir':'/work', 'mem_limit':'256m'}
    mounts = ['./inputs:/work/inputs:ro','./outputs:/work/outputs:ro','./recipe/check:/work/check:ro']
    compose = {'services': {
        'task': {**base,'build':{'context':'./recipe'},'volumes':[mounts[0],'./outputs:/work/outputs'],
                 'command':['inputs/notes.md','-f','markdown','-t','docx','-o','outputs/notes.docx']},
        'check': {**base,'profiles':['check'],'volumes':mounts,'entrypoint':['python','check/document.py']},
        'sample': {**base,'profiles':['sample'],'volumes':['./inputs:/work/inputs'],
                   'entrypoint':['python','-c',"from pathlib import Path;Path('inputs/notes.md').write_text('# Sample heading\\n\\nSome sample notes.\\n')"]},
        'sample-check': {**base,'profiles':['sample'],'volumes':mounts,'entrypoint':['python','check/document.py','sample']}}}
    files = {'recipe.json':json.dumps(manifest), 'compose.yaml':json.dumps(compose), 'check/document.py':check,
        'Dockerfile':'''FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
ADD --checksum=sha256:38aa33b325a2bc96a4c39409631212db55702f564250bcdbead180b3bf646389 https://github.com/jgm/pandoc/releases/download/3.13/pandoc-3.13-linux-amd64.tar.gz /tmp/pandoc.tar.gz
RUN tar -xzf /tmp/pandoc.tar.gz -C /usr/local --strip-components=1 && rm /tmp/pandoc.tar.gz
WORKDIR /work
ENTRYPOINT ["pandoc"]
'''}
    verified = complete(tool('ugt_verify', {'dir':draft['dir'], 'files':files}))
    assert verified['passed'] and verified['check_strength'], verified
    proof['verification'] = {k:verified[k] for k in ('passed','check_strength')}; save()
    # This input is independently chosen after verification, not the sample.
    home = Path.home()/'UGT'; home.mkdir(exist_ok=True)
    source = '# Wharf booking\n\nDeliver 23 blue chairs to Pier 8. Keep the café open.\n'
    (home/'booking.md').write_text(source, encoding='utf-8')
    run = complete(tool('ugt_run', {'project':'jgm/pandoc','task':'word','inputs':['booking.md']}))
    assert run['passed'], run
    output = Path(run['results'])/'notes.docx'
    root = ET.fromstring(ZipFile(output).read('word/document.xml'))
    ns = {'w':'http://schemas.openxmlformats.org/wordprocessingml/2006/main'}
    text = ' '.join(t.text or '' for t in root.findall('.//w:t',ns))
    assert 'Deliver 23 blue chairs to Pier 8. Keep the café open.' in text, text
    assert root.find('.//w:pPr/w:pStyle',ns).attrib['{'+ns['w']+'}val']=='Heading1'
    shutil.copyfile(output,evidence/'booking.docx')
    # A new MCP connection finds the locally authored task for the next use.
    proc.stdin.close(); proc.wait(timeout=10)
    messages = queue.Queue()
    proc = subprocess.Popen([exe, 'mcp'], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                            stderr=log, text=True, encoding='utf-8')
    threading.Thread(target=read, daemon=True).start()
    rpc('initialize', {'protocolVersion':'2025-11-25', 'capabilities':{},
                       'clientInfo':{'name':'returning-user','version':'1'}})
    proc.stdin.write('{"jsonrpc":"2.0","method":"notifications/initialized"}\n'); proc.stdin.flush()
    again = tool('ugt_find', {'query':'jgm/pandoc/word'})
    assert len(again['recipes'])==1 and again['recipes'][0]['source']=='local', again
    proof.update(passed=True,delivered_text=text,source=again['recipes'][0]['source']); save()
except BaseException as exc:
    proof['failure'] = str(exc)[-3500:]; save(); raise
finally:
    proc.stdin.close()
    try: proc.wait(timeout=5)
    except subprocess.TimeoutExpired: proc.terminate(); proc.wait(timeout=5)
    log.close()
