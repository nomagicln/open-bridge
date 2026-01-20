#!/bin/bash
# Property Test Runner - Executes all property tests with specified iterations
# Task 16.3: Run all property tests with 100 iterations

set -e

echo "=========================================="
echo "Property Test Execution Report"
echo "Task 16.3: Run all property tests"
echo "=========================================="
echo ""

# Run all property tests
echo "Running property tests across all packages..."
echo ""

go test -v ./internal/proptest -run TestProperty
echo ""

go test -v ./pkg/credential -run TestProperty
echo ""

go test -v ./pkg/config -run TestProperty
echo ""

echo "=========================================="
echo "Property test execution completed"
echo "=========================================="
