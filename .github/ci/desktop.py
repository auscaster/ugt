"""Exercise the shipped UGT desktop handler, file delivery and service lifecycle."""
import json, os, subprocess, time
from pathlib import Path
from urllib.parse import urlsplit
from playwright.sync_api import sync_playwright, expect
from docx import Document

if os.getenv('GITHUB_REPOSITORY') != 'auscaster/ugt' or os.getenv('GITHUB_ACTIONS') != 'true' or os.getenv('RUNNER_ENVIRONMENT') != 'github-hosted' or os.getenv('RUNNER_OS') != 'Linux':
    raise SystemExit("Only run this against a disposable hosted desktop.")
out=Path('evidence'); out.mkdir(exist_ok=True)
proof={"candidate":os.environ['GITHUB_SHA'],"run":os.environ['GITHUB_RUN_ID']}
def save():
    (out/'journey.json').write_text(json.dumps(proof,indent=2)+'\n')
save()
exe=Path.home()/'.local/bin/ugt'
def cli(*args):
    return subprocess.check_output([exe,*args],text=True)
cli('reports','off')
opened=Path(os.environ['UGT_CAPTURE'])
def launch(page, repo):
    opened.unlink(missing_ok=True)
    # The public link that the catalogue offers for one checked task.
    import urllib.request
    data = json.load(urllib.request.urlopen('https://api.usegithubthing.com/v1/projects/'+repo, timeout=15))
    assert data['project'].lower() == repo.lower() and len(data['recipes']) == 1
    recipe = data['recipes'][0]['recipe']
    assert recipe.lower().startswith(repo.lower()+'/')
    uri='ugt://run/'+recipe
    subprocess.run(['gio','open',uri],check=True)
    for _ in range(160):
        if opened.exists() and opened.read_text().strip():break
        time.sleep(.25)
    assert opened.exists(),'The registered desktop handler did not open the UGT page'
    page.goto(opened.read_text().strip())
    return uri
try:
  with sync_playwright() as pw:
    browser=pw.chromium.launch(args=['--no-sandbox'])
    p=browser.new_page();p.set_default_timeout(45000)
    proof['handler']=subprocess.check_output(['xdg-mime','query','default','x-scheme-handler/ugt'],text=True).strip()
    assert proof['handler']=='ugt-link.desktop',proof['handler']
    proof['document_link']=launch(p,'microsoft/markitdown'); save()
    file=p.locator('input[type=file]').first
    expect(file).to_be_visible()
    assert not p.get_by_role('button',name='Or type it instead').count()
    p.get_by_text('Need to finish setting up UGT?',exact=True).click()
    p.get_by_role('button',name='Continue with setup').click()
    expect(p.get_by_role('button',name='Continue with my project')).to_be_visible(timeout=90000)
    p.get_by_role('button',name='Continue with my project').click()
    proof['setup']='finished from project page'; save()
    bad=out/'wrong.docx';bad.write_bytes(b'bad input')
    file.set_input_files(bad);p.get_by_role('button',name='Run',exact=True).click()
    expect(p.get_by_role('heading',name='It did not finish')).to_be_visible(timeout=360000)
    from urllib.parse import parse_qs, urlsplit
    help_link=urlsplit(p.get_by_role('link',name='Get help with this run').get_attribute('href'))
    assert help_link.hostname == 'usegithubthing.com' and parse_qs(help_link.fragment)['run'] == [p.url.split('#job=')[1]]
    p.get_by_role('button',name='Change inputs and try again').click()
    doc=Document();doc.add_heading('Harbour booking',0);doc.add_paragraph('Take 23 blue chairs to Pier 8.');doc.save(out/'my booking.docx')
    file.set_input_files(out/'my booking.docx');p.get_by_role('button',name='Run',exact=True).click()
    p.wait_for_url('**/#job=*'); job=p.url.split('#job=')[1]
    p.reload();expect(p.get_by_role('heading',name='Your file is ready')).to_be_visible(timeout=240000)
    assert p.url.endswith(job)
    opened.unlink(missing_ok=True);p.get_by_role('button',name='Open the folder').click()
    for _ in range(100):
        if opened.exists() and opened.read_text().strip():break
        time.sleep(.1)
    markdown=(Path(opened.read_text().strip())/'document.md').read_text()
    assert 'Harbour booking' in markdown and '23 blue chairs to Pier 8' in markdown
    proof.update(retry="invalid DOCX refused; replaced from same page",refresh_job=job,delivered=markdown);save()
    proof['service_link']=launch(p,'meilisearch/meilisearch');p.get_by_text('Run a separate copy',exact=True).click();p.get_by_label('Copy name').fill('workshop')
    p.get_by_role('button',name='Start the app').click();expect(p.get_by_role('heading',name='Your app started')).to_be_visible(timeout=300000)
    import urllib.request
    address=p.locator('section').filter(has=p.get_by_role('heading',name='For other software')).locator('li').first.inner_text().split('\n')[0].strip().rstrip('/')
    def http(path,data=None):
        req=urllib.request.Request(address+path,data=json.dumps(data).encode() if data is not None else None,headers={'Content-Type':'application/json'})
        return json.load(urllib.request.urlopen(req,timeout=10))
    task=http('/indexes/booking/documents',[{'id':1,'note':'23 blue chairs'}])
    for _ in range(50):
        if http('/tasks/'+str(task['taskUid']))['status']=='succeeded':break
        time.sleep(.2)
    assert http('/indexes/booking/documents/1')['note']=='23 blue chairs'
    p.reload();p.get_by_role('button',name='Stop this copy').click();expect(p.get_by_role('heading',name='You stopped this copy')).to_be_visible()
    try: http('/health')
    except OSError: pass
    else: raise AssertionError('service was still answering')
    p.reload();expect(p.get_by_role('button',name='Start the app')).to_be_visible()
    assert p.get_by_label('Copy name').input_value()=='workshop'
    p.get_by_role('button',name='Start the app').click();expect(p.get_by_role('heading',name='Your app started')).to_be_visible(timeout=120000)
    assert http('/indexes/booking/documents/1')['note']=='23 blue chairs'
    proof.update(restart='same address, same booking after stop and restart',passed=True);save(); browser.close()
except BaseException as exc:
    proof['failure']=str(exc)[-1500:];save();raise
