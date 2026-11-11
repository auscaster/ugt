"""Exercise the candidate's real local web UI on a disposable public Linux runner.

Two bounded admitted targets: single-file recovery/delivery and a nested-folder
MuJoCo scene. This is not desktop protocol, installer or cross-platform proof.
"""
import hashlib
import json
import os
import subprocess
import time
import urllib.request
import zipfile
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

if os.environ.get('GITHUB_REPOSITORY') != 'usegeekthing/ugt' or os.environ.get('GITHUB_ACTIONS') != 'true' or os.environ.get('RUNNER_ENVIRONMENT') != 'github-hosted' or os.environ.get('RUNNER_OS') != 'Linux':
    raise SystemExit('Run real workflow qualification only on the standard public hosted Linux runner.')

out = Path('evidence/web-runtime'); out.mkdir(parents=True, exist_ok=True)
exe = Path(os.environ['UGT_CI_EXE'])
opened = Path(os.environ['UGT_CAPTURE'])
proof = {'candidate': os.environ['GITHUB_SHA'], 'run': os.environ['GITHUB_RUN_ID'], 'cases': {}, 'kind': 'real candidate web workflow with admitted prebuilt images'}
processes = []

def save():
    (out / 'runtime-checks.json').write_text(json.dumps(proof, indent=2) + '\n')


def cli(*args):
    return subprocess.check_output([str(exe), *args], text=True)


def open_project(page, project):
    found = json.loads(cli('find', '--json', project))
    assert len(found['recipes']) == 1 and found['recipes'][0].get('images'), 'This bounded check requires the admitted prebuilt image'
    opened.unlink(missing_ok=True)
    process = subprocess.Popen([str(exe), 'open', 'ugt://run/' + project], stdout=subprocess.DEVNULL, stderr=open(out / (project.replace('/', '-') + '.log'), 'w'))
    processes.append(process)
    for _ in range(120):
        if opened.exists() and opened.read_text().strip():
            break
        time.sleep(.25)
    assert opened.exists(), 'The actual candidate did not open its page'
    page.goto(opened.read_text().strip())
    expect(page.locator('form')).to_be_visible()


def read_job(page):
    return page.evaluate("async () => {const id=new URLSearchParams(location.hash.slice(1)).get('job');return (await fetch('api/job?id='+encodeURIComponent(id))).json()}")


