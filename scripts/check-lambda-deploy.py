#!/usr/bin/env python3
"""Checks that each deployed function runs exactly the build CI attested.

For every <name>.zip in the build directory, it fetches the code that the
function <prefix>-<name> holds from Lambda itself, and checks that:

  - the function has a code signing config that enforces signatures;
  - the deployed package is that build plus AWS Signer's signature: the same
    files with the same bytes and modes, and nothing else but META_INF/;
  - Lambda's CodeSha256 is the hash of that package.

It prints both hashes, so the public log ties the deployed code to the
attested, reproducible build. Nothing environment-specific is printed (the
download address is a presigned URL and stays private).

Usage: check-lambda-deploy.py <build-dir> <function-prefix>
Needs the AWS CLI with credentials that may read the functions.
"""

import base64
import hashlib
import io
import json
import pathlib
import re
import subprocess
import sys
import urllib.request
import zipfile

SIGNATURE = re.compile(r"META_INF/(aws_signer_signature_v[0-9.]+\.SF)?")


def aws(*args):
    # AWS's error text can hold ARNs, so it isn't printed.
    run = subprocess.run(["aws", *args, "--output", "json"], capture_output=True, text=True)
    if run.returncode:
        raise RuntimeError(f"aws {args[0]} {args[1]} failed")
    return json.loads(run.stdout) if run.stdout.strip() else {}


def entries(archive):
    return {i.filename: (archive.read(i), i.external_attr >> 16) for i in archive.infolist()}


def compare(built, deployed):
    """Returns why deployed isn't built plus a signature, or None."""
    want = entries(zipfile.ZipFile(io.BytesIO(built)))
    got = entries(zipfile.ZipFile(io.BytesIO(deployed)))
    extra = sorted(got.keys() - want.keys())
    if not any(name != "META_INF/" for name in extra):
        return "the deployed package has no signature"
    if any(not SIGNATURE.fullmatch(name) for name in extra):
        return f"unexpected files in the deployed package: {', '.join(extra)}"
    for name, content in want.items():
        if got.get(name) != content:
            return f"{name} differs from the build"
    return None


def check(build, function):
    built = build.read_bytes()
    config = aws("lambda", "get-function", "--function-name", function)

    signing = aws("lambda", "get-function-code-signing-config", "--function-name", function)
    arn = signing.get("CodeSigningConfigArn")
    if not arn:
        return "no code signing config"
    policy = aws("lambda", "get-code-signing-config", "--code-signing-config-arn", arn)
    if policy["CodeSigningConfig"]["CodeSigningPolicies"]["UntrustedArtifactOnDeployment"] != "Enforce":
        return "code signing isn't enforced"

    with urllib.request.urlopen(config["Code"]["Location"], timeout=60) as response:
        deployed = response.read()
    if base64.b64encode(hashlib.sha256(deployed).digest()).decode() != config["Configuration"]["CodeSha256"]:
        return "the downloaded package doesn't match Lambda's CodeSha256"

    problem = compare(built, deployed)
    if problem:
        return problem

    print(f"{build.stem:<8} build    sha256 {hashlib.sha256(built).hexdigest()}")
    print(f"{'':<8} deployed sha256 {hashlib.sha256(deployed).hexdigest()} (build + signature)")
    return None


def main():
    if len(sys.argv) != 3:
        sys.exit("usage: check-lambda-deploy.py <build-dir> <function-prefix>")
    builds = sorted(pathlib.Path(sys.argv[1]).glob("*.zip"))
    if not builds:
        sys.exit(f"no builds in {sys.argv[1]}")
    failed = False
    for build in builds:
        try:
            problem = check(build, f"{sys.argv[2]}-{build.stem}")
        except (RuntimeError, OSError, KeyError, zipfile.BadZipFile) as err:
            problem = str(err) or type(err).__name__
        if problem:
            print(f"::error::{build.stem}: {problem}")
            failed = True
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
