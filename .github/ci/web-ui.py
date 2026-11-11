"""Browser component checks for the real local UI, with explicit synthetic API fixtures.

--serve is a labelled development preview. No mode runs a project or reads credentials.
"""
import argparse
import json
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[2]
STATIC = ROOT / 'internal/surface/web/static'
JOB = 'job_' + '0' * 25 + '1'
RECIPE = 'example/tool/process'
STATES = {}
PREVIEW = False


def state(name):
    return STATES.setdefault(name, {'posts': 0, 'status': None, 'phase': 'task', 'delay': 0, 'job_delay': 0, 'started': time.time()})


def plan(name):
    recipe = {'recipe': RECIPE, 'what': 'Turn a supplied document into useful files', 'kind': 'job', 'inputs': [{'path': 'inputs/document.txt', 'what': 'the document you want to use'}], 'outputs': [{'path': 'outputs/result.md', 'what': 'an editable Markdown document'}], 'known_issues': ['The result keeps text, not the original page layout.'], 'results': '/home/example/UGT/example/tool/process'}
    if name == 'folder':
        recipe['inputs'] = [{'path': 'inputs/scene', 'what': 'a scene folder with its referenced assets'}]
    if name in ('app', 'tools'):
        recipe['inputs'] = []
        recipe['outputs'] = []
        recipe['kind'] = 'service' if name == 'app' else 'job'
        recipe['provides'] = {'pages': [{'what': 'Your notes', 'service': 'app', 'port': 8080}]} if name == 'app' else {'commands': [{'name': 'example-tool', 'what': 'Use the project from a terminal'}], 'mcp_servers': [{'what': 'Project tools for your AI app'}]}
    if name == 'long':
        recipe['what'] = 'Process a document with a deliberately long task description that must remain readable on a small screen'
        recipe['settings'] = [{'kind': 'value', 'name': 'LANGUAGE', 'what': 'Language', 'required': True}, {'kind': 'value', 'name': 'FORMAT', 'what': 'Output format', 'how': 'Optional. Keep the project default.'}]
        recipe['outputs'] = [{'path': 'outputs/' + 'long-result-name-' * 8 + '.md', 'what': 'A detailed output description that should wrap comfortably without touching the panel edge or making the page scroll sideways'}]
    return {'folder': '/home/example/UGT', 'link': 'ugt://run/' + RECIPE, 'recipes': [recipe], 'last_job': JOB if state(name).get('resume') else None, 'windows_setup': name == 'restart'}


