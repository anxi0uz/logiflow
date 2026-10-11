import os
from pathlib import Path

import pytest


@pytest.fixture
def font_path() -> str:
    candidates = [
        Path(os.getenv("FONT_PATH", "/nonexistent-font")),
        Path("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"),
        Path("/usr/share/fonts/TTF/DejaVuSans.ttf"),
    ]
    for path in candidates:
        if path.is_file():
            return str(path)
    pytest.fail("Install DejaVu Sans or set FONT_PATH to its TTF file")
