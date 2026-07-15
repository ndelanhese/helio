#!/usr/bin/env bash
set -euo pipefail

results="${1:-web/test-results}"

emit() {
  printf 'has_artifacts=%s\n' "$1"
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    printf 'has_artifacts=%s\n' "$1" >> "$GITHUB_OUTPUT"
  fi
}

if [[ ! -e "$results" ]]; then
  emit false
  exit 0
fi

trace_count="$(python3 - "$results" <<'PY'
import ipaddress
import os
from pathlib import Path, PurePosixPath
import re
import stat
import sys
import zipfile

root = Path(sys.argv[1])
MAX_ARCHIVES = 50
MAX_ARCHIVE_BYTES = 25 * 1024 * 1024
MAX_TOTAL_ARCHIVE_BYTES = 100 * 1024 * 1024
MAX_MEMBERS = 5000
MAX_MEMBER_BYTES = 25 * 1024 * 1024
MAX_EXPANDED_BYTES = 100 * 1024 * 1024

CORE_MEMBER = re.compile(r"^(?:trace|[0-9]+-trace)\.(?:trace|network)$|^(?:[0-9]+-)?stack\.stacks$")
RESOURCE_MEMBER = re.compile(r"^resources/[A-Za-z0-9][A-Za-z0-9._@-]{0,255}$")
FORBIDDEN_NAME = re.compile(r"(?:^|[/._-])(?:env|auth|cookie|credential|database|session|storage[-_]?state)(?:[/._-]|$)", re.I)
FORBIDDEN_SUFFIXES = (
    ".zip", ".tar", ".tgz", ".gz", ".bz2", ".xz", ".7z", ".rar",
    ".db", ".sqlite", ".sqlite3", ".pcap", ".pcapng", ".har", ".raw", ".env",
)
SECRET = re.compile(
    rb"(?i)(?:password|passwd|authorization|cookie|csrf(?:token)?|access[_-]?token|refresh[_-]?token|session(?:id|token)?|logger[_-]?serial)"
    rb"\s*[\"']?\s*[:=]\s*[\"']?[^\s\"',;}{]{4,}"
)
MAC = re.compile(rb"(?i)(?<![0-9a-f])(?:[0-9a-f]{2}[:-]){5}[0-9a-f]{2}(?![0-9a-f])")
COORDINATES = re.compile(rb"(?i)[\"']?(?:latitude|longitude|lat|lon)[\"']?\s*[:=]\s*-?\d{1,3}(?:\.\d+)?")
FORBIDDEN_TEXT = re.compile(rb"(?i)(?:^|[/\s\"'=:])(?:\.env(?:\.[a-z0-9_-]+)?|storage[-_]?state|packet capture|raw capture)(?:$|[/\s\"'=:])")
IPV4 = re.compile(rb"(?<!\d)(?:\d{1,3}\.){3}\d{1,3}(?!\d)")
IPV6 = re.compile(rb"(?i)(?<![0-9a-f:])(?:[0-9a-f]{0,4}:){2,7}[0-9a-f]{0,4}(?![0-9a-f:])")
ARCHIVE_SIGNATURES = (
    b"PK\x03\x04", b"PK\x05\x06", b"\x1f\x8b", b"BZh", b"\xfd7zXZ\x00",
    b"7z\xbc\xaf\x27\x1c", b"Rar!\x1a\x07",
)
DATABASE_CAPTURE_SIGNATURES = (
    b"SQLite format 3\x00", b"\xd4\xc3\xb2\xa1", b"\xa1\xb2\xc3\xd4",
    b"\x4d\x3c\xb2\xa1", b"\xa1\xb2\x3c\x4d", b"\x0a\x0d\x0d\x0a",
)

def reject(message):
    print(f"artifact validator: rejected: {message}", file=sys.stderr)
    raise SystemExit(1)

if root.is_symlink() or not root.is_dir():
    reject("results path must be a real directory")

