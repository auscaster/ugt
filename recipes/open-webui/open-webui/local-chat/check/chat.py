import json
import re
import sys
import time
import urllib.error
import urllib.request

def request(url, data=None, token=None, timeout=15):
    headers = {"Content-Type":"application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(url, headers=headers, data=None if data is None else json.dumps(data).encode())
    with urllib.request.urlopen(req, timeout=timeout) as res:
        return json.load(res)

url = "http://webui:8080"
deadline = time.monotonic()+150
while True:
    try:
        request(url+"/health", timeout=5)
        break
    except (urllib.error.URLError,TimeoutError) as exc:
        if time.monotonic() >= deadline or "Name or service not known" in str(exc):
            raise
        time.sleep(2)

model = "qwen2.5:1.5b"
expected = "65ec06548149b04c096a120e4a6da9d4017ea809c91734ea5631e89f96ddc57b"
if not any(m["name"]==model and m["digest"]==expected for m in request("http://ollama:11434/api/tags")["models"]):
    raise SystemExit("Ollama does not have the tested model revision")
token = request(url+"/api/v1/auths/signin", {"email":"admin@localhost","password":"admin"})["token"]
models = request(url+"/api/models",token=token)["data"]
if model not in [entry["id"] for entry in models]:
    raise SystemExit("The model is not available in the chat interface")
if "sample" in sys.argv:
    data = {"model":model, "messages":[{"role":"user","content":"What is six plus seven? Reply with just the number."}],
            "temperature":0, "stream":False, "max_tokens":16}
    reply = request(url+"/api/chat/completions",data,token,timeout=180)["choices"][0]["message"]["content"]
    if not re.fullmatch(r"\s*13[.!]?\s*",reply):
        raise SystemExit(f"The chat returned {reply!r} instead of 13")
