#!/usr/bin/env bash
#
# Check for secrets using trufflehog
#

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

if ! command -v trufflehog >/dev/null 2>&1; then
    echo -e "${YELLOW}trufflehog not installed${NC}"
    echo "Install with: brew install trufflehog"
    echo "Or: make tools"
    exit 0
fi

echo "Scanning for secrets with trufflehog..."

# Scan the filesystem, excluding common false positive paths
RESULT=$(trufflehog filesystem . \
    --no-update \
    --exclude-paths=.trufflehog-ignore \
    --exclude-detectors=PrivateKey \
    --fail \
    2>&1) || true

if echo "$RESULT" | grep -q "Found verified result"; then
    echo -e "${RED}Verified secrets found!${NC}"
    echo ""
    echo "$RESULT"
    exit 1
elif echo "$RESULT" | grep -q "Found unverified result"; then
    echo -e "${YELLOW}Potential secrets found (unverified):${NC}"
    echo ""
    echo "$RESULT" | head -50
    echo ""
    echo "Review findings. Add false positives to .trufflehog-ignore"
    exit 0
else
    echo -e "${GREEN}No secrets found.${NC}"
    exit 0
fi
