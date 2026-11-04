import time
import urllib.error
import urllib.request
def check():
    deadline=time.monotonic()+150
    while True:
        try:
            with urllib.request.urlopen("http://archive:8000/",timeout=4) as page:
                if b"paperless" in page.read(100000).lower(): return
        except (urllib.error.URLError,TimeoutError) as exc:
            if time.monotonic()>=deadline or "Name or service not known" in str(exc): raise
        if time.monotonic()>=deadline: raise SystemExit("The archive's sign-in page did not open")
        time.sleep(2)
if __name__=="__main__": check()
