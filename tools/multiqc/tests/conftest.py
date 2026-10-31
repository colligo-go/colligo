import copy
import json
from pathlib import Path

import pytest

EXAMPLES = Path(__file__).parent / "examples"


def load(name):
    return json.loads((EXAMPLES / f"{name}.colligo.json").read_text())


@pytest.fixture
def example():
    return lambda name: copy.deepcopy(load(name))
