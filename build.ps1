<#
.SYNOPSIS

.EXAMPLE
    .\build.ps1 test
    .\build.ps1 build -Version 1.2.0
    .\build.ps1 package -Version 1.2.0 -UpdateRepo owner/dofirst -UpdateKey <открытый ключ>
#>
param(
    [Parameter(Position = 0)]
    [ValidateSet('build', 'test', 'lint', 'run', 'package', 'smoke', 'live', 'clean')]
    [string]$Target = 'build',

    # Версия, которая попадет в .exe и в имена файлов
    [string]$Version = '0.1.0',
    [string]$UpdateRepo = '',
    # Открытый ключ Ed25519 для проверки подписи обновлений (см. tools/sign) 
    [string]$UpdateKey = '',
    [string[]]$Arch = @('amd64', 'arm64')
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

if (-not $UpdateKey -and (Test-Path 'build/update.pub')) {
    $UpdateKey = (Get-Content 'build/update.pub' -Raw).Trim()
}

$go = (Get-Command go -ErrorAction SilentlyContinue).Source
if (-not $go) { $go = 'C:\Program Files\Go\bin\go.exe' }
if (-not (Test-Path $go)) { throw 'Go не найден. Установите Go 1.26+ (winget install GoLang.Go).' }
$gofmt = Join-Path (Split-Path $go) 'gofmt.exe'

function Invoke-Go {
    & $go @args
    if ($LASTEXITCODE -ne 0) { throw "go $($args -join ' ') завершился с кодом $LASTEXITCODE" }
}

function Invoke-Build {
    Invoke-Go run ./tools/mkres -version $Version -out cmd/dofirst
    New-Item -ItemType Directory -Force dist | Out-Null
    $pkg = 'dofirst/internal/config'
    $ldflags = "-s -w -H windowsgui -X $pkg.Version=$Version -X $pkg.UpdateRepo=$UpdateRepo -X $pkg.UpdateKey=$UpdateKey"
    foreach ($a in $Arch) {
        $env:GOOS = 'windows'; $env:GOARCH = $a; $env:CGO_ENABLED = '0'
        try {
            Invoke-Go build -trimpath -ldflags $ldflags -o "dist/dofirst-windows-$a.exe" ./cmd/dofirst
        } finally {
            Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
        }
        Write-Host "dist/dofirst-windows-$a.exe"
    }
}

function Invoke-Lint {
    $unformatted = & $gofmt -l cmd internal tools
    if ($unformatted) { throw "Не отформатировано gofmt:`n$($unformatted -join "`n")" }
    Invoke-Go vet ./...
    $linter = Get-Command golangci-lint -ErrorAction SilentlyContinue
    if ($linter) {
        & $linter.Source run ./...
        if ($LASTEXITCODE -ne 0) { throw 'golangci-lint нашёл замечания' }
    } else {
        Write-Warning 'golangci-lint не установлен — выполнены только gofmt и go vet.'
    }
}

switch ($Target) {
    'build' { Invoke-Build }

    'test' {
        Invoke-Go vet ./...
        Invoke-Go test -cover ./...
    }

    'lint' { Invoke-Lint }

    'run' { Invoke-Go run ./cmd/dofirst --data-dir (Join-Path $PSScriptRoot '.devdata') }

    'smoke' {
        $env:DOFIRST_UI_SMOKE = '1'
        try { Invoke-Go test -count=1 ./internal/ui/ } finally { Remove-Item Env:DOFIRST_UI_SMOKE }
    }

    'live' {
        $env:DOFIRST_LIVE = '1'
        try { Invoke-Go test -count=1 -v -run TestLiveBlocking ./internal/platform/win/ } finally { Remove-Item Env:DOFIRST_LIVE }
    }

    'package' {
        Invoke-Build
        foreach ($a in $Arch) {
            $exe = "dist/dofirst-windows-$a.exe"
            if ($env:DOFIRST_SIGN_KEY) { Invoke-Go run ./tools/sign sign $exe }

            $stage = "dist/stage-$a"
            Remove-Item -Recurse -Force $stage -ErrorAction SilentlyContinue
            New-Item -ItemType Directory -Force $stage | Out-Null
            Copy-Item $exe "$stage/DoFirstTo.exe"
            Copy-Item README.md $stage
            $zip = "dist/DoFirstTo-$Version-windows-$a.zip"
            Remove-Item $zip -ErrorAction SilentlyContinue
            Compress-Archive -Path "$stage/*" -DestinationPath $zip
            Remove-Item -Recurse -Force $stage
            Write-Host $zip

            $nsis = Get-Command makensis -ErrorAction SilentlyContinue
            if ($nsis) {
                & $nsis.Source /V2 "/DVERSION=$Version" "/DARCH=$a" build/installer/dofirst.nsi
                if ($LASTEXITCODE -ne 0) { Write-Warning "makensis завершился с ошибкой — установщик для $a не собран (zip готов)." }
            } else {
                Write-Warning "NSIS не установлен — установщик для $a не собран (zip готов)."
            }
        }
    }

    'clean' {
        Remove-Item -Recurse -Force dist, bin, .devdata -ErrorAction SilentlyContinue
        Remove-Item cmd/dofirst/*.syso -ErrorAction SilentlyContinue
    }
}
