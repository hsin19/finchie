"""OCR for text that a PDF only carries as images (HSBC transaction descriptions)."""

import threading
from dataclasses import dataclass
from typing import Any, Protocol

import numpy as np
from opentelemetry import trace

from finchie_parser.pdf import Document
from finchie_parser.text import clean


@dataclass(frozen=True)
class OcrLine:
    text: str
    top: float
    bottom: float
    left: float


class Ocr(Protocol):
    def read_column(self, document: Document, page_index: int, left: float, right: float) -> list[OcrLine]:
        """Text in the vertical strip `left`..`right` of a page, in PDF points; past the page's edge reads to it."""
        ...


_tracer = trace.get_tracer(__name__)


class RapidOcr:
    # Rendering at 3x keeps 7-8 pt statement text legible to the recognizer.
    SCALE = 3.0

    def __init__(self) -> None:
        self._engine: Any = None
        self._lock = threading.Lock()

    def read_column(self, document: Document, page_index: int, left: float, right: float) -> list[OcrLine]:
        # The slowest step of a parse, and one parse at a time: the span includes waiting for the lock.
        with _tracer.start_as_current_span("ocr", attributes={"finchie.ocr.page": page_index}):
            image = document.render(page_index, self.SCALE)
            # Cropping past the page pads it, which the recognizer reads as nothing: a strip ends at the edge.
            strip = image.crop((int(left * self.SCALE), 0, min(int(right * self.SCALE), image.width), image.height))
            # One engine shared across gRPC worker threads; it is not documented as thread-safe.
            with self._lock:
                result = self._get_engine()(np.asarray(strip))
        if result.boxes is None:
            return []
        lines = []
        for box, text in zip(result.boxes, result.txts, strict=True):
            xs, ys = box[:, 0], box[:, 1]
            lines.append(
                OcrLine(
                    text=clean(text),
                    top=float(ys.min()) / self.SCALE,
                    bottom=float(ys.max()) / self.SCALE,
                    left=left + float(xs.min()) / self.SCALE,
                )
            )
        return lines

    def _get_engine(self) -> Any:  # RapidOCR ships no type information
        if self._engine is None:
            from rapidocr import RapidOCR  # loads ONNX models; keep it off the import path

            self._engine = RapidOCR()
        return self._engine
