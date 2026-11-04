from pathlib import Path
from docling.datamodel.base_models import InputFormat
from docling.datamodel.pipeline_options import PdfPipelineOptions
from docling.document_converter import DocumentConverter, PdfFormatOption

options = PdfPipelineOptions(artifacts_path=Path("/models"), do_ocr=False, do_table_structure=True)
converter = DocumentConverter(allowed_formats=[InputFormat.PDF],format_options={InputFormat.PDF:PdfFormatOption(pipeline_options=options)})
result = converter.convert("inputs/document.pdf")
Path("outputs/document.md").write_text(result.document.export_to_markdown(),encoding="utf-8")
