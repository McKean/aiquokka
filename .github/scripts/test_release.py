import contextlib
from datetime import date
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
            if self.tag_object is None:
                raise release.GitHubAPIError(404, "not found")
            return {"object": {"sha": self.tag_object}}
        if method == "POST" and url.endswith("/git/refs"):
            self.tag_object = payload["sha"]
            return {"ref": payload["ref"], "object": {"sha": self.tag_object}}
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
    def test_first_calendar_release(self):
        self.assertEqual(release.next_calver_tag([], date(2026, 10, 2)), "v2026.10.0")

    def test_calendar_counter_uses_highest_tag_including_prereleases(self):
        tags = ["v2026.10.2", "v2026.10.11-rc.1", "v2026.10.4", "v2026.10.9+build"]
        self.assertEqual(release.next_calver_tag(tags, date(2026, 10, 2)), "v2026.10.12")

    def test_calendar_counter_resets_each_month_and_year(self):
        tags = ["v2026.9.99", "v2025.10.8", "v1.2.3", "v2026.010.5", "latest"]
        self.assertEqual(release.next_calver_tag(tags, date(2026, 10, 2)), "v2026.10.0")

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


class NotesTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / "releases").mkdir()
        self.path = self.root / "releases" / "next.yaml"

    def test_yaml_supports_quoted_items_and_multiline_highlights(self):
        self.path.write_text('title: Release\nhighlights: |\n  First line.\n  Second line.\nfeatures:\n  - "New: **feature**"\nfixes: []\n')
        notes = release.load_notes(self.path)
        self.assertEqual(notes["highlights"], "First line.\nSecond line.\n")
        self.assertEqual(notes["features"], ["New: **feature**"])

    def test_invalid_notes_fail_instead_of_silently_losing_content(self):
        for text in (
            "title: Only a title\n",
            "features: single string\n",
            "features: [true]\n",
            "features: ['']\n",
            "features: [Fine]\nfeature: [Typo]\n",
            "features: [First]\nfeatures: [Duplicate]\n",
            "features: [unclosed\n",
            "- list instead of mapping\n",
            "!!python/object:builtins.object {}\n",
        ):
            with self.subTest(text=text):
                self.path.write_text(text)
                with self.assertRaises(ValueError):
                    release.load_notes(self.path)

    def test_notes_validation_checks_versioned_files_too(self):
        self.path.write_text('features: [Good]\n')
        (self.root / "releases" / "v2026.10.0.yaml").write_text('features: [Good]\n')
        with contextlib.redirect_stdout(io.StringIO()):
            release.validate_notes(self.root)
        (self.root / "releases" / "v2026.10.0.yaml").write_text('fixes: wrong\n')
        with contextlib.redirect_stdout(io.StringIO()), self.assertRaises(ValueError):
            release.validate_notes(self.root)

    def test_notes_symlinks_are_rejected(self):
        target = self.root / "external.yaml"
        target.write_text('features: [Good]\n')
        self.path.symlink_to(target)
        with self.assertRaises(RuntimeError):
            release.load_notes(self.path)


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

    def write_notes(self, text, name="next.yaml"):
        notes = self.root / "releases" / name
        notes.parent.mkdir(exist_ok=True)
        notes.write_text(text)
        return notes

    def publish_calendar(self, dry_run=False):
        with contextlib.redirect_stdout(io.StringIO()) as output:
            release.publish_release(
                self.directory, "v2026.10.0", "owner/repo", self.root, self.api,
                dry_run=dry_run, create_tag=True,
            )
        return output.getvalue()

    def test_automatic_version_reuses_tag_for_the_same_commit(self):
        self.git("tag", "v2026.10.0")
        result = release.resolve_version("workflow_dispatch", "refs/heads/main", root=self.root, date=date(2026, 10, 2))
        self.assertEqual(result, {"tag": "v2026.10.0", "commit": self.commit, "publish": "true", "auto": "false"})
        self.git("commit", "--allow-empty", "-q", "-m", "Next release")
        result = release.resolve_version("workflow_dispatch", "refs/heads/main", root=self.root, date=date(2026, 10, 2))
        self.assertEqual(result["tag"], "v2026.10.1")
        self.assertEqual(result["auto"], "true")
        self.assertEqual(result["commit"], self.git("rev-parse", "HEAD"))

    def test_manual_tag_and_tag_push_both_check_out_exact_version(self):
        manual = release.resolve_version("workflow_dispatch", "refs/heads/main", " v1.2.3 ", self.root)
        pushed = release.resolve_version("push", "refs/tags/v1.2.3", root=self.root)
        self.assertEqual(manual, pushed)
        self.assertEqual(manual["tag"], "v1.2.3")
        self.assertEqual(manual["auto"], "false")
        self.git("commit", "--allow-empty", "-q", "-m", "Different checkout")
        with self.assertRaisesRegex(RuntimeError, "checkout does not match"):
            release.resolve_version("workflow_dispatch", "refs/heads/main", "v1.2.3", self.root)

    def test_ci_builds_do_not_create_releases(self):
        for event, ref in (("push", "refs/heads/main"), ("pull_request", "refs/pull/9/merge")):
            with self.subTest(event=event):
                result = release.resolve_version(event, ref, root=self.root)
                self.assertEqual(result["tag"], "dev-" + self.commit[:7])
                self.assertEqual(result["publish"], "false")

    def test_automatic_publication_creates_tag_at_built_commit(self):
        self.api.tag_object = None
        self.publish_calendar()
        tag_request = next(payload for method, url, payload in self.api.requests if method == "POST" and url.endswith("/git/refs"))
        self.assertEqual(tag_request, {"ref": "refs/tags/v2026.10.0", "sha": self.commit})
        self.assertEqual(self.api.release["target_commitish"], self.commit)
        self.assertFalse(self.api.release["prerelease"])
        self.assertFalse(self.api.release["draft"])
        self.assertEqual(len(self.api.uploads), 9)

    def test_automatic_publication_can_resume_after_creating_tag(self):
        self.api.tag_object = None
        self.api.fail_upload = sorted(release.ARCHIVES)[1]
        with self.assertRaises(release.GitHubAPIError):
            self.publish_calendar()
        self.assertEqual(self.api.tag_object, self.commit)
        self.assertTrue(self.api.release["draft"])
        self.api.fail_upload = None
        self.publish_calendar()
        self.assertFalse(self.api.release["draft"])
        self.assertEqual(sum(method == "POST" and url.endswith("/git/refs") for method, url, _ in self.api.requests), 1)

    def test_automatic_tag_collision_never_overwrites_existing_tag(self):
        with self.assertRaisesRegex(RuntimeError, "remote tag"):
            self.publish_calendar()
        self.assertFalse(any(method != "GET" for method, _, _ in self.api.requests))

    def test_automatic_dry_run_does_not_create_tag_or_contact_github(self):
        self.api.tag_object = None
        output = self.publish_calendar(dry_run=True)
        self.assertIn("Would create tag: v2026.10.0", output)
        self.assertEqual(self.api.requests, [])
        self.assertEqual(self.git("tag", "--list"), "v1.2.3")

    def test_invalid_yaml_fails_before_creating_automatic_tag(self):
        self.api.tag_object = None
        self.write_notes('features: [Fine]\nfeatures: [Duplicate]\n')
        with self.assertRaises(ValueError):
            self.publish_calendar()
        self.assertEqual(self.api.requests, [])

    def test_non_calendar_tag_cannot_be_created_automatically(self):
        with self.assertRaisesRegex(ValueError, "automatic tags"):
            release.publish_release(self.directory, "v1.2.4", "owner/repo", self.root, self.api, create_tag=True)
        self.assertEqual(self.api.requests, [])

    def test_yaml_notes_render_sections_title_and_compare_link(self):
        self.api.tag_object = None
        self.write_notes('title: Easier installs\nhighlights: |\n  No Go needed.\nfeatures:\n  - "New: **installer**"\nimprovements: [Faster]\nfixes: [Windows update]\nbreaking: []\n')
        self.publish_calendar()
        self.assertEqual(self.api.release["name"], "v2026.10.0: Easier installs")
        self.assertFalse(self.api.release["generate_release_notes"])
        self.assertIn("### Features\n\n- New: **installer**", self.api.release["body"])
        self.assertIn("### Improvements\n\n- Faster", self.api.release["body"])
        self.assertIn("### Fixes\n\n- Windows update", self.api.release["body"])
        self.assertIn("compare/v1.2.3...v2026.10.0", self.api.release["body"])
        self.assertNotIn("Breaking changes", self.api.release["body"])

    def test_versioned_notes_override_next_notes(self):
        self.write_notes('features: [Upcoming]\n')
        self.write_notes('fixes: [Specific release]\n', "v1.2.3.yaml")
        self.publish()
        self.assertIn("Specific release", self.api.release["body"])
        self.assertNotIn("Upcoming", self.api.release["body"])
        self.assertIn("commits/v1.2.3", self.api.release["body"])

    def test_unchanged_next_notes_are_not_repeated_in_future_releases(self):
        self.write_notes('features: [Original feature]\n')
        self.git("add", "releases/next.yaml")
        self.git("commit", "-q", "-m", "Original notes")
        self.git("tag", "v2026.9.0")
        self.git("commit", "--allow-empty", "-q", "-m", "Later changes")
        self.assertIsNone(release.release_notes("v2026.10.0", "owner/repo", self.root))
        self.write_notes('fixes: [New fix]\n')
        notes = release.release_notes("v2026.10.0", "owner/repo", self.root)
        self.assertIn("New fix", notes["body"])
        self.assertIn("compare/v2026.9.0...v2026.10.0", notes["body"])

    def test_existing_release_notes_can_be_updated_without_reuploading_assets(self):
        self.publish()
        self.write_notes('title: Clear notes\nfixes: [A fix]\n')
        self.publish()
        self.assertEqual(self.api.release["name"], "v1.2.3: Clear notes")
        self.assertIn("A fix", self.api.release["body"])
        self.assertEqual(len(self.api.uploads), 9)

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
