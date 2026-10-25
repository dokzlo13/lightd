#!/usr/bin/env sh
set -eu
files=$(git ls-files '*.go')
if [ -n "$files" ]; then
    unformatted=$(gofmt -l $files)
    if [ -n "$unformatted" ]; then
        printf '%s\n' "$unformatted"
        exit 1
    fi
fi
go mod verify
go vet ./...
go test -race -count=1 ./...

python3 -m unittest discover -s scripts -p "test_*.py"
