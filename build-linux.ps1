# Cross-compile a portable, fully-static linux/amd64 x-ui binary from Windows.
# No gcc / WSL / Docker needed: the sqlite driver is pure-Go (CGO_ENABLED=0),
# so the result runs on ANY Linux x86_64 regardless of glibc version.
#
# Usage:   powershell -ExecutionPolicy Bypass -File build-linux.ps1
# Output:  .\x-ui-linux-amd64

$env:GOOS = "linux"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"

$out = Join-Path $PSScriptRoot "x-ui-linux-amd64"
Write-Host "Building linux/amd64 (static, CGO off) ..."
go build -ldflags "-w -s" -o $out (Join-Path $PSScriptRoot "main.go")

if ($LASTEXITCODE -eq 0) {
    $mb = [math]::Round((Get-Item $out).Length / 1MB, 1)
    Write-Host "OK -> $out  ($mb MB)"
    Write-Host ""
    Write-Host "Deploy (replace SERVER):"
    Write-Host "  ssh root@SERVER 'systemctl stop x-ui'"
    Write-Host "  scp `"$out`" root@SERVER:/usr/local/x-ui/x-ui"
    Write-Host "  ssh root@SERVER 'chmod +x /usr/local/x-ui/x-ui && systemctl restart x-ui'"
} else {
    Write-Host "Build FAILED (exit $LASTEXITCODE)"
}
