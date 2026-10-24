#!/bin/sh
# Builds release artifacts into dist/:
#   sentrix-agent_<v>_<arch>.deb/.rpm, sentrix-server_<v>_<arch>.deb/.rpm,
#   sentrix-agent_<v>_windows_amd64.zip
# Requires Go, Node (for the dashboard) and nfpm.
#   scripts/package.sh [version]     (default: git describe)
set -eu
cd "$(dirname "$0")/.."
VERSION="${1:-$(git describe --tags --always --dirty)}"
export SENTRIX_VERSION="${VERSION#v}"
LDFLAGS="-s -w -X main.version=${VERSION} -X github.com/MerhiOPS/sentrix/internal/agent.Version=${VERSION}"

(cd web && npm ci --silent && npm run build --silent)

rm -rf dist && mkdir -p dist
for arch in amd64 arm64; do
    out="dist/linux-${arch}"
    mkdir -p "$out"
    for cmd in sentrix-agent sentrix-server; do
        CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags "$LDFLAGS" -o "$out/$cmd" "./cmd/$cmd"
    done
    for pkg in agent server; do
        # nfpm does not expand variables in every field: render the config.
        cfg="dist/nfpm-${pkg}-${arch}.yaml"
        sed -e "s/\${GOARCH}/${arch}/g" -e "s/\${SENTRIX_VERSION}/${SENTRIX_VERSION}/g" \
            "packaging/nfpm-${pkg}.yaml" > "$cfg"
        for fmt in deb rpm; do
            nfpm package --config "$cfg" --packager "$fmt" --target dist/
        done
    done
done

win="dist/windows-amd64"
mkdir -p "$win"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$win/sentrix-agent.exe" ./cmd/sentrix-agent
cp packaging/windows/README.txt "$win/"
(cd "$win" && zip -q -9 "../sentrix-agent_${SENTRIX_VERSION}_windows_amd64.zip" sentrix-agent.exe README.txt)

rm -f dist/nfpm-*.yaml
(cd dist && sha256sum ./*.deb ./*.rpm ./*.zip > SHA256SUMS)
ls -l dist
