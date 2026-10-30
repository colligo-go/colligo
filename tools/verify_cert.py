#!/usr/bin/env python3
"""Independent verifier for colligo certificates.

Standard library plus the `blake3` package. It imports nothing from Colligo:
it recomputes size, hashes and the FASTQ record facts from the file itself
and compares them with the certificate. A disagreement is a bug in one of
the two implementations.

usage: verify_cert.py CERTIFICATE [FILE]
exit:  0 the certificate agrees, 1 it does not, 2 it cannot be judged:
       usage, unreadable input, a schema this verifier does not know, or a
       certificate with a missing or unknown field
"""
import gzip
import hashlib
import json
import os
import re
import sys
import zlib

import blake3

SUFFIX = ".colligo-cert.json"
GROUP_SIZE = 8 << 20
MAX_LENGTH_BUCKETS = 4096
SCHEMA_VERSION = 1
GZIP_MAGIC = b"\x1f\x8b"

CERTIFICATE_FIELDS = {
    "schema_version": int,
    "tool": dict,
    "size": int,
    "blake3_root": str,
    "group_size": int,
    "md5": str,
    "sha256": str,
    "facts": list,
    "assertions": list,
}
TOOL_FIELDS = {"version": str, "git_sha": str}
FACT_FIELDS = {"kind": str, "extractor_version": str, "value": dict}
ASSERTION_REQUIRED = {"id": str, "mode": str, "severity": str, "passed": bool, "declared": str, "observed": str}
ASSERTION_OPTIONAL = {
    "skipped": bool,
    "skipped_by": str,
    "observation": str,
    "probable_cause": str,
    "suggested_action": str,
}


def fail_usage(message):
    print(f"verify_cert: {message}", file=sys.stderr)
    sys.exit(2)


def check_fields(where, obj, required, optional=None):
    if not isinstance(obj, dict):
        fail_usage(f"{where} is not an object")
    allowed = {**required, **(optional or {})}
    for name in required:
        if name not in obj:
            fail_usage(f"{where} is missing the required field {name!r}")
    for name, value in obj.items():
        if name not in allowed:
            fail_usage(f"{where} has the unknown field {name!r}")
        if type(value) is not allowed[name]:
            fail_usage(f"{where}.{name} has the wrong type {type(value).__name__}")


def check_schema(cert):
    check_fields("certificate", cert, CERTIFICATE_FIELDS)
    check_fields("certificate.tool", cert["tool"], TOOL_FIELDS)
    for i, fact in enumerate(cert["facts"]):
        check_fields(f"certificate.facts[{i}]", fact, FACT_FIELDS)
    for i, assertion in enumerate(cert["assertions"]):
        check_fields(f"certificate.assertions[{i}]", assertion, ASSERTION_REQUIRED, ASSERTION_OPTIONAL)


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
        self.blank = 0
        self.first = None
        self.last = None
        self.problem = None


FASTQ_DUMP_READ_ID = re.compile(rb"([SED]RR[0-9]+\.[0-9]+)\.[12]")


def read_id(header):
    token = header[1:].split(None, 1)[0] if len(header) > 1 else b""
    token = token.rstrip(b"\r")
    if len(token) >= 2 and token[-2:-1] == b"/" and token[-1:] in (b"1", b"2"):
        token = token[:-2]
    else:
        dump = FASTQ_DUMP_READ_ID.fullmatch(token)
        if dump:
            token = dump.group(1)
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
                    if line == b"":
                        out.blank += 1
                        continue
                    if out.blank:
                        out.problem = "blank line inside the file"
                        return out
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

    expect("group_size", cert.get("group_size"), GROUP_SIZE)
    size, md5, sha, b3 = hash_file(path)
    expect("size", cert.get("size"), size)
    expect("md5", cert.get("md5"), md5)
    expect("sha256", cert.get("sha256"), sha)
    expect("blake3_root", cert.get("blake3_root"), b3)

    for fact in facts_of(cert, "digests", "digests"):
        expect("digests.md5", fact["value"].get("md5"), md5)
        expect("digests.sha256", fact["value"].get("sha256"), sha)
        expect("digests.size", fact["value"].get("size"), size)

    recomputed = {md5, sha, b3}
    parsed = None
    verdicts = {}
    structure_formats = {f["extractor_version"].split("-")[0] for f in facts_of(cert, "structure", "")}

    fastq_structure = facts_of(cert, "structure", "fastq")
    if fastq_structure:
        parsed = parse_fastq(path)
        verdicts["fastq"] = parsed.problem is None
        expect("fastq structure.valid", fastq_structure[0]["value"]["valid"], parsed.problem is None)
        if parsed.problem is None:
            expect("fastq structure.trailing_blank_lines", fastq_structure[0]["value"].get("trailing_blank_lines", 0), parsed.blank)
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
        verdicts["gzip"] = decompresses
        for fact in facts_of(cert, "structure", "gzip"):
            expect("gzip structure.valid", fact["value"]["valid"], decompresses)
        for fact in facts_of(cert, "structure", "bgzf"):
            chained = decompresses and has_bgzf_chain(path)
            verdicts["bgzf"] = chained
            expect("bgzf structure.valid", fact["value"]["valid"], chained)

    for a in cert["assertions"]:
        if a.get("skipped"):
            continue
        problems.extend(check_assertion(a, size, recomputed, parsed, verdicts, structure_formats))
    return problems


