#!/bin/sh
# Builds the frontend and the bblv binaries for Windows and Linux into dist/.
# Usage: ./build.sh            (VERSION=1.0.0 ./build.sh to set the version)
set -eu
cd "$(dirname "$0")"

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"

(cd web && npm ci --prefer-offline && npm run build)

mkdir -p dist
for target in windows/amd64 linux/amd64 linux/arm64; do
	os=${target%/*}
	arch=${target#*/}
	out="dist/bblv-$os-$arch"
	if [ "$os" = windows ]; then
		out="$out.exe"
	fi
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$out" ./cmd/bblv
	echo "built $out ($VERSION)"
done
