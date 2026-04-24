#!/usr/bin/env bash
# Creates 50 000 Solution objects spread across 10 namespaces.
# Batches objects into multi-document YAML files and applies each batch
# in parallel to avoid the per-process overhead of individual kubectl calls.
#
# Usage: ./scripts/create-solutions.sh [SERVER]
#   SERVER defaults to https://localhost:8443

set -euo pipefail

SERVER="${1:-https://localhost:8443}"
KUBECTL="kubectl --server=${SERVER} --insecure-skip-tls-verify"

NAMESPACES=10
TOTAL=50000
PER_NS=$(( TOTAL / NAMESPACES ))   # 5 000 per namespace
BATCH=500                           # objects per kubectl apply call
PARALLEL=8                          # concurrent kubectl apply processes

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

# Ensure namespaces exist
echo "Creating namespaces..."
for i in $(seq 1 $NAMESPACES); do
  $KUBECTL create namespace "ns-${i}" --dry-run=client -o yaml | $KUBECTL apply -f - >/dev/null
done

# Generate batch YAML files
echo "Generating YAML batches..."
batch_num=0
for i in $(seq 1 $NAMESPACES); do
  NS="ns-${i}"
  j=1
  while (( j <= PER_NS )); do
    batch_file="${TMPDIR}/batch-$(printf '%05d' $batch_num).yaml"
    # Write up to BATCH objects into one file
    for (( k=0; k<BATCH && j<=PER_NS; k++, j++ )); do
      cat >> "$batch_file" <<EOF
---
apiVersion: solution.piotrmiskiewicz.github.com/v1alpha1
kind: Solution
metadata:
  name: solution-$(printf '%05d' $j)
  namespace: ${NS}
spec:
  solutionName: solution-$(printf '%05d' $j)
EOF
    done
    (( batch_num++ ))
  done
done

total_batches=$batch_num
echo "Applying ${total_batches} batches of up to ${BATCH} objects (${PARALLEL} in parallel)..."

# Apply all batches in parallel via xargs
ls "$TMPDIR"/batch-*.yaml \
  | xargs -P "$PARALLEL" -I{} sh -c \
      "$KUBECTL apply -f {} >/dev/null && echo -n '.'"

echo ""
echo "Done. ${TOTAL} solutions created across ${NAMESPACES} namespaces."
