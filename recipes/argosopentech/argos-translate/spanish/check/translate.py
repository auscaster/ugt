from pathlib import Path
import stanza
from argostranslate.package import get_installed_packages
from argostranslate.translate import Language, PackageTranslation

source = Path("inputs/english.txt").read_text(encoding="utf-8-sig")
pkg = next(p for p in get_installed_packages() if (p.from_code, p.to_code) == ("en", "es"))
translation = PackageTranslation(Language(pkg.from_code, pkg.from_name), Language(pkg.to_code, pkg.to_name), pkg)
# The package contains its tokenizer and resources.json. Stanza's default
# nevertheless refreshes that index online whenever it creates a pipeline.
translation.sentencizer.stanza_pipeline = stanza.Pipeline(
    lang="en", dir=str(pkg.package_path / "stanza"), processors="tokenize",
    package="ewt", download_method=None, use_gpu=False, logging_level="WARNING",
)
Path("outputs/spanish.txt").write_text(translation.translate(source) + "\n", encoding="utf-8")