def job(name):
    value = state(name)
    status = value['status'] or 'running'
    phase = value['phase']
    if PREVIEW and value['posts']:
        age = time.time() - value['started']
        phase = 'task' if age < 5 else 'check'
        status = 'running' if age < 9 else 'succeeded'
    events = [] if phase == 'prepare' else [{'step': 'task', 'message': 'started'}]
    if phase in ('check', 'deliver'):
        events += [{'step': 'task', 'message': 'exit 0'}, {'step': 'check', 'message': 'started'}]
    if phase == 'deliver':
        events += [{'step': 'check', 'message': 'exit 0'}]
    result = {'passed': value.get('passed', True), 'kind': 'job', 'recipe': RECIPE, 'seconds': 12.3, 'results': '/home/example/UGT/example/tool/process', 'files': ['result.md', 'data/table.csv']}
    if name == 'many':
        result['files'] = [f'result-{i}.md' for i in range(12)]
    if name == 'app':
        result = {'passed': True, 'kind': 'service', 'seconds': 3.2, 'installation': RECIPE + '@test', 'pages': [{'url': 'http://127.0.0.1:8080', 'what': 'Your notes'}]}
    if name == 'tools':
        result = {'passed': True, 'kind': 'job', 'seconds': 2.1, 'commands': [{'type': 'example-tool --input "my file.txt"', 'what': 'Use the project from a terminal', 'note': 'Reads files from the current folder.'}], 'mcp_servers': [{'what': 'Project tools for your AI app', 'note': 'Available through your existing UGT connection.'}]}
    record = {'id': JOB, 'kind': 'setup' if name == 'restart' else 'run', 'recipe': RECIPE, 'state': status, 'started': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime(value['started'])), 'events': events, 'result': result}
    if name == 'restart':
        record['result'] = {'needs_restart': True}
    if status == 'failed':
        record['error'] = 'This document could not be read. Choose a supported document.'
    return record


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def reply(self, value, status=200):
        data = json.dumps(value).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        path = urlsplit(self.path).path
        parts = path.strip('/').split('/')
        name = parts[0] or 'files'
        if path.endswith('/api/plan'):
            if name == 'loading':
                time.sleep(.8)
            return self.reply(plan(name))
        if '/api/job' in path:
            time.sleep(state(name)['job_delay'])
            return self.reply(job(name))
        if path.endswith('/api/alive'):
            self.send_response(204); self.end_headers(); return
        filename = parts[-1] if '.' in parts[-1] else 'index.html'
        if filename not in ('index.html', 'page.js', 'page.css', 'archivo-latin.woff2', 'ugt-wordmark-black.svg', 'ugt-wordmark-white.svg'):
            self.send_error(404); return
        data = (STATIC / filename).read_bytes()
        if PREVIEW and filename == 'index.html':
            data = data.replace(b'On this computer</span>', b'UI preview: simulated job</span>')
        content_type = {'html': 'text/html', 'js': 'text/javascript', 'css': 'text/css', 'svg': 'image/svg+xml', 'woff2': 'font/woff2'}[filename.split('.')[-1]]
        self.send_response(200); self.send_header('Content-Type', content_type); self.send_header('Content-Length', str(len(data))); self.end_headers(); self.wfile.write(data)

    def do_POST(self):
        parts = urlsplit(self.path).path.strip('/').split('/')
        name = parts[0]
        data = self.rfile.read(int(self.headers.get('Content-Length', 0)))
        if parts[-1] in ('run', 'setup'):
            value = state(name); value['posts'] += 1; value['body'] = data; value['started'] = time.time()
            time.sleep(value['delay'])
            if name == 'reject':
                return self.reply({'error': 'This input was not accepted.'}, 400)
            if name == 'uncertain':
                return self.reply({'accepted_without_reference': True})
            return self.reply({'job': JOB})
        return self.reply({}, 204)


