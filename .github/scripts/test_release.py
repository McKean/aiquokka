import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock
from urllib.error import HTTPError


SPEC = importlib.util.spec_from_file_location("aiquokka_release", Path(__file__).with_name("release.py"))
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)


def make_assets(directory):
    directory.mkdir(exist_ok=True)
    for name in sorted(release.EXPECTED_ASSETS):
        path = directory / name
        path.write_bytes(f"artifact:{name}".encode())
        if name in release.ARCHIVES:
            (directory / (name + ".sha256")).write_text(f"{release.sha256(path)}  {name}\n")
    write_manifest(directory)


def write_manifest(directory):
    (directory / "checksums.txt").write_text("".join(
        f"{release.sha256(directory / name)}  {name}\n" for name in sorted(release.EXPECTED_ASSETS)
    ))


class FakeAPI:
    api_url = "https://api.github.test"

    def __init__(self, tag_object):
        self.tag_object = tag_object
        self.release = None
        self.assets = {}
        self.requests = []
        self.uploads = []
        self.fail_upload = None

    def request_json(self, method, url, payload=None):
        self.requests.append((method, url, payload))
        if method == "GET" and "/git/ref/tags/" in url:
            return {"object": {"sha": self.tag_object}}
        if method == "GET" and "/releases/tags/" in url:
            if self.release is None:
                raise release.GitHubAPIError(404, "not found")
            return self.release
        if method == "POST" and url.endswith("/releases"):
            self.release = {
                **payload, "id": 42,
                "upload_url": "https://uploads.github.test/releases/42/assets{?name,label}",
                "html_url": "https://github.test/releases/tag/" + payload["tag_name"],
            }
            return self.release
        if method == "GET" and "/42/assets?" in url:
            return list(self.assets.values())
        if method == "DELETE" and "/releases/assets/" in url:
            asset_id = int(url.rsplit("/", 1)[-1])
            self.assets = {name: asset for name, asset in self.assets.items() if asset["id"] != asset_id}
            return None
        if method == "PATCH" and url.endswith("/releases/42"):
            self.release.update(payload)
            return self.release
        raise AssertionError(f"Unexpected request: {method} {url}")

    def upload_asset(self, url, path):
        if path.name == self.fail_upload:
            raise release.GitHubAPIError(502, "failed upload")
        self.uploads.append(path.name)
        asset = {
            "id": len(self.uploads) + 100,
            "name": path.name,
            "size": path.stat().st_size,
            "state": "uploaded",
            "digest": "sha256:" + release.sha256(path),
        }
        self.assets[path.name] = asset
        return asset


class TagTests(unittest.TestCase):
    def test_semver(self):
        for tag in ("v0.1.0", "v12.3.45", "v1.2.3-rc.1", "v1.2.3+build-with-hyphens"):
            with self.subTest(tag=tag):
                self.assertEqual(release.validate_tag(tag), tag)

    def test_invalid_tag(self):
        for tag in ("latest", "1.2.3", "v1.2", "v01.2.3", "v1.2.3-01", "v1.2.3-rc.01", "v1.2.3\n", "../v1.2.3"):
            with self.subTest(tag=tag):
                with self.assertRaises(ValueError):
                    release.validate_tag(tag)

    def test_metadata_hyphen_is_not_prerelease(self):
        api = FakeAPI("tag")
        result = release.create_or_get_release(api, "owner/repo", "v1.2.3+build-with-hyphens", "commit")
        self.assertFalse(result["prerelease"])
        self.assertTrue(result["draft"])

    def test_permission_error_is_not_treated_as_missing_release(self):
        api = FakeAPI("tag")
        with mock.patch.object(api, "request_json", side_effect=release.GitHubAPIError(403, "forbidden")) as request:
            with self.assertRaises(release.GitHubAPIError):
                release.create_or_get_release(api, "owner/repo", "v1.2.3", "commit")
        self.assertEqual(request.call_count, 1)


class AssetTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.directory = self.root / "release"
        make_assets(self.directory)
        for name in release.INSTALLERS:
            (self.root / name).write_bytes(f"current:{name}".encode())

    def test_complete_release(self):
        assets = release.collect_and_verify_assets(self.directory)
        self.assertEqual(len(assets), 9)
        self.assertEqual(assets[-1].name, "checksums.txt")

    def test_prepare_copies_installers_and_checksums_them(self):
        release.prepare_assets(self.directory, self.root)
        for name in release.INSTALLERS:
            self.assertEqual((self.directory / name).read_bytes(), (self.root / name).read_bytes())
        self.assertEqual(len(release.collect_and_verify_assets(self.directory)), 9)

    def test_prepare_rejects_corrupted_build_artifact(self):
        name = sorted(release.ARCHIVES)[0]
        (self.directory / name).write_bytes(b"corrupted")
        with self.assertRaisesRegex(RuntimeError, "checksum mismatch"):
            release.prepare_assets(self.directory, self.root)

    def test_missing_architecture(self):
        (self.directory / "aiquokka_windows_arm64.zip").unlink()
        with self.assertRaisesRegex(RuntimeError, "missing"):
            release.collect_and_verify_assets(self.directory)

    def test_checksum_mismatch(self):
        (self.directory / "install.sh").write_bytes(b"changed installer")
        with self.assertRaisesRegex(RuntimeError, "checksum mismatch"):
            release.collect_and_verify_assets(self.directory)

    def test_duplicate_checksum(self):
        path = self.directory / "checksums.txt"
        contents = path.read_text()
        path.write_text(contents + contents.splitlines()[0] + "\n")
        with self.assertRaisesRegex(RuntimeError, "duplicate"):
            release.collect_and_verify_assets(self.directory)

    def test_unexpected_artifact(self):
        (self.directory / "aiquokka_linux_386.tar.gz").write_bytes(b"unexpected")
        with self.assertRaisesRegex(RuntimeError, "unexpected"):
            release.collect_and_verify_assets(self.directory)

    def test_prepare_rejects_symlink_without_changing_its_target(self):
        target = self.root / "untouched.txt"
        target.write_bytes(b"original")
        path = self.directory / "install.sh"
        path.unlink()
        path.symlink_to(target)
        with self.assertRaisesRegex(RuntimeError, "invalid release file"):
            release.prepare_assets(self.directory, self.root)
        self.assertEqual(target.read_bytes(), b"original")


class PublishTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.directory = self.root / "release"
        make_assets(self.directory)
        self.git("init", "-q")
        self.git("config", "user.name", "Release Test")
        self.git("config", "user.email", "release@example.test")
        self.git("config", "commit.gpgsign", "false")
        self.git("config", "tag.gpgsign", "false")
        self.git("commit", "--allow-empty", "-q", "-m", "Test release")
        self.git("tag", "-a", "v1.2.3", "-m", "Test tag")
        self.tag_object, self.commit = release.tag_checkout("v1.2.3", self.root)
        self.api = FakeAPI(self.tag_object)

    def git(self, *args):
        return subprocess.check_output(
            ["git", "-c", "core.hooksPath=/dev/null", *args], cwd=self.root, text=True, stderr=subprocess.PIPE
        ).strip()

    def publish(self, dry_run=False):
        with contextlib.redirect_stdout(io.StringIO()):
            release.publish_release(self.directory, "v1.2.3", "owner/repo", self.root, self.api, dry_run)

    def test_first_publish_uses_tag_commit_and_finalizes_after_all_uploads(self):
        self.assertNotEqual(self.tag_object, self.commit)
        self.publish()
        self.assertEqual(self.api.release["target_commitish"], self.commit)
        self.assertFalse(self.api.release["draft"])
        self.assertEqual(len(self.api.uploads), 9)
        self.assertEqual(self.api.uploads[-1], "checksums.txt")
        self.assertEqual(self.api.requests[-1][0], "PATCH")

    def test_rerun_reuses_existing_release_and_unchanged_assets(self):
        self.publish()
        self.publish()
        self.assertEqual(len(self.api.uploads), 9)
        self.assertEqual(sum(method == "POST" for method, _, _ in self.api.requests), 1)
        self.assertFalse(any(method == "DELETE" for method, _, _ in self.api.requests))

    def test_upload_failure_keeps_new_release_draft_and_can_resume(self):
        self.api.fail_upload = sorted(release.ARCHIVES)[1]
        with self.assertRaises(release.GitHubAPIError):
            self.publish()
        self.assertTrue(self.api.release["draft"])
        self.assertFalse(any(method == "PATCH" for method, _, _ in self.api.requests))
        self.api.fail_upload = None
        self.publish()
        self.assertFalse(self.api.release["draft"])
        self.assertEqual(len(self.api.uploads), 9)

    def test_changed_asset_is_replaced_without_deleting_unrelated_assets(self):
        self.publish()
        self.api.assets["other.txt"] = {"id": 999, "name": "other.txt"}
        (self.directory / "install.sh").write_bytes(b"updated installer")
        write_manifest(self.directory)
        self.publish()
        self.assertEqual(len(self.api.uploads), 11)
        self.assertIn("other.txt", self.api.assets)

    def test_corrupt_bundle_fails_before_contacting_github(self):
        (self.directory / "install.sh").write_bytes(b"corrupt")
        with self.assertRaisesRegex(RuntimeError, "checksum mismatch"):
            self.publish()
        self.assertEqual(self.api.requests, [])

    def test_missing_tag_does_not_contact_github(self):
        self.git("tag", "-d", "v1.2.3")
        with self.assertRaisesRegex(RuntimeError, "existing release tag"):
            self.publish()
        self.assertEqual(self.api.requests, [])

    def test_manual_run_cannot_publish_from_the_wrong_checkout(self):
        self.git("commit", "--allow-empty", "-q", "-m", "Later main commit")
        with self.assertRaisesRegex(RuntimeError, "checkout does not match"):
            self.publish()
        self.assertEqual(self.api.requests, [])

    def test_remote_tag_mismatch_does_not_create_release(self):
        self.api.tag_object = "different-tag"
        with self.assertRaisesRegex(RuntimeError, "remote tag"):
            self.publish()
        self.assertFalse(any(method != "GET" for method, _, _ in self.api.requests))

    def test_dry_run_does_not_contact_github(self):
        self.publish(dry_run=True)
        self.assertEqual(self.api.requests, [])

    def test_cli_dry_run_validates_bundle_and_tag_without_network(self):
        script = self.root / ".github" / "scripts" / "release.py"
        script.parent.mkdir(parents=True)
        script.write_bytes(Path(release.__file__).read_bytes())
        env = os.environ.copy()
        env.update(
            RELEASE_TAG="v1.2.3",
            GITHUB_REPOSITORY="owner/repo",
            GITHUB_TOKEN="test-token",
            GITHUB_API_URL="http://127.0.0.1:1",
        )
        result = subprocess.run(
            [sys.executable, str(script), "--dry-run"], env=env, text=True, capture_output=True
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(self.commit, result.stdout)
        self.assertEqual(result.stdout.count("Validated:"), 9)
        self.assertNotIn("test-token", result.stdout + result.stderr)

    def test_immutable_release_rejects_changed_asset_before_deleting_it(self):
        self.publish()
        self.api.release["immutable"] = True
        (self.directory / "install.sh").write_bytes(b"updated")
        write_manifest(self.directory)
        with self.assertRaisesRegex(RuntimeError, "immutable release"):
            self.publish()
        self.assertFalse(any(method == "DELETE" for method, _, _ in self.api.requests))


class APIClientTests(unittest.TestCase):
    def test_upload_uses_binary_body_and_url_encoded_filename(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "asset with spaces.zip"
            path.write_bytes(b"binary contents")
            response = io.BytesIO(json.dumps({"state": "uploaded"}).encode())
            with mock.patch.object(release, "urlopen", return_value=response) as request:
                release.GitHubAPI("test-token", "https://api.github.test").upload_asset(
                    "https://uploads.github.test/assets{?name,label}", path
                )
            sent = request.call_args.args[0]
            self.assertIn("name=asset+with+spaces.zip", sent.full_url)
            self.assertEqual(sent.data, b"binary contents")
            self.assertEqual(sent.get_header("Authorization"), "Bearer test-token")
            self.assertEqual(request.call_args.kwargs["timeout"], 120)

    def test_api_errors_redact_the_token(self):
        error = HTTPError("https://api.github.test", 403, "forbidden", None, io.BytesIO(b"test-token"))
        with mock.patch.object(release, "urlopen", side_effect=error):
            with self.assertRaises(release.GitHubAPIError) as failure:
                release.GitHubAPI("test-token", "https://api.github.test").request_json("GET", "https://api.github.test")
        self.assertNotIn("test-token", str(failure.exception))


if __name__ == "__main__":
    unittest.main()
