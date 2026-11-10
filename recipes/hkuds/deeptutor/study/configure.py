"""Seed DeepTutor's own first-install profile; its UI owns later settings."""
import json
from pathlib import Path

settings = Path("/app/data/user/settings")
settings.mkdir(parents=True, exist_ok=True)
catalog = settings / "model_catalog.json"
if not catalog.exists():
    catalog.write_text(json.dumps({"version": 1, "services": {"llm": {
        "active_profile_id": "local-ollama", "active_model_id": "qwen-local",
        "profiles": [{"id": "local-ollama", "name": "Ollama on this computer", "binding": "openai",
                      "base_url": "http://ollama:11434/v1", "api_key": "ollama",
                      "api_version": "", "extra_headers": {},
                      "models": [{"id": "qwen-local", "name": "Qwen 2.5 1.5B (local CPU)", "model": "qwen2.5:1.5b"}]}]
    }}}, indent=2) + "\n")
