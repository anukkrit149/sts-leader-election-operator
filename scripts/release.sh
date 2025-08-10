#!/bin/bash

# Release script for StatefulSet Leader Election Operator
# Usage: ./scripts/release.sh <version>
# Example: ./scripts/release.sh 0.2.0

set -e

if [ $# -ne 1 ]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 0.2.0"
    exit 1
fi

NEW_VERSION=$1

# Validate version format (semantic versioning)
if ! [[ $NEW_VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "Error: Version must be in semantic versioning format (e.g., 1.0.0)"
    exit 1
fi

echo "Preparing release for version $NEW_VERSION"

# Update VERSION file
echo "$NEW_VERSION" > VERSION

# Generate manifests
echo "Generating manifests..."
make manifests generate

# Build and test
echo "Building and testing..."
make build
make test

# Generate installation manifests
echo "Generating installation manifests..."
make build-installer
make build-installer-production

echo "Release preparation complete for version $NEW_VERSION"
echo ""
echo "Next steps:"
echo "1. Review the changes: git diff"
echo "2. Commit the changes: git add -A && git commit -m 'Release v$NEW_VERSION'"
echo "3. Create a git tag: git tag v$NEW_VERSION"
echo "4. Push the changes: git push origin main --tags"
echo "5. Build and push the container image:"
echo "   make docker-build"
echo "   make docker-push"
echo ""
echo "Generated files:"
echo "- dist/install.yaml (development installation)"
echo "- dist/install-production.yaml (production installation)"