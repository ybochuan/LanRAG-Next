param(
    [Parameter(Mandatory)][string] $OutputDirectory,
    [string] $WebRelease,
    [string] $LlamaRuntimeZip,
    [switch] $PortableOnly
)

$ErrorActionPreference = 'Stop'

function Invoke-BuildStep {
    param([string] $Program, [string[]] $Arguments)
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Program failed with exit code $LASTEXITCODE."
    }
}

$root = Split-Path -Parent $PSScriptRoot
$output = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($OutputDirectory)
if (Test-Path -LiteralPath $output) {
    throw 'Choose a new Windows release output directory.'
}
$webReleasePath = if ($WebRelease) { (Resolve-Path -LiteralPath $WebRelease).Path }
$llamaZip = if ($LlamaRuntimeZip) { (Resolve-Path -LiteralPath $LlamaRuntimeZip).Path }
$savedEnvironment = @{}
foreach ($name in @('GOWORK', 'CGO_ENABLED', 'CGO_CFLAGS', 'EDITION', 'VITE_FRONTEND_COMMIT', 'GOFLAGS')) {
    $savedEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}
Push-Location $root
try {
    $revision = (Invoke-BuildStep git @('rev-parse', 'HEAD')).Trim()
    $status = @(Invoke-BuildStep git @('status', '--short', '--untracked-files=all'))
    $app = Join-Path $output 'LanRAG Lite'
    $resources = Join-Path $app 'resources'
    $include = Join-Path $output 'build-include'
    New-Item -ItemType Directory -Path $resources, $include | Out-Null
    $llamaRelease = 'b12431'
    if (!$llamaZip) {
        $llamaZip = Join-Path $include "llama-$llamaRelease-win-vulkan-x64.zip"
        Invoke-BuildStep curl.exe @('-fLsS', '--retry', '2', '--max-time', '120',
            '-H', 'Accept: application/octet-stream', '-o', $llamaZip,
            'https://api.github.com/repos/ggml-org/llama.cpp/releases/assets/754223153?download=1')
    }
    $llamaExtract = Join-Path $include 'llama'
    Expand-Archive -LiteralPath $llamaZip -DestinationPath $llamaExtract
    $llamaRuntime = Join-Path $resources 'llama-runtime'
    New-Item -ItemType Directory -Path $llamaRuntime | Out-Null
    foreach ($file in Get-ChildItem -LiteralPath $llamaExtract -File) {
        if ($file.Extension -eq '.dll' -or $file.Name -eq 'llama-server.exe' -or $file.Name -like 'LICENSE*') {
            Copy-Item -LiteralPath $file.FullName -Destination $llamaRuntime
        }
    }
    $llamaServer = Join-Path $llamaRuntime 'llama-server.exe'
    $llamaVersion = Invoke-BuildStep $llamaServer @('--version') 2>&1
    if (($llamaVersion -join ' ') -notmatch '\bbuild 12431\b') {
        throw "Expected llama.cpp $llamaRelease in the Windows runtime."
    }
    $llamaLicense = Invoke-RestMethod -TimeoutSec 30 -Uri "https://api.github.com/repos/ggml-org/llama.cpp/contents/LICENSE?ref=$llamaRelease"
    [IO.File]::WriteAllBytes((Join-Path $llamaRuntime 'LICENSE-llama.cpp'), [Convert]::FromBase64String($llamaLicense.content))
    $env:GOWORK = 'off'
    $env:GOFLAGS = '-mod=readonly'
    $env:EDITION = 'lite'
    $env:VITE_FRONTEND_COMMIT = $revision

    if ($webReleasePath) {
        $webRevision = (Get-Content -Raw -LiteralPath (Join-Path $webReleasePath 'REVISION')).Trim()
        if ($webRevision -ne $revision) {
            throw 'The supplied web release is from a different commit.'
        }
        Invoke-BuildStep git @('diff', '--quiet', 'HEAD', '--', 'frontend')
        $web = Join-Path $webReleasePath 'web'
    } else {
        Push-Location (Join-Path $root 'frontend')
        try {
            Invoke-BuildStep npm.cmd @('ci', '--no-audit', '--no-fund')
            Invoke-BuildStep npm.cmd @('run', 'build')
        } finally {
            Pop-Location
        }
        $web = Join-Path $root 'frontend/dist'
    }
    Get-Item -LiteralPath (Join-Path $web 'index.html') | Out-Null
    Copy-Item -LiteralPath $web -Destination (Join-Path $resources 'web') -Recurse
    Add-Content -LiteralPath (Join-Path $resources 'web/config.js') -Encoding ascii -Value '
window.__RUNTIME_CONFIG__ = { ...window.__RUNTIME_CONFIG__, LANRAG_LITE_PRODUCT: true };'
    Copy-Item -LiteralPath (Join-Path $root 'config') -Destination $resources -Recurse
    New-Item -ItemType Directory -Path (Join-Path $resources 'migrations') | Out-Null
    Copy-Item -LiteralPath (Join-Path $root 'migrations/sqlite') -Destination (Join-Path $resources 'migrations') -Recurse
    Copy-Item -LiteralPath (Join-Path $root 'deploy/windows-lite.env.example') -Destination (Join-Path $resources '.env.lite.example')
    Invoke-BuildStep go @('mod', 'download', 'github.com/yanyiwu/gojieba')
    $jiebaModule = (Invoke-BuildStep go @('list', '-m', '-f', '{{.Dir}}', 'github.com/yanyiwu/gojieba')).Trim()
    Copy-Item -LiteralPath (Join-Path $jiebaModule 'deps/cppjieba/dict') -Destination (Join-Path $resources 'jieba') -Recurse
    foreach ($item in @('LICENSE', 'THIRD_PARTY_NOTICES.md', 'licenses')) {
        Copy-Item -LiteralPath (Join-Path $root $item) -Destination $app -Recurse
    }
    Copy-Item -LiteralPath (Join-Path $jiebaModule 'LICENSE') -Destination (Join-Path $app 'licenses/gojieba-MIT.txt')
    Copy-Item -LiteralPath (Join-Path $jiebaModule 'deps/cppjieba/LICENSE') -Destination (Join-Path $app 'licenses/cppjieba-MIT.txt')
    Set-Content -LiteralPath (Join-Path $resources 'REVISION') -Value $revision -Encoding ascii
    Set-Content -LiteralPath (Join-Path $resources 'SOURCE_STATUS') -Value ($status -join [Environment]::NewLine) -Encoding utf8

    $env:CGO_ENABLED = '0'
    Invoke-BuildStep go @('build', '-trimpath', '-ldflags=-H=windowsgui', '-o', (Join-Path $app 'LanRAG Lite.exe'), './cmd/desktop-launcher')
    Push-Location (Join-Path $root 'extensions')
    try {
        Invoke-BuildStep go @('build', '-trimpath', '-ldflags=-H=windowsgui', '-o', (Join-Path $app 'extension-host.exe'), './cmd/extension-host')
        Invoke-BuildStep go @('build', '-trimpath', '-ldflags=-H=windowsgui', '-o', (Join-Path $app 'model-supervisor.exe'), './cmd/model-supervisor')
    } finally {
        Pop-Location
    }
    $env:CGO_ENABLED = '1'
    Invoke-BuildStep go @('mod', 'download', 'github.com/mattn/go-sqlite3')
    $sqliteModule = (Invoke-BuildStep go @('list', '-m', '-f', '{{.Dir}}', 'github.com/mattn/go-sqlite3')).Trim()
    Copy-Item -LiteralPath (Join-Path $sqliteModule 'sqlite3-binding.h') -Destination (Join-Path $include 'sqlite3.h')
    $includeSlash = $include.Replace('\', '/')
    $env:CGO_CFLAGS = "-I `"$includeSlash`" -Wno-deprecated-declarations"
    $coreModule = 'github.com/Tencent/WeKnora/internal/handler'
    $ldflags = "-H=windowsgui -X $coreModule.Edition=lite -X $coreModule.Version=$(Get-Content -LiteralPath VERSION) -X $coreModule.CommitID=$revision -X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn"
    Invoke-BuildStep go @('build', '-trimpath', '-tags', 'desktop,sqlite_fts5,production', '-ldflags', $ldflags,
        '-o', (Join-Path $app 'LanRAG Desktop.exe'), './cmd/desktop')
    if (!$PortableOnly) {
        $installer = Join-Path $output 'LanRAG-Lite-windows-amd64-setup.exe'
        Invoke-BuildStep makensis @("-DPAYLOAD_DIR=$app", "-DINSTALLER_OUT=$installer", (Join-Path $root 'deploy/windows-lite.nsi'))
    }
    Write-Output "Windows Lite output: $output"
} finally {
    Pop-Location
    foreach ($name in $savedEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable($name, $savedEnvironment[$name], 'Process')
    }
}