def run_checks(address, output):
    from playwright.sync_api import sync_playwright, expect
    output.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as pw:
        browser = pw.chromium.launch(args=['--use-mock-keychain', '--password-store=basic'])
        context = browser.new_context(viewport={'width': 1440, 'height': 1000})
        page = context.new_page()
        errors = []
        page.on('pageerror', lambda error: errors.append(str(error)))
        def open_case(name):
            page.goto(address + name + '/')
            expect(page.locator('form')).to_be_visible()
        def pick():
            page.locator('input[type=file]').set_input_files({'name': 'notes.txt', 'mimeType': 'text/plain', 'buffer': b'The actual supplied file'})
        def post():
            page.get_by_role('button', name='Run', exact=True).click()

        page.goto(address + 'loading/', wait_until='domcontentloaded')
        expect(page.get_by_role('heading', name='Opening your project')).to_be_visible()
        expect(page.get_by_role('heading', name='What it needs')).to_be_visible()

        open_case('files'); pick()
        state('files').update(delay=1.3, job_delay=.8, phase='prepare')
        post()
        expect(page.get_by_role('status')).to_have_text('Starting your run', timeout=600)
        expect(page.get_by_role('button', name='Run', exact=True)).to_have_count(0)
        expect(page.get_by_role('status')).to_have_text('Getting the project ready', timeout=3500)
        expect(page.locator('.elapsed')).to_contain_text('elapsed')
        assert state('files')['posts'] == 1
        state('files').update(job_delay=0, phase='task')
        expect(page.get_by_role('status')).to_have_text('Running tool')
        page.get_by_text('Show run details', exact=True).click()
        panel = page.locator('.run-panel').element_handle()
        focused = page.locator('.activity-details > summary').element_handle()
        focused.focus()
        state('files')['phase'] = 'check'
        expect(page.get_by_role('status')).to_have_text('Checking the result')
        assert panel.evaluate('(node) => node === document.querySelector(".run-panel")')
        assert focused.evaluate('(node) => node === document.activeElement')
        assert page.locator('.activity-details').evaluate('(node) => node.open')
        assert page.locator('.elapsed').get_attribute('aria-live') == 'off'
        state('files')['phase'] = 'deliver'
        expect(page.get_by_role('status')).to_have_text('Getting your result ready')
        state('files')['status'] = 'succeeded'
        expect(page.get_by_role('heading', name='Your files are ready', exact=True)).to_be_visible()
        assert page.locator('.files li').count() == 2
        expect(page.get_by_text('data/table.csv', exact=True)).to_be_visible()
        expect(page.locator('.files .why')).to_have_text('An editable Markdown document')
        assert page.get_by_role('button', name='Open the folder', exact=True).bounding_box()['y'] < page.locator('.files li').first.bounding_box()['y']
        page.screenshot(path=str(output / 'files-light.png'), full_page=True)
        page.get_by_role('button', name='Run again', exact=True).click()
        expect(page.locator('form')).to_be_visible()
        assert page.locator('input[type=file]').evaluate('(node) => node.files[0].name') == 'notes.txt'
        assert state('files')['posts'] == 1

        open_case('reject'); pick(); post()
        expect(page.get_by_role('alert')).to_contain_text('This input was not accepted')
        assert page.locator('input[type=file]').evaluate('(node) => node.files[0].name') == 'notes.txt'
        expect(page.get_by_role('button', name='Run', exact=True)).to_be_enabled()

        open_case('uncertain'); pick(); post()
        expect(page.get_by_role('heading', name='Let’s check whether it started')).to_be_visible()
        expect(page.get_by_role('button', name='Run', exact=True)).to_have_count(0)
        assert state('uncertain')['posts'] == 1

        open_case('failed'); pick(); state('failed')['status'] = 'failed'; post()
        expect(page.get_by_role('heading', name='This run needs attention')).to_be_visible()
        page.screenshot(path=str(output / 'failed.png'), full_page=True)
        page.get_by_role('button', name='Change inputs and try again').click()
        assert page.locator('input[type=file]').evaluate('(node) => node.files[0].name') == 'notes.txt'
        assert state('failed')['posts'] == 1

        open_case('unpassed'); pick(); state('unpassed').update(status='succeeded', passed=False); post()
        expect(page.get_by_role('heading', name='This run needs attention')).to_be_visible()
        expect(page.get_by_role('heading', name='Your files are ready')).to_have_count(0)

        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp) / 'scene'; (directory / 'assets/nested').mkdir(parents=True)
            (directory / 'model.xml').write_text('<mujoco/>')
            (directory / 'assets/nested/mesh.obj').write_text('a mesh')
            open_case('folder')
            page.get_by_role('button', name='Choose a folder instead').click()
            page.locator('input[webkitdirectory]').set_input_files(directory)
            state('folder')['status'] = 'succeeded'; post()
            expect(page.get_by_role('heading', name='Your files are ready')).to_be_visible()
            assert b'filename="assets/nested/mesh.obj"' in state('folder')['body']
            assert b'name="directory:inputs/scene"' in state('folder')['body']

        open_case('text')
        page.get_by_role('button', name='Type it instead').click()
        page.get_by_role('textbox').fill('My typed document')
        state('text')['status'] = 'succeeded'; post()
        expect(page.get_by_role('heading', name='Your files are ready')).to_be_visible()
        assert b'My typed document' in state('text')['body']

        open_case('app')
        page.get_by_text('Run a separate copy', exact=True).click(); page.get_by_label('Copy name').fill('test')
        state('app')['status'] = 'succeeded'
        page.get_by_role('button', name='Start the app', exact=True).click()
        expect(page.get_by_role('heading', name='Your app is ready')).to_be_visible()
        expect(page.get_by_role('link', name='Your notes', exact=True)).to_be_visible()
        page.get_by_role('button', name='Stop this copy').click()
        expect(page.get_by_role('heading', name='You stopped this copy')).to_be_visible()
        page.get_by_role('button', name='Back to the app').click()
        expect(page.get_by_label('Copy name')).to_have_value('test')
        assert state('app')['posts'] == 1

        open_case('many'); pick(); state('many')['status'] = 'succeeded'; post()
        expect(page.get_by_role('heading', name='Your files are ready')).to_be_visible()
        assert page.locator('.files li').count() == 6
        page.get_by_text('Show 6 more files', exact=True).click()
        expect(page.locator('.files li')).to_have_count(12)

        open_case('tools'); state('tools')['status'] = 'succeeded'; post()
        expect(page.get_by_role('heading', name='Ready to use', exact=True)).to_be_visible()
        expect(page.get_by_text('example-tool --input "my file.txt"', exact=True)).to_be_visible()
        expect(page.get_by_text('Project tools for your AI app', exact=True)).to_be_visible()

        state('restart').update(resume=True, status='succeeded')
        page.goto(address + 'restart/')
        expect(page.get_by_role('heading', name='Restart Windows to continue')).to_be_visible()
        assert state('restart')['posts'] == 0

        for theme in ('light', 'dark'):
            page.emulate_media(color_scheme=theme, reduced_motion='reduce')
            for width in (320, 768, 1440):
                page.set_viewport_size({'width': width, 'height': 1000})
                open_case('long')
                page.get_by_text('Change the defaults', exact=True).click()
                assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), (theme, width)
                expect(page.get_by_role('button', name='Run', exact=True)).to_be_visible()
                assert page.get_by_role('button', name='Run', exact=True).bounding_box()['height'] >= 44
                page.screenshot(path=str(output / f'form-{theme}-{width}.png'), full_page=True)
        page.set_viewport_size({'width': 1440, 'height': 1000})
        page.emulate_media(color_scheme='dark', reduced_motion='reduce')
        page.goto(address + 'files/'); expect(page.get_by_role('heading', name='What it needs')).to_be_visible()
        pick(); post(); expect(page.get_by_role('heading', name='Your files are ready')).to_be_visible()
        page.screenshot(path=str(output / 'files-dark.png'), full_page=True)
        assert not errors, errors
        context.close(); browser.close()
    (output / 'ui-checks.json').write_text(json.dumps({'passed': True, 'kind': 'browser component checks with synthetic API fixtures', 'immediate_start': True, 'stable_polling_dom_and_focus': True, 'directory_relative_paths': True, 'unknown_submission_not_retried': True, 'retry_preserves_input': True, 'service_named_copy': True, 'provided_commands_and_mcp': True, 'viewports': [320, 768, 1440], 'themes': ['light', 'dark'], 'reduced_motion': True, 'page_errors': errors}, indent=2) + '\n')


def main():
    global PREVIEW
    parser = argparse.ArgumentParser(); parser.add_argument('--serve', action='store_true'); parser.add_argument('--out', default='evidence/web-ui')
    args = parser.parse_args(); PREVIEW = args.serve
    server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    address = f'http://127.0.0.1:{server.server_port}/'
    if args.serve:
        print(address + 'files/', flush=True); server.serve_forever(); return
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try: run_checks(address, Path(args.out))
    finally: server.shutdown(); server.server_close()

if __name__ == '__main__':
    main()
