#!/usr/bin/env python3
import hashlib
import io
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest


INSTALLER = Path(__file__).resolve().parent.parent / "install.sh"


class InstallTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="aiquokka-installer-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.tools = self.root / "tools"
        self.tools.mkdir()
        self.bin_dir = self.root / "installed binaries"
        self.assets = self.root / "release"
        self.assets.mkdir()
        self.log = self.root / "requests.txt"
        self.env = os.environ.copy()
        self.env.update(
            PATH=str(self.tools) + os.pathsep + os.environ["PATH"],
            BIN_DIR=str(self.bin_dir),
            VERSION="v1.2.3",
            AIQUOKKA_REPO="McKean/aiquokka",
            FAKE_PLATFORM="Linux",
            FAKE_ARCH="x86_64",
            FAKE_RELEASE_ROOT=str(self.assets),
            FAKE_CURL_LOG=str(self.log),
            TMPDIR=str(self.root),
        )
        self.write_tool("uname", """#!/bin/sh
case "$1" in
  -s) printf '%s\\n' "$FAKE_PLATFORM" ;;
  -m) printf '%s\\n' "$FAKE_ARCH" ;;
  *) exit 1 ;;
esac
""")
        self.write_tool("curl", f"""#!{sys.executable}
import os
from pathlib import Path
import shutil
import sys

args = sys.argv[1:]
url = args[-1]
with open(os.environ["FAKE_CURL_LOG"], "a") as log:
    log.write(url + "\\n")
base = "https://github.com/" + os.environ["AIQUOKKA_REPO"] + "/releases"
if url == base + "/latest":
    print(base + "/tag/v1.2.3", end="")
    sys.exit(0)
if not url.startswith(base + "/download/v1.2.3/"):
    sys.exit(22)
source = Path(os.environ["FAKE_RELEASE_ROOT"]) / url.rsplit("/", 1)[1]
if not source.is_file():
    sys.exit(22)
output = args[args.index("--output") + 1]
shutil.copyfile(source, output)
""")

    def write_tool(self, name, text):
        file = self.tools / name
        file.write_text(text, encoding="utf-8")
        file.chmod(0o755)

    def prepare_release(self, goos="linux", goarch="amd64", corrupt=False, binary=True):
        name = f"aiquokka_{goos}_{goarch}.tar.gz"
        archive = self.assets / name
        payload = b"#!/bin/sh\nprintf 'aiquokka version v1.2.3\\n'\n"
        with tarfile.open(archive, "w:gz") as package:
            entry = tarfile.TarInfo("aiquokka" if binary else "other")
            entry.size = len(payload)
            entry.mode = 0o755
            package.addfile(entry, io.BytesIO(payload))
        digest = "0" * 64 if corrupt else hashlib.sha256(archive.read_bytes()).hexdigest()
        (self.assets / "checksums.txt").write_text(f"{digest}  {name}\n", encoding="utf-8")
        return payload

    def run_installer(self, piped=False):
        args = ["sh"] if piped else ["sh", str(INSTALLER)]
        return subprocess.run(
            args, env=self.env, input=INSTALLER.read_text() if piped else None,
            text=True, capture_output=True,
        )

    def assert_clean(self):
        self.assertEqual(list(self.root.glob("aiquokka-install.*")), [])
        if self.bin_dir.exists():
            self.assertEqual(list(self.bin_dir.glob(".aiquokka.*")), [])

    def test_supported_platforms(self):
        for platform, arch, goos, goarch in [
            ("Linux", "x86_64", "linux", "amd64"),
            ("Linux", "aarch64", "linux", "arm64"),
            ("Darwin", "x86_64", "darwin", "amd64"),
            ("Darwin", "arm64", "darwin", "arm64"),
        ]:
            with self.subTest(platform=platform, arch=arch):
                self.env.update(FAKE_PLATFORM=platform, FAKE_ARCH=arch)
                payload = self.prepare_release(goos, goarch)
                result = self.run_installer()
                self.assertEqual(result.returncode, 0, result.stderr)
                installed = self.bin_dir / "aiquokka"
                self.assertEqual(installed.read_bytes(), payload)
                self.assertTrue(os.access(installed, os.X_OK))
                self.assert_clean()

    def test_latest_when_piped(self):
        self.env["VERSION"] = "latest"
        self.prepare_release()
        result = self.run_installer(piped=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("/releases/latest", self.log.read_text())
        self.assertIn("/download/v1.2.3/aiquokka_linux_amd64.tar.gz", self.log.read_text())
        self.assert_clean()

    def test_checksum_failure_preserves_existing_binary(self):
        self.prepare_release(corrupt=True)
        self.bin_dir.mkdir()
        installed = self.bin_dir / "aiquokka"
        installed.write_bytes(b"existing binary")
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("SHA-256 verification failed", result.stderr)
        self.assertEqual(installed.read_bytes(), b"existing binary")
        self.assert_clean()

    def test_successful_update(self):
        payload = self.prepare_release()
        self.bin_dir.mkdir()
        (self.bin_dir / "aiquokka").write_bytes(b"existing binary")
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.bin_dir / "aiquokka").read_bytes(), payload)
        self.assert_clean()

    def test_missing_checksum(self):
        self.prepare_release()
        (self.assets / "checksums.txt").write_text("", encoding="utf-8")
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing or invalid checksum", result.stderr)
        self.assertFalse(self.bin_dir.exists())
        self.assert_clean()

    def test_missing_download(self):
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("could not download", result.stderr)
        self.assertFalse(self.bin_dir.exists())
        self.assert_clean()

    def test_missing_binary(self):
        self.prepare_release(binary=False)
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("could not extract aiquokka", result.stderr)
        self.assertFalse(self.bin_dir.exists())
        self.assert_clean()

    def test_unsupported_platform(self):
        self.env["FAKE_PLATFORM"] = "MINGW64_NT"
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("use install.ps1", result.stderr)
        self.assertFalse(self.log.exists())

    def test_unsupported_architecture(self):
        self.env["FAKE_ARCH"] = "i686"
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unsupported architecture", result.stderr)
        self.assertFalse(self.log.exists())

    def test_invalid_version(self):
        self.env["VERSION"] = "../../other"
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("invalid release version", result.stderr)
        self.assertFalse(self.log.exists())


if __name__ == "__main__":
    unittest.main()
