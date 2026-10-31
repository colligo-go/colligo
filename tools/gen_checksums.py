#!/usr/bin/env python3
"""Builds identity/checksums.json from the checksum files that official
sources publish.

Usage: python3 -I tools/gen_checksums.py tools/checksums/sources.tsv

Each line of the sources file has four tab-separated fields: the name of the
release, the address the checksum file was downloaded from, the day it was
downloaded (YYYY-MM-DD) and the path of the downloaded copy, relative to the
sources file. The copies are kept in the repository, so the table can be
rebuilt without the network and the result checked.

The output is deterministic: entries are sorted, keys are sorted, and the
order of the lines of the sources file does not matter.
"""

import datetime
import json
import os
import re
import sys

KEPT_SUFFIXES = (".gtf.gz", ".gff3.gz", ".fa.gz")
CHECKSUM_LINE = re.compile(r"^([0-9a-f]{32})  (\S+)$")


def read_sources(path):
    base = os.path.dirname(os.path.abspath(path))
    sources = []
    with open(path, encoding="utf-8") as f:
        for number, line in enumerate(f, 1):
            line = line.rstrip("\n")
            if not line:
                continue
            fields = line.split("\t")
            if len(fields) != 4:
                sys.exit(f"{path}:{number}: want 4 tab-separated fields, got {len(fields)}")
            name, origin, retrieved, copy = fields
            if not origin.startswith("https://"):
                sys.exit(f"{path}:{number}: origin is not an https address")
            try:
                datetime.date.fromisoformat(retrieved)
            except ValueError:
                sys.exit(f"{path}:{number}: retrieved is not a date (YYYY-MM-DD)")
            sources.append((name, origin, retrieved, os.path.join(base, copy)))
    return sources


def entries_of(source):
    name, origin, retrieved, copy = source
    out = []
    with open(copy, encoding="utf-8") as f:
        for number, line in enumerate(f, 1):
            line = line.rstrip("\n")
            if not line:
                continue
            match = CHECKSUM_LINE.match(line)
            if not match:
                sys.exit(f"{copy}:{number}: not a line of an md5sum file")
            digest, file = match.groups()
            if file.endswith(KEPT_SUFFIXES):
                out.append(
                    {"name": name, "file": file, "md5": digest, "origin": origin, "retrieved": retrieved}
                )
    return out


def main(argv):
    if len(argv) != 2:
        sys.exit(__doc__)
    entries = []
    for source in read_sources(argv[1]):
        entries.extend(entries_of(source))
    seen = {}
    for entry in entries:
        other = seen.setdefault(entry["md5"], entry)
        if other is not entry:
            sys.exit(f"md5 {entry['md5']} is published for {other['file']} and for {entry['file']}")
    entries.sort(key=lambda e: (e["name"], e["file"]))
    sys.stdout.write(json.dumps(entries, indent=2, sort_keys=True, ensure_ascii=False) + "\n")


main(sys.argv)
