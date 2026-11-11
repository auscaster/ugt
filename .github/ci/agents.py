"""Check first-use client configuration and the actual configured MCP transport.

This uses real client CLIs and a protocol client, without a model/API credential.
It proves configuration + local tools, not a signed-in model's decisions or UI.
"""
import json, os, queue, subprocess, sys, threading, time
from pathlib import Path

if (os.getenv('GITHUB_REPOSITORY') != 'usegeekthing/ugt'
        or os.getenv('GITHUB_ACTIONS') != 'true'
        or os.getenv('RUNNER_ENVIRONMENT') != 'github-hosted'):
    raise SystemExit('Use a disposable public hosted runner only.')

home = Path.home()
windows = os.name == 'nt'
config = Path(os.getenv('APPDATA', home/'.config'))
files = {
    'claude-desktop': config/'Claude/claude_desktop_config.json',
    'cursor': home/'.cursor/mcp.json',
    'vscode': config/'Code/User/mcp.json',
    'gemini': home/'.gemini/settings.json',
}

if sys.argv[1:] == ['prepare']:
    for name, file in files.items():
        file.parent.mkdir(parents=True, exist_ok=True)
        file.write_text(json.dumps({'firstUseFixture': 'keep this setting'}), encoding='utf-8')
    raise SystemExit(0)

evidence = Path('evidence'); evidence.mkdir(exist_ok=True)
proof = {'head':os.environ['GITHUB_SHA'], 'configured':[], 'tool_calls':[], 'passed':False,
         'limits':'Real client configuration and independent MCP transport; no model or signed-in client UI.'}
def save():
    (evidence/'agents.json').write_text(json.dumps(proof,indent=2), encoding='utf-8')

def command(args):
    # Windows npm entry points are .cmd files. cmd handles only these fixed CLI
    # arguments, never project notes or credentials.
    if windows:
        args = ['cmd', '/d', '/c', *args]
    return subprocess.check_output(args, text=True, encoding='utf-8', timeout=90)

class MCP:
    def __init__(self, entry):
        self.log = (evidence/'agent-mcp.log').open('a', encoding='utf-8')
        self.proc = subprocess.Popen([entry['command'], *entry['args']], stdin=subprocess.PIPE,
            stdout=subprocess.PIPE, stderr=self.log, text=True, encoding='utf-8')
        self.messages = queue.Queue(); self.serial = 0
        def read():
            for line in self.proc.stdout:
                try: self.messages.put(json.loads(line))
                except ValueError: self.messages.put({'invalid_json':True})
            self.messages.put({'ended':True})
        threading.Thread(target=read, daemon=True).start()
        self.rpc('initialize', {'protocolVersion':'2025-11-25','capabilities':{},
            'clientInfo':{'name':'first-use-transport-check','version':'1'}})
        self.proc.stdin.write('{"jsonrpc":"2.0","method":"notifications/initialized"}\n'); self.proc.stdin.flush()
    def rpc(self, method, params):
        self.serial += 1
        self.proc.stdin.write(json.dumps({'jsonrpc':'2.0','id':self.serial,'method':method,'params':params})+'\n')
        self.proc.stdin.flush(); end=time.monotonic()+90
        while time.monotonic()<end:
            answer=self.messages.get(timeout=max(1,end-time.monotonic()))
            assert not answer.get('ended') and not answer.get('invalid_json'), answer
            if answer.get('id') == self.serial:
                assert 'error' not in answer, answer
                return answer['result']
        raise TimeoutError(method)
    def tool(self, name, args):
        answer=self.rpc('tools/call',{'name':name,'arguments':args})
        assert not answer.get('isError'), answer
        proof['tool_calls'].append(name); save()
        return answer.get('structuredContent') or json.loads(answer['content'][0]['text'])
    def close(self):
        self.proc.stdin.close()
        try: self.proc.wait(timeout=10)
        except subprocess.TimeoutExpired: self.proc.terminate(); self.proc.wait(timeout=10)
        self.log.close()

try:
    entries={}
    for name,file in files.items():
        doc=json.loads(file.read_text(encoding='utf-8'))
        assert doc['firstUseFixture']=='keep this setting', name
        entry=doc['servers' if name=='vscode' else 'mcpServers']['ugt']
        assert Path(entry['command']).is_absolute() and entry['args']==['mcp'], entry
        entries[name]=entry
        proof['configured'].append(name); save()
    codex=json.loads(command(['codex','mcp','get','ugt','--json']))
    entry=codex.get('transport',codex)
    assert Path(entry['command']).is_absolute() and entry['args']==['mcp'], entry
    entries['codex']=entry
    assert 'ugt' in command(['claude','mcp','get','ugt'])
    proof['configured'] += ['codex','claude-code']; save()
    # Gemini validates the actual settings through its own MCP listing, without
    # generating a response or supplying an AI-provider key.
    assert 'ugt' in command(['gemini','mcp','list'])
    for name,entry in entries.items():
        m=MCP(entry)
        try:
            doctor=m.tool('ugt_doctor',{})
            assert doctor['ok'], (name,doctor)
            assert 'ugt_run' in {t['name'] for t in m.rpc('tools/list',{})['tools']}
        finally: m.close()
    # Use the configured server, then close the entire connection before
    # resuming the persisted job from a fresh process.
    from docx import Document
    folder=home/'UGT'; folder.mkdir(exist_ok=True)
    source=folder/'agent-first-use.docx'
    doc=Document(); doc.add_heading('First use',0); doc.add_paragraph('Keep 47 blue chairs at the café.'); doc.save(source)
    m=MCP(entries['codex'])
    try:
        answer=m.tool('ugt_run',{'project':'microsoft/markitdown','task':'markdown','inputs':[str(source)]})
        job=answer['job']; proof['job']=job; save()
        # Do not interrupt live work: let this connection finish its job, then
        # prove the status and delivered files survive a full MCP restart.
        end=time.monotonic()+300
        while answer['state'] in ('queued','running'):
            assert time.monotonic()<end, answer
            answer=m.tool('ugt_status',{'job':job,'wait_seconds':30})
        assert answer['state']=='succeeded', answer
    finally: m.close()
    m=MCP(entries['codex'])
    try:
        again=m.tool('ugt_status',{'job':job})
        assert again['state']=='succeeded' and again['result']['passed'], again
        text=(Path(again['result']['results'])/'document.md').read_text(encoding='utf-8')
        assert '47 blue chairs' in text and 'café' in text, text
        proof.update(delivered=text,reopened_same_job=True,passed=True); save()
    finally: m.close()
except BaseException as exc:
    proof['failure']=str(exc)[-2500:]; save(); raise
