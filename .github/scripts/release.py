#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlencode
from urllib.request import Request, urlopen


ROOT = Path(__file__).resolve().parents[2]
TAG_PATTERN = re.compile(
    r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?"
    r"(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?"
)
ARCHIVES = {
    f"aiquokka_{goos}_{goarch}.{'zip' if goos == 'windows' else 'tar.gz'}"
    for goos in ("linux", "darwin", "windows")
    for goarch in ("amd64", "arm64")
}
INSTALLERS = {"install.sh", "install.ps1"}
EXPECTED_ASSETS = ARCHIVES | INSTALLERS


class GitHubAPIError(RuntimeError):
    def __init__(self, status, body):
        super().__init__(f"GitHub API returned HTTP {status}: {body}")
        self.status = status


class GitHubAPI:
    def __init__(self, token, api_url):
        self.token = token
        self.api_url = api_url.rstrip("/")

    def request(self, method, url, data=None, content_type="application/json", timeout=30):
        request = Request(url, data=data, method=method, headers={
            "Accept": "application/vnd.github+json",
            "Authorization": f"Bearer {self.token}",
            "Content-Type": content_type,
            "User-Agent": "aiquokka-release-script",
            "X-GitHub-Api-Version": "2026-03-10",
        })
        try:
            with urlopen(request, timeout=timeout) as response:
                body = response.read()
        except HTTPError as error:
            with error:
                body = error.read().decode(errors="replace").replace(self.token, "<redacted>")
            raise GitHubAPIError(error.code, body) from error
        except URLError as error:
            raise RuntimeError(f"GitHub request failed: {error.reason}") from error
        return json.loads(body) if body else None

    def request_json(self, method, url, payload=None):
        data = json.dumps(payload).encode() if payload is not None else None
        return self.request(method, url, data)

    def upload_asset(self, upload_url, path):
        url = upload_url.split("{", 1)[0] + "?" + urlencode({"name": path.name})
        return self.request("POST", url, path.read_bytes(), "application/octet-stream", timeout=120)


