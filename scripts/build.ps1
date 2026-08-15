$ErrorActionPreference = 'Stop'

Set-Location (Join-Path $PSScriptRoot '..')
go build -trimpath -buildvcs=false ./...
exit $LASTEXITCODE
