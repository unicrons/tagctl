#!/usr/bin/env bash
#
# Scans files for secrets with trufflehog and fails on any finding, verified
# or not. Usage: check-secrets.sh [file...] (default: every file git would
# commit). Paths matching a regular expression in .trufflehog-ignore are skipped.
#

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

IGNORE_FILE=.trufflehog-ignore

# make tools installs into the Go bin directory, which is not always on PATH.
find_tool() {
    local dir
    dir=$(go env GOBIN 2>/dev/null)
    [ -n "$dir" ] || dir="$(go env GOPATH 2>/dev/null)/bin"
    if [ -x "$dir/$1" ]; then
        echo "$dir/$1"
    else
        command -v "$1"
    fi
}

printf 'Checking for secrets (trufflehog)... '

TRUFFLEHOG=$(find_tool trufflehog)
if [ -z "$TRUFFLEHOG" ]; then
    printf '%bSKIPPED%b (trufflehog not installed, run: make tools)\n' "$YELLOW" "$NC"
    exit 0
fi

files=()
if [ "$#" -gt 0 ]; then
    files=("$@")
else
    while IFS= read -r -d '' file; do
        files+=("$file")
    done < <(git ls-files -z --cached --others --exclude-standard)
fi

patterns=()
if [ -f "$IGNORE_FILE" ]; then
    while IFS= read -r line || [ -n "$line" ]; do
        [[ $line =~ ^[[:space:]]*(#|$) ]] || patterns+=("$line")
    done <"$IGNORE_FILE"
fi

# trufflehog applies --exclude-paths only while walking directories, never to
# paths passed as arguments, so the ignore list is applied here.
targets=()
for file in "${files[@]}"; do
    [ -f "$file" ] || continue
    for pattern in "${patterns[@]}"; do
        [[ $file =~ $pattern ]] && continue 2
    done
    targets+=("$file")
done

if [ "${#targets[@]}" -eq 0 ]; then
    printf '%bOK%b (no files to scan)\n' "$GREEN" "$NC"
    exit 0
fi

output=$("$TRUFFLEHOG" filesystem --no-update --fail "${targets[@]}" 2>&1)
case $? in
    0)
        printf '%bOK%b\n' "$GREEN" "$NC"
        ;;
    183)
        printf '%bFAILED%b\n\n%s\n\n' "$RED" "$NC" "$output"
        echo "Remove the secret. For a false positive, add a regular expression"
        echo "matching the file path to $IGNORE_FILE."
        exit 1
        ;;
    *)
        printf '%bFAILED%b (trufflehog did not complete)\n\n%s\n' "$RED" "$NC" "$output"
        exit 1
        ;;
esac
