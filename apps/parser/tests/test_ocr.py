from types import SimpleNamespace

import numpy as np
import pytest
from PIL import Image

from finchie_parser.ocr import RapidOcr


class RenderedPage:
    """A page RapidOcr renders: 300 x 400 pixels at its scale."""

    def render(self, page_index: int, scale: float) -> Image.Image:
        return Image.new("RGB", (300, 400))


def test_a_strip_past_the_pages_edge_ends_at_it(monkeypatch: pytest.MonkeyPatch) -> None:
    shapes: list[tuple[int, ...]] = []

    def engine(image: np.ndarray) -> SimpleNamespace:
        shapes.append(image.shape)
        return SimpleNamespace(boxes=None)

    ocr = RapidOcr()
    monkeypatch.setattr(ocr, "_get_engine", lambda: engine)
    # Cropped past the page, the strip would be padded, and the recognizer reads padding as nothing.
    # From 50 pt (150 px at scale 3) to far past the 300 px page: 150 px wide.
    assert ocr.read_column(RenderedPage(), 0, 50, 10_000) == []  # type: ignore[arg-type]
    assert shapes == [(400, 150, 3)]