def validate_tag(tag):
    matched = TAG_PATTERN.fullmatch(tag)
    if not matched:
        raise ValueError(f"invalid release tag {tag!r}; expected v1.2.3 or v1.2.3-rc.1")
    for identifier in (matched.group(4) or "").split("."):
        if identifier.isdigit() and len(identifier) > 1 and identifier.startswith("0"):
            raise ValueError(f"invalid numeric prerelease identifier in {tag!r}")
    return tag


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as file:
        for chunk in iter(lambda: file.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def require_file(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size == 0:
        raise RuntimeError(f"missing, empty or invalid release file: {path.name}")


def read_checksums(path, expected_names):
    require_file(path)
    checksums = {}
    for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        matched = re.fullmatch(r"([a-fA-F0-9]{64})\s+\*?([^\s]+)", line)
        if not matched:
            raise RuntimeError(f"invalid checksum on line {number} of {path.name}")
        digest, name = matched.groups()
        if name in checksums:
            raise RuntimeError(f"duplicate checksum for {name}")
        checksums[name] = digest.lower()
    if set(checksums) != expected_names:
        raise RuntimeError(f"{path.name} does not contain exactly the expected assets")
    return checksums


def verify_directory(release_dir):
    if not release_dir.is_dir():
        raise RuntimeError(f"release directory not found: {release_dir}")
    allowed = EXPECTED_ASSETS | {"checksums.txt"} | {name + ".sha256" for name in ARCHIVES}
    unexpected = {path.name for path in release_dir.iterdir()} - allowed
    if unexpected:
        raise RuntimeError(f"unexpected release files: {', '.join(sorted(unexpected))}")
    for path in release_dir.iterdir():
        if path.is_symlink() or not path.is_file():
            raise RuntimeError(f"invalid release file: {path.name}")
    for name in ARCHIVES:
        require_file(release_dir / name)


def prepare_assets(release_dir, root=ROOT):
    verify_directory(release_dir)
    for name in sorted(ARCHIVES):
        checksums = read_checksums(release_dir / (name + ".sha256"), {name})
        if sha256(release_dir / name) != checksums[name]:
            raise RuntimeError(f"checksum mismatch for {name}")
    for name in sorted(INSTALLERS):
        require_file(root / name)
        shutil.copyfile(root / name, release_dir / name)
    manifest = "".join(f"{sha256(release_dir / name)}  {name}\n" for name in sorted(EXPECTED_ASSETS))
    (release_dir / "checksums.txt").write_text(manifest, encoding="utf-8")
    return collect_and_verify_assets(release_dir)


def collect_and_verify_assets(release_dir):
    verify_directory(release_dir)
    checksums = read_checksums(release_dir / "checksums.txt", EXPECTED_ASSETS)
    for name in sorted(EXPECTED_ASSETS):
        require_file(release_dir / name)
        if sha256(release_dir / name) != checksums[name]:
            raise RuntimeError(f"checksum mismatch for {name}")
    return [release_dir / name for name in sorted(EXPECTED_ASSETS)] + [release_dir / "checksums.txt"]


def tag_checkout(tag, root=ROOT):
    def git(*args):
        return subprocess.check_output(["git", *args], cwd=root, text=True, stderr=subprocess.PIPE).strip()

    ref = f"refs/tags/{validate_tag(tag)}"
    try:
        tag_object = git("rev-parse", "--verify", ref)
        commit = git("rev-parse", "--verify", ref + "^{commit}")
    except subprocess.CalledProcessError as error:
        raise RuntimeError(f"could not resolve existing release tag {tag}") from error
    if git("rev-parse", "HEAD") != commit:
        raise RuntimeError(f"checkout does not match release tag {tag}")
    return tag_object, commit


def create_or_get_release(api, repository, tag, commit):
    url = f"{api.api_url}/repos/{repository}/releases"
    try:
        release = api.request_json("GET", url + "/tags/" + quote(tag, safe=""))
    except GitHubAPIError as error:
        if error.status != 404:
            raise
        release = api.request_json("POST", url, {
            "tag_name": tag,
            "target_commitish": commit,
            "name": tag,
            "draft": True,
            "prerelease": "-" in tag.partition("+")[0],
            "generate_release_notes": True,
        })
    if not isinstance(release, dict) or release.get("tag_name") != tag:
        raise RuntimeError("GitHub returned an invalid release response")
    if not isinstance(release.get("id"), int) or not isinstance(release.get("upload_url"), str):
        raise RuntimeError("GitHub release response is missing id or upload_url")
    return release


def replace_assets(api, repository, release, assets):
    base_url = f"{api.api_url}/repos/{repository}/releases"
    existing_assets = {}
    page = 1
    while True:
        entries = api.request_json("GET", f"{base_url}/{release['id']}/assets?per_page=100&page={page}")
        if not isinstance(entries, list):
            raise RuntimeError("GitHub returned an invalid asset list")
        for asset in entries:
            existing_assets[asset["name"]] = asset
        if len(entries) < 100:
            break
        page += 1

    for path in assets:
        existing = existing_assets.get(path.name)
        if existing and existing.get("state") == "uploaded" and existing.get("digest") == "sha256:" + sha256(path):
            print(f"Already uploaded: {path.name}")
            continue
        if release.get("immutable"):
            raise RuntimeError(f"cannot replace {path.name} in an immutable release")
        if existing:
            api.request_json("DELETE", f"{base_url}/assets/{existing['id']}")
        uploaded = api.upload_asset(release["upload_url"], path)
        if not isinstance(uploaded, dict) or uploaded.get("state") != "uploaded" or uploaded.get("size") != path.stat().st_size:
            raise RuntimeError(f"GitHub did not confirm the upload of {path.name}")
        if uploaded.get("digest") and uploaded["digest"] != "sha256:" + sha256(path):
            raise RuntimeError(f"GitHub returned an incorrect checksum for {path.name}")
        print(f"Uploaded: {path.name}")


def publish_release(release_dir, tag, repository, root=ROOT, api=None, dry_run=False):
    validate_tag(tag)
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        raise ValueError("GITHUB_REPOSITORY must be owner/name")
    assets = collect_and_verify_assets(release_dir)
    tag_object, commit = tag_checkout(tag, root)
    print(f"Release: {repository} {tag} ({commit})")
    if dry_run:
        for path in assets:
            print(f"Validated: {path.name} ({path.stat().st_size} bytes)")
        return
    if api is None:
        token = os.environ.get("GITHUB_TOKEN", "").strip()
        if not token:
            raise RuntimeError("GITHUB_TOKEN is required")
        api = GitHubAPI(token, os.environ.get("GITHUB_API_URL", "https://api.github.com"))
    remote_tag = api.request_json("GET", f"{api.api_url}/repos/{repository}/git/ref/tags/" + quote(tag, safe=""))
    if not isinstance(remote_tag, dict) or remote_tag.get("object", {}).get("sha") != tag_object:
        raise RuntimeError(f"remote tag {tag} does not match the local tag")
    release = create_or_get_release(api, repository, tag, commit)
    replace_assets(api, repository, release, assets)
    if release.get("draft"):
        published = api.request_json("PATCH", f"{api.api_url}/repos/{repository}/releases/{release['id']}", {
            "draft": False,
            "make_latest": "false" if release.get("prerelease") else "legacy",
        })
        if not isinstance(published, dict) or published.get("draft") is not False:
            raise RuntimeError("GitHub did not confirm publication of the release")
    url = release.get("html_url") or f"https://github.com/{repository}/releases/tag/{quote(tag, safe='')}"
    print(f"Published: {url}")
    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a", encoding="utf-8") as file:
            file.write(f"## Release {tag}\n\n[GitHub Release]({url})\n\n")
            file.writelines(f"- {path.name}\n" for path in assets)


def main():
    parser = argparse.ArgumentParser(description="Validate and publish aiquokka GitHub releases")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--validate-tag", metavar="TAG")
    mode.add_argument("--validate-checkout", metavar="TAG")
    mode.add_argument("--prepare", action="store_true")
    mode.add_argument("--dry-run", action="store_true")
    parser.add_argument("--release-dir", type=Path, default=ROOT / "release")
    args = parser.parse_args()
    try:
        if args.validate_tag:
            print(validate_tag(args.validate_tag))
        elif args.validate_checkout:
            print(tag_checkout(args.validate_checkout)[1])
        elif args.prepare:
            for path in prepare_assets(args.release_dir):
                print(f"Prepared: {path.name}")
        else:
            publish_release(
                args.release_dir,
                os.environ.get("RELEASE_TAG", "").strip(),
                os.environ.get("GITHUB_REPOSITORY", "").strip(),
                dry_run=args.dry_run,
            )
    except (OSError, RuntimeError, ValueError, subprocess.SubprocessError) as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
