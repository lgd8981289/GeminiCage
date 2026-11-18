#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p _output/bin
tmp=$(mktemp -d _output/gemini-core.XXXXXXXX)
trap 'rm -rf "$tmp"' EXIT
mod=github.com/Code-Hex/vz/v3
go mod download "$mod"
vz=$(go list -m -f '{{.Dir}}' "$mod")
cp -R "$vz" "$tmp/vz"
chmod -R u+w "$tmp/vz"
cat >"$tmp/vz/cage_view_object_darwin.go" <<'EOF'
//go:build darwin && cgo
package vz
import ("unsafe"; "github.com/Code-Hex/vz/v3/internal/objc")
func (v *VirtualMachine) CageViewObject() unsafe.Pointer {return objc.Ptr(v)}
EOF
cp go.mod "$tmp/fork.mod"
cp go.sum "$tmp/fork.sum"
go mod edit -modfile="$tmp/fork.mod" -replace "$mod=./$tmp/vz"
export MACOSX_DEPLOYMENT_TARGET=14.0
tags=yq_noxml,yq_nolua,yq_noini,yq_nohcl,yq_notoml,yq_noprops,yq_nocsv,yq_nouri,yq_nobase64,yq_nosh,yq_noshell
go build -modfile="$tmp/fork.mod" -trimpath -tags "$tags" -ldflags "-s -w -X github.com/sylvinus/agent-vm/internal/version.Version=$(cat VERSION) -X github.com/lima-vm/lima/v2/pkg/version.Version=$(cat internal/limaembed/lima-version)" -o _output/bin/cage ./cmd/cage
codesign -f -s - --entitlements third_party/lima/vz.entitlements _output/bin/cage
_output/bin/cage verify-web-assets
_output/bin/cage verify-macos-guest
_output/bin/cage verify-vm-assets
