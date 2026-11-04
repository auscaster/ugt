"""The scanner inbox must reach a searchable document in the archive."""
from pathlib import Path
import time
from uuid import uuid4
from page import check
check()
import django
django.setup()
from documents.models import Document

name="Harbour invoice "+uuid4().hex[:12]
text="The harbour invoice includes sixty brass lanterns."
inbox=Path("/usr/src/paperless/consume")
tmp=inbox/(name+".part")
tmp.write_text(text)
tmp.rename(inbox/(name+".txt"))
deadline=time.monotonic()+120
while time.monotonic()<deadline:
    doc=Document.objects.filter(title=name).first()
    if doc is not None and "sixty brass lanterns" in doc.content.casefold():
        break
    time.sleep(2)
else:
    raise SystemExit("The scanner inbox did not produce a searchable document")
