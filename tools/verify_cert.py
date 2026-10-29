#!/usr/bin/env python3
"""Independent verifier for colligo certificates.

Standard library plus the `blake3` package. It imports nothing from Colligo:
it recomputes size, hashes and the FASTQ record facts from the file itself
and compares them with the certificate. A disagreement is a bug in one of
the two implementations.

usage: verify_cert.py CERTIFICATE [FILE]
exit:  0 the certificate agrees, 1 it does not, 2 usage or unreadable input
"""
import gzip
import hashlib
import json
import os
import sys
import zlib

import blake3

SUFFIX = ".colligo-cert.json"
GROUP_SIZE = 8 << 20
MAX_LENGTH_BUCKETS = 4096
SCHEMA_VERSION = 1
GZIP_MAGIC = b"\x1f\x8b"


def fail_usage(message):
    print(f"verify_cert: {message}", file=sys.stderr)
    sys.exit(2)


def hash_file(path):
    md5, sha, b3 = hashlib.md5(), hashlib.sha256(), blake3.blake3()
    size = 0
    with open(path, "rb") as f:
        while chunk := f.read(1 << 20):
            size += len(chunk)
            md5.update(chunk)
            sha.update(chunk)
            b3.update(chunk)
    return size, md5.hexdigest(), sha.hexdigest(), b3.hexdigest()


def is_gzip(path):
    with open(path, "rb") as f:
        return f.read(2) == GZIP_MAGIC


def open_content(path):
    return gzip.open(path, "rb") if is_gzip(path) else open(path, "rb")


class Fastq:
    def __init__(self):
        self.records = 0
        self.bases = 0
        self.lengths = {}
        self.overflow = 0
        self.first = None
        self.last = None
        self.problem = None


def read_id(header):
    token = header[1:].split(None, 1)[0] if len(header) > 1 else b""
    token = token.rstrip(b"\r")
    if len(token) >= 2 and token[-2:-1] == b"/" and token[-1:] in (b"1", b"2"):
        token = token[:-2]
    return token.decode("utf-8", "replace")


def parse_fastq(path):
    out = Fastq()
    try:
        with open_content(path) as f:
            state, seq_len = 0, 0
            for raw in f:
                if not raw.endswith(b"\n"):
                    out.problem = "last line is incomplete"
                    return out
                line = raw[:-1]
                if line.endswith(b"\r"):
                    line = line[:-1]
                if state == 0:
                    if not line.startswith(b"@"):
                        out.problem = "record does not start with '@'"
                        return out
                    identifier = read_id(line)
                    if out.first is None:
                        out.first = identifier
                    out.last = identifier
                    state = 1
                elif state == 1:
                    seq_len = len(line)
                    state = 2
                elif state == 2:
                    if not line.startswith(b"+"):
                        out.problem = "separator line does not start with '+'"
                        return out
                    state = 3
                else:
                    if len(line) != seq_len:
                        out.problem = "quality length differs from sequence length"
                        return out
                    out.records += 1
                    out.bases += seq_len
                    key = str(seq_len)
                    if key in out.lengths or len(out.lengths) < MAX_LENGTH_BUCKETS:
                        out.lengths[key] = out.lengths.get(key, 0) + 1
                    else:
                        out.overflow += 1
                    state = 0
            if state != 0:
                out.problem = "line count is not a multiple of 4"
            elif out.records == 0:
                out.problem = "no records"
    except (EOFError, OSError, zlib.error) as exc:
        out.problem = f"container: {exc}"
    return out


def facts_of(cert, kind, prefix):
    return [f for f in cert.get("facts", []) if f["kind"] == kind and f["extractor_version"].startswith(prefix)]


def verify(cert, path):
    problems = []

    def expect(label, claimed, observed):
        if claimed != observed:
            problems.append(f"{label}: certificate says {claimed!r}, recomputed {observed!r}")

    expect("schema_version", cert.get("schema_version"), SCHEMA_VERSION)
    expect("group_size", cert.get("group_size"), GROUP_SIZE)
    size, md5, sha, b3 = hash_file(path)
    expect("size", cert.get("size"), size)
    expect("md5", cert.get("md5"), md5)
    expect("sha256", cert.get("sha256"), sha)
    expect("blake3_root", cert.get("blake3_root"), b3)

    recomputed = {md5, sha, b3}
    for a in cert.get("assertions", []):
        if a["id"] == "checksum_mismatch" and not a.get("skipped"):
            if a["observed"] not in recomputed:
                problems.append(f"assertion checksum_mismatch observed {a['observed']!r}, which is none of the recomputed hashes")
            if a["passed"] != (a["declared"] == a["observed"]):
                problems.append("assertion checksum_mismatch: result disagrees with declared and observed")

    fastq_structure = facts_of(cert, "structure", "fastq")
    if fastq_structure:
        parsed = parse_fastq(path)
        expect("fastq structure.valid", fastq_structure[0]["value"]["valid"], parsed.problem is None)
        if parsed.problem is None:
            for fact in facts_of(cert, "records", "fastq"):
                value = fact["value"]
                expect("records.records", value["records"], parsed.records)
                expect("records.bases", value["bases"], parsed.bases)
                expect("records.length_histogram", value["length_histogram"], parsed.lengths)
                expect("records.length_overflow", value["length_overflow"], parsed.overflow)
            for fact in facts_of(cert, "read_ends", "fastq"):
                expect("read_ends.first", fact["value"]["first"], parsed.first)
                expect("read_ends.last", fact["value"]["last"], parsed.last)

    if is_gzip(path):
        decompresses = decompresses_cleanly(path)
        for fact in facts_of(cert, "structure", "gzip"):
            expect("gzip structure.valid", fact["value"]["valid"], decompresses)
        for fact in facts_of(cert, "structure", "bgzf"):
            expect("bgzf structure.valid", fact["value"]["valid"], decompresses and has_bgzf_eof(path))
    return problems


def decompresses_cleanly(path):
    try:
        with gzip.open(path, "rb") as f:
            while f.read(1 << 20):
                pass
        return True
    except (EOFError, OSError, zlib.error):
        return False


BGZF_EOF = bytes.fromhex("1f8b08040000000000ff0600424302001b0003000000000000000000")


def has_bgzf_eof(path):
    with open(path, "rb") as f:
        f.seek(0, os.SEEK_END)
        if f.tell() < len(BGZF_EOF):
            return False
        f.seek(-len(BGZF_EOF), os.SEEK_END)
        return f.read() == BGZF_EOF


def main(argv):
    if len(argv) not in (2, 3):
        fail_usage(__doc__.strip().splitlines()[-3])
    cert_path = argv[1]
    if len(argv) == 3:
        path = argv[2]
    elif cert_path.endswith(SUFFIX):
        path = cert_path[: -len(SUFFIX)]
    else:
        fail_usage(f"cannot derive the data file from {cert_path!r}; pass it explicitly")
    try:
        with open(cert_path, "r", encoding="utf-8") as f:
            cert = json.load(f)
        if not os.path.isfile(path):
            fail_usage(f"{path!r} is not a file")
        problems = verify(cert, path)
    except (OSError, ValueError, KeyError, TypeError) as exc:
        fail_usage(f"unreadable input: {exc}")
    if problems:
        for p in problems:
            print(f"DISAGREE {p}")
        return 1
    print("AGREE")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
