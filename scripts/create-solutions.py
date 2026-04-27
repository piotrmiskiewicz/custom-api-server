#!/usr/bin/env python3
"""
Creates 50 000 Solution objects spread across 10 namespaces.

Usage: python3 scripts/create-solutions.py [SERVER]
  SERVER defaults to https://localhost:8443
"""

import subprocess
import sys
import tempfile
import os
from concurrent.futures import ThreadPoolExecutor, as_completed
#
# SERVER = sys.argv[1] if len(sys.argv) > 1 else "https://localhost:8443"
# KUBECTL = ["kubectl", f"--server={SERVER}", "--insecure-skip-tls-verify"]
KUBECTL=["kubectl"]  # assumes kubectl is configured to point to the right cluster

NAMESPACES = 10
TOTAL = 500_000
PER_NS = TOTAL // NAMESPACES   # 5 000 per namespace
BATCH = 500                     # objects per kubectl apply call
PARALLEL = 8                    # concurrent kubectl apply processes


def kubectl(*args, stdin=None):
    result = subprocess.run(
        KUBECTL + list(args),
        input=stdin,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise RuntimeError(result.stderr.strip())
    return result.stdout


def ensure_namespaces():
    print("Creating namespaces...")
    for i in range(1, NAMESPACES + 1):
        ns = f"ns-{i}"
        manifest = f"apiVersion: v1\nkind: Namespace\nmetadata:\n  name: {ns}\n"
        kubectl("apply", "-f", "-", stdin=manifest)


def build_batch(ns: str, start: int, end: int) -> str:
    docs = []
    for j in range(start, end + 1):
        name = f"solution-{j:05d}"
        docs.append(
            f"---\n"
            f"apiVersion: solution.piotrmiskiewicz.github.com/v1alpha1\n"
            f"kind: Solution\n"
            f"metadata:\n"
            f"  name: {name}\n"
            f"  namespace: {ns}\n"
            f"spec:\n"
            f"  solutionName: {name}\n"
        )
    return "".join(docs)


def apply_batch(batch_file: str) -> int:
    kubectl("apply", "-f", batch_file)
    return BATCH


def main():
    ensure_namespaces()

    with tempfile.TemporaryDirectory() as tmpdir:
        # Generate batch files
        print("Generating YAML batches...")
        batch_files = []
        for i in range(1, NAMESPACES + 1):
            ns = f"ns-{i}"
            for start in range(1, PER_NS + 1, BATCH):
                end = min(start + BATCH - 1, PER_NS)
                content = build_batch(ns, start, end)
                path = os.path.join(tmpdir, f"batch-ns{i:02d}-{start:05d}.yaml")
                with open(path, "w") as f:
                    f.write(content)
                batch_files.append(path)

        total_batches = len(batch_files)
        print(f"Applying {total_batches} batches of up to {BATCH} objects ({PARALLEL} in parallel)...")

        created = 0
        failed = 0
        with ThreadPoolExecutor(max_workers=PARALLEL) as pool:
            futures = {pool.submit(apply_batch, bf): bf for bf in batch_files}
            for i, future in enumerate(as_completed(futures), 1):
                try:
                    created += future.result()
                except RuntimeError as e:
                    failed += 1
                    print(f"\n  ERROR applying {futures[future]}: {e}")
                if i % 10 == 0 or i == total_batches:
                    print(f"  {i}/{total_batches} batches done ({created} objects)")

    status = f"Done. {created} solutions created across {NAMESPACES} namespaces."
    if failed:
        status += f" ({failed} batches failed)"
    print(status)


if __name__ == "__main__":
    main()
