import re
from pathlib import Path

import pytest

from conftest import EXAMPLES

GOLDEN = Path(__file__).resolve().parents[3] / "cmd" / "colligo" / "testdata" / "golden" / "json"
FROM_GOLDEN = ["verified", "blocking", "advisory", "source-verified", "source-link", "not-declared", "unreadable"]


@pytest.mark.parametrize("name", FROM_GOLDEN)
def test_example_is_the_current_golden_of_colligo(name):
    golden = GOLDEN / f"{name}.json"
    if not golden.exists():
        pytest.skip("the Go golden files are not part of this checkout")
    assert (EXAMPLES / f"{name}.colligo.json").read_bytes() == golden.read_bytes()


def test_examples_hold_no_machine_path():
    for path in EXAMPLES.glob("*.json"):
        body = path.read_text()
        assert not re.search(r'"(?:/home|/tmp|/Users|/mnt|C:\\)', body), path.name
