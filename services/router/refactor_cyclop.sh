#!/bin/bash

# Script to help identify and refactor high cyclomatic complexity functions
# This script lists all cyclop issues sorted by complexity

echo "=== Cyclomatic Complexity Analysis ==="
echo "Finding all functions with cyclomatic complexity > 10..."
echo ""

# Run golangci-lint and extract cyclop issues with their complexity scores
golangci-lint run -E cyclop ./... 2>&1 | \
  grep "calculated cyclomatic complexity" | \
  sed -E 's/.*complexity for function ([^ ]+) is ([0-9]+).*/\2 \1/' | \
  sort -rn | \
  while read complexity func; do
    echo "Complexity: $complexity - Function: $func"
  done

echo ""
echo "=== Summary ==="
total=$(golangci-lint run -E cyclop ./... 2>&1 | grep -c "calculated cyclomatic complexity")
echo "Total functions with complexity > 10: $total"

# Show which files have the most issues
echo ""
echo "=== Files with most issues ==="
golangci-lint run -E cyclop ./... 2>&1 | \
  grep "calculated cyclomatic complexity" | \
  cut -d: -f1 | \
  sort | uniq -c | \
  sort -rn | \
  head -10