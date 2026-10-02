#!/usr/bin/env python3
import argparse
import hashlib
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import zipfile


def main():
    root = Path(__file__).resolve().parent.parent
    native_os, native_arch, host_os = subprocess.check_output(
        ["go", "env", "GOOS", "GOARCH", "GOHOSTOS"], text=True, cwd=root
    ).splitlines()
    parser = argparse.ArgumentParser(description="Build a release archive and its SHA-256 checksum")
    parser.add_argument("version", nargs="?", default="dev")
    parser.add_argument("--os", default=native_os, choices=["darwin", "linux", "windows"])
    parser.add_argument("--arch", default=native_arch, choices=["amd64", "arm64"])
    parser.add_argument("--output", type=Path, default=root / "dist")
    args = parser.parse_args()
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._+-]*", args.version):
        parser.error("version must contain only letters, digits, dots, underscores, pluses or hyphens")
    if args.os == "darwin" and host_os != "darwin":
        parser.error("macOS builds require a macOS host with Xcode Command Line Tools")

    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    env.update(GOOS=args.os, GOARCH=args.arch, CGO_ENABLED="1" if args.os == "darwin" else "0")
    if args.os == "darwin":
        env["MACOSX_DEPLOYMENT_TARGET"] = "12.0"
        for flag in ["CGO_CFLAGS", "CGO_LDFLAGS"]:
            env[flag] = f"{env.get(flag, '')} -mmacosx-version-min=12.0".strip()

    binary_name = "aiquokka.exe" if args.os == "windows" else "aiquokka"
    extension = "zip" if args.os == "windows" else "tar.gz"
    archive = output / f"aiquokka_{args.os}_{args.arch}.{extension}"
    with tempfile.TemporaryDirectory(prefix="aiquokka-build-") as directory:
        binary = Path(directory) / binary_name
        subprocess.run(
            [
                "go", "build", "-trimpath", "-ldflags",
                f"-s -w -X github.com/McKean/aiquokka/cmd.Version={args.version}",
                "-o", str(binary), ".",
            ],
            cwd=root, env=env, check=True,
        )
        if args.os == "darwin":
            subprocess.run(["codesign", "--force", "--sign", "-", str(binary)], check=True)
        files = [binary, root / "LICENSE", root / "README.md"]
        if args.os == "windows":
            with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as package:
                for file in files:
                    package.write(file, file.name)
        else:
            with tarfile.open(archive, "w:gz") as package:
                for file in files:
                    package.add(file, arcname=file.name)

    digest = hashlib.sha256()
    with archive.open("rb") as file:
        for block in iter(lambda: file.read(1024 * 1024), b""):
            digest.update(block)
    archive.with_name(archive.name + ".sha256").write_text(
        f"{digest.hexdigest()}  {archive.name}\n", encoding="utf-8"
    )
    print(archive)


if __name__ == "__main__":
    main()