def check_assertion(a, size, recomputed, parsed, verdicts, structure_formats):
    name = a["id"]
    problems = []
    if name in ("checksum_mismatch", "size_mismatch", "count_mismatch"):
        if a["passed"] != (a["declared"] == a["observed"]):
            problems.append(f"assertion {name}: result disagrees with declared and observed")
    if name == "checksum_mismatch" and a["observed"] not in recomputed:
        problems.append(f"assertion checksum_mismatch observed {a['observed']!r}, which is none of the recomputed hashes")
    if name == "size_mismatch" and a["observed"] != str(size):
        problems.append(f"assertion size_mismatch observed {a['observed']!r}, recomputed {str(size)!r}")
    if name == "count_mismatch" and parsed is not None and parsed.problem is None:
        counted = {str(parsed.records), str(parsed.bases)}
        if a["observed"] not in counted:
            problems.append(f"assertion count_mismatch observed {a['observed']!r}, recomputed {sorted(counted)!r}")
    if name == "structure_invalid":
        if a["passed"] != (a["observed"] == "valid"):
            problems.append("assertion structure_invalid: result disagrees with observed")
        recomputed_valid = all(verdicts.values())
        judged = bool(verdicts) and set(verdicts) >= structure_formats
        if a["passed"] and verdicts and not recomputed_valid:
            problems.append("assertion structure_invalid passed, but the structure recomputed as invalid")
        if not a["passed"] and judged and recomputed_valid:
            problems.append("assertion structure_invalid failed, but the structure recomputed as valid")
    return problems


RECALCULATED_FACTS = {
    "digests": ("digests",),
    "structure": ("fastq", "gzip", "bgzf"),
    "records": ("fastq",),
    "read_ends": ("fastq",),
}
RECALCULATED_ASSERTIONS = {"checksum_mismatch", "size_mismatch", "count_mismatch", "structure_invalid"}


def unchecked(cert):
    """Facts and assertions the verifier did not recompute, one line each."""
    lines = []
    for fact in cert["facts"]:
        prefixes = RECALCULATED_FACTS.get(fact["kind"], ())
        if not fact["extractor_version"].startswith(prefixes) if prefixes else True:
            lines.append(f"UNCHECKED fact {fact['kind']} ({fact['extractor_version']})")
    for a in cert["assertions"]:
        if not a.get("skipped") and a["id"] not in RECALCULATED_ASSERTIONS:
            lines.append(f"UNCHECKED assertion {a['id']}")
    return lines


def decompresses_cleanly(path):
    try:
        with gzip.open(path, "rb") as f:
            while f.read(1 << 20):
                pass
        return True
    except (EOFError, OSError, zlib.error):
        return False


BGZF_EOF = bytes.fromhex("1f8b08040000000000ff0600424302001b0003000000000000000000")
GZIP_FIXED_HEADER = 12
GZIP_TRAILER = 8


def bgzf_block_size(f, offset, file_size):
    """Total size of the BGZF block at offset, or None when no valid block starts there."""
    f.seek(offset)
    fixed = f.read(GZIP_FIXED_HEADER)
    if len(fixed) < GZIP_FIXED_HEADER or fixed[0] != 0x1F or fixed[1] != 0x8B or fixed[2] != 8 or not fixed[3] & 4:
        return None
    xlen = int.from_bytes(fixed[10:12], "little")
    extra = f.read(xlen)
    if len(extra) < xlen:
        return None
    i = 0
    while i + 4 <= xlen:
        slen = int.from_bytes(extra[i + 2 : i + 4], "little")
        if i + 4 + slen > xlen:
            return None
        if extra[i : i + 2] == b"BC" and slen == 2:
            size = int.from_bytes(extra[i + 4 : i + 6], "little") + 1
            minimum = GZIP_FIXED_HEADER + xlen + GZIP_TRAILER
            return size if minimum <= size and offset + size <= file_size else None
        i += 4 + slen
    return None


def has_bgzf_chain(path):
    """True when the BSIZE fields chain from offset 0 to exactly the end of
    the file and the last block is the 28-byte EOF marker."""
    file_size = os.path.getsize(path)
    offset, last = 0, None
    with open(path, "rb") as f:
        while offset < file_size:
            size = bgzf_block_size(f, offset, file_size)
            if size is None:
                return False
            offset, last = offset + size, offset
        if last is None or file_size - last != len(BGZF_EOF):
            return False
        f.seek(last)
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
        if cert.get("schema_version") != SCHEMA_VERSION:
            fail_usage(f"unknown schema_version {cert.get('schema_version')!r}; this verifier knows {SCHEMA_VERSION}")
        check_schema(cert)
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
    for line in unchecked(cert):
        print(line)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