try:
    cli('reports', 'off')
    with sync_playwright() as pw:
        browser = pw.chromium.launch(args=['--no-sandbox', '--use-mock-keychain', '--password-store=basic'])
        context = browser.new_context(viewport={'width': 1440, 'height': 1000})
        page = context.new_page(); page.set_default_timeout(45000)
        open_project(page, 'microsoft/markitdown')
        bad = out / 'wrong.docx'; bad.write_bytes(b'not a document')
        page.locator('input[type=file]').set_input_files(bad)
        page.get_by_role('button', name='Run', exact=True).click()
        expect(page.get_by_role('heading', name='This run needs attention')).to_be_visible(timeout=300000)
        failed_id = page.url.split('#job=')[1]
        page.get_by_role('button', name='Change inputs and try again').click()
        assert page.locator('input[type=file]').evaluate('(node) => node.files[0].name') == 'wrong.docx'
        document = out / 'workshop.docx'
        with zipfile.ZipFile(document, 'w') as archive:
            archive.writestr('[Content_Types].xml', '<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>')
            archive.writestr('_rels/.rels', '<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>')
            archive.writestr('word/document.xml', '<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Workshop setup: take 23 blue chairs to Pier 8.</w:t></w:r></w:p></w:body></w:document>')
        page.locator('input[type=file]').set_input_files(document)
        page.get_by_role('button', name='Run', exact=True).click()
        expect(page.get_by_role('heading', name='Your file is ready', exact=True)).to_be_visible(timeout=240000)
        record = read_job(page)
        assert record['state'] == 'succeeded' and record['result']['passed'] and record['id'] != failed_id
        results = Path(record['result']['results'])
        text = (results / 'document.md').read_text()
        assert '23 blue chairs' in text and 'Pier 8' in text
        opened.unlink(missing_ok=True)
        page.get_by_role('button', name='Open the folder', exact=True).click()
        for _ in range(100):
            if opened.exists(): break
            time.sleep(.1)
        assert opened.read_text().strip() == str(results)
        page.reload(); expect(page.get_by_role('heading', name='Your file is ready', exact=True)).to_be_visible()
        page.screenshot(path=str(out / 'document-result.png'), full_page=True)
        proof['cases']['single_file'] = {'passed': True, 'failed_job': failed_id, 'actual_job': record, 'delivered_text': text, 'refresh_same_job': page.url.endswith(record['id']), 'reveal_matches_result_folder': True}
        save()

        scene = out / 'scene'; (scene / 'assets').mkdir(parents=True, exist_ok=True)
        (scene / 'model.xml').write_text('<mujoco model="folder upload"><option timestep="0.002" gravity="0 0 -9.81"/><include file="assets/objects.xml"/></mujoco>')
        (scene / 'assets/objects.xml').write_text('<mujocoinclude><worldbody><light pos="0 -2 4"/><camera name="film" pos="0 -3 1.5" xyaxes="1 0 0 0 .447 .894"/><geom type="plane" size="3 3 .1" rgba=".2 .2 .2 1"/><body pos="0 0 1"><freejoint/><geom type="sphere" size=".1" mass="1" rgba=".9 .2 .1 1"/></body></worldbody></mujocoinclude>')
        (scene / 'options.json').write_text(json.dumps({'duration': .6, 'fps': 10, 'width': 320, 'height': 240, 'camera': 'film'}))
        open_project(page, 'google-deepmind/mujoco')
        page.get_by_role('button', name='Choose a folder instead', exact=True).click()
        page.locator('input[webkitdirectory]').set_input_files(scene)
        page.screenshot(path=str(out / 'folder-ready.png'), full_page=True)
        page.get_by_role('button', name='Run', exact=True).click()
        expect(page.get_by_role('heading', name='Your files are ready', exact=True)).to_be_visible(timeout=300000)
        record = read_job(page)
        assert record['state'] == 'succeeded' and record['result']['passed']
        assert not any(step['name'] == 'build' for step in record['result']['steps'])
        result = Path(record['result']['results'])
        for name in ('simulation.mp4', 'trajectory.csv', 'frame.png', 'simulation.json'):
            assert (result / name).stat().st_size > 0, name
        probe = json.loads(subprocess.check_output(['ffprobe', '-v', 'error', '-show_entries', 'stream=width,height,nb_frames', '-of', 'json', str(result / 'simulation.mp4')]))
        assert probe['streams'][0]['width'] == 320 and probe['streams'][0]['height'] == 240 and probe['streams'][0]['nb_frames'] == '6'
        metadata = json.loads((result / 'simulation.json').read_text())
        page.screenshot(path=str(out / 'folder-result.png'), full_page=True)
        proof['cases']['directory'] = {'passed': True, 'actual_job': record, 'nested_input_sha256': {str(file.relative_to(scene)): hashlib.sha256(file.read_bytes()).hexdigest() for file in scene.rglob('*') if file.is_file()}, 'probe': probe, 'simulation': metadata, 'output_sha256': {name: hashlib.sha256((result / name).read_bytes()).hexdigest() for name in record['result']['files']}}
        proof['passed'] = True; save()
        context.close(); browser.close()
except BaseException as error:
    proof['failure'] = str(error)[-2000:]; save(); raise
finally:
    for process in processes:
        process.terminate()
    for process in processes:
        try: process.wait(timeout=10)
        except subprocess.TimeoutExpired: process.kill(); process.wait()