archives = []
total_archive_bytes = 0
for current, directories, files in os.walk(root, followlinks=False):
    for directory in directories:
        if (Path(current) / directory).is_symlink():
            reject("outer directory symlinks are forbidden")
    for filename in files:
        path = Path(current) / filename
        if path.is_symlink() or not path.is_file():
            reject("outer symlinks and special files are forbidden")
        if filename != "trace.zip":
            reject(f"outer file is not an allowlisted trace.zip: {path.relative_to(root)}")
        size = path.stat().st_size
        if size > MAX_ARCHIVE_BYTES:
            reject("trace archive exceeds compressed size bound")
        total_archive_bytes += size
        archives.append(path)

if len(archives) > MAX_ARCHIVES or total_archive_bytes > MAX_TOTAL_ARCHIVE_BYTES:
    reject("trace archive count or total size exceeds bounds")

for archive_path in archives:
    if not zipfile.is_zipfile(archive_path):
        reject(f"invalid or corrupt ZIP: {archive_path.relative_to(root)}")
    try:
        with zipfile.ZipFile(archive_path) as archive:
            infos = archive.infolist()
            if len(infos) > MAX_MEMBERS:
                reject("trace member count exceeds bound")
            seen = set()
            expanded = 0
            for info in infos:
                name = info.filename
                if name in seen:
                    reject("duplicate trace member")
                seen.add(name)
                if "\\" in name:
                    reject("backslash path in trace member")
                path = PurePosixPath(name)
                if path.is_absolute() or ".." in path.parts:
                    reject("path traversal in trace member")
                mode = (info.external_attr >> 16) & 0o170000
                if mode == stat.S_IFLNK:
                    reject("symlink trace member")
                if info.flag_bits & 0x1:
                    reject("encrypted trace member")
                if info.is_dir():
                    if name != "resources/":
                        reject("unknown trace directory")
                    continue
                if not (CORE_MEMBER.fullmatch(name) or RESOURCE_MEMBER.fullmatch(name)):
                    reject(f"unknown Playwright trace member: {name}")
                lowered = name.lower()
                if FORBIDDEN_NAME.search(lowered) or lowered.endswith(FORBIDDEN_SUFFIXES):
                    reject(f"forbidden trace member name: {name}")
                expanded += info.file_size
                if info.file_size > MAX_MEMBER_BYTES or expanded > MAX_EXPANDED_BYTES:
                    reject("expanded trace size exceeds bounds")
                content = archive.read(info)
                if content.startswith(ARCHIVE_SIGNATURES) or (len(content) > 262 and content[257:262] == b"ustar"):
                    reject("nested archive payload")
                if content.startswith(DATABASE_CAPTURE_SIGNATURES):
                    reject("database or packet-capture payload")
                if SECRET.search(content):
                    reject("secret-like value in trace")
                if MAC.search(content):
                    reject("MAC address in trace")
                if COORDINATES.search(content):
                    reject("coordinates in trace")
                if FORBIDDEN_TEXT.search(content):
                    reject("forbidden environment, storage-state, or raw-capture reference in trace")
                for raw in IPV4.findall(content):
                    try:
                        address = ipaddress.ip_address(raw.decode("ascii"))
                    except ValueError:
                        continue
                    allowed = address.is_loopback or address.is_unspecified or address in ipaddress.ip_network("192.0.2.0/24") or address in ipaddress.ip_network("198.51.100.0/24") or address in ipaddress.ip_network("203.0.113.0/24")
                    if not allowed:
                        reject(f"non-documentation IPv4 address in trace: {address}")
                for raw in IPV6.findall(content):
                    try:
                        address = ipaddress.ip_address(raw.decode("ascii"))
                    except ValueError:
                        continue
                    if not (address.is_loopback or address.is_unspecified):
                        reject("IPv6 address in trace")
    except (OSError, zipfile.BadZipFile, RuntimeError) as error:
        reject(f"invalid or corrupt ZIP: {error}")

print(len(archives))
PY
)"

if [[ "$trace_count" -eq 0 ]]; then
  emit false
else
  emit true
fi
