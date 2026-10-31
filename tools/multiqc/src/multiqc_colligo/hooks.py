from multiqc import config

SEARCH_PATTERN = {"fn": "*.colligo.json"}


def register_search_pattern():
    config.sp.setdefault("colligo", dict(SEARCH_PATTERN))
