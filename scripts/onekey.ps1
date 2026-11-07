# AgentRAG one-key launcher (Windows half).
#
# The Windows side owns the services that only exist here: Docker Desktop, the
# MinerU parser, the model supervisor and the two llama-server instances. It then
# hands off to start-all.sh inside WSL, which owns Core and the Extension Host.
#
# Everything is idempotent: a healthy service is reused, never duplicated, and a
# second run should finish in seconds.
[CmdletBinding()]
param(
    [switch]$DryRun,
    [switch]$SkipDocker,
    [switch]$SkipMinerU,
    [switch]$NoNetworkFix,
    [switch]$FixNetwork,
    [switch]$Strict,
    [switch]$Stop,
    [string]$WslDistro = 'Ubuntu',
    [Parameter(Position = 0)][string]$Command = ''
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root
$LogDir = Join-Path $Root 'data\logs'
New-Item -ItemType Directory -Force -Path $LogDir | Out-Null
$MineruExe = 'D:\mineru-venv\Scripts\mineru-api.exe'
$script:Problems = @()
# A failed mirrored switch makes WSL worse (nat -> none) until Windows reboots,
# so the failure is remembered and the switch is not retried every launch.
$NetworkMarker = Join-Path $Root 'data\.wsl-mirrored-failed'

function Step([string]$Message) { Write-Host "[..] $Message" -ForegroundColor Cyan }
function Ok([string]$Message) { Write-Host "[ ok] $Message" -ForegroundColor Green }
function Warn([string]$Message) {
    Write-Host "[ !!] $Message" -ForegroundColor Yellow
    $script:Problems += $Message
}
function Die([string]$Message) { Write-Host "[err] $Message" -ForegroundColor Red; exit 1 }

function Test-Http([string]$Url, [int]$TimeoutSec = 3) {
    try {
        $response = Invoke-WebRequest -Uri $Url -UseBasicParsing -TimeoutSec $TimeoutSec
        return $response.StatusCode -eq 200
    } catch { return $false }
}

function Wait-Http([string]$Url, [int]$Seconds, [string]$Name) {
    $deadline = (Get-Date).AddSeconds($Seconds)
    while ((Get-Date) -lt $deadline) {
        if (Test-Http $Url 3) { Ok "$Name ready"; return $true }
        Start-Sleep -Milliseconds 500
    }
    return $false
}

function Invoke-Wsl([string]$Command) {
    $result = Invoke-Native { wsl.exe -d $WslDistro -- bash -lc $Command }
    if ($result.Code -ne 0) { throw "WSL command failed ($($result.Code)): $Command`n$($result.Output -join "`n")" }
    return ($result.Output -join "`n")
}

function Get-SupervisorHeaders([string]$Token) {
    return @{
        'X-API-Key'                           = $Token
        'X-AgentRAG-Model-Supervisor-Version' = '1'
    }
}

# Windows PowerShell 5.1 turns a native command's stderr into a terminating
# error while ErrorActionPreference is Stop, which is exactly what docker/wsl
# write when they fail. Every native call goes through here so a failed command
# reports its exit code instead of aborting the launcher.
function Invoke-Native([scriptblock]$Action) {
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $output = & $Action 2>&1
        return @{ Code = $LASTEXITCODE; Output = @($output) }
    } finally { $ErrorActionPreference = $previous }
}

# --------------------------------------------------------------------------
# WSL plumbing: the launcher state lives in the repo and is produced by the WSL
# half, so prepare/plan run there and the result is read back here.
# --------------------------------------------------------------------------
# wsl.exe can print boot warnings on stderr even when wslpath succeeds; taking
# the last absolute path keeps those warnings out of every later bash command.
$WslRoot = ''
try {
    $wslPathOutput = Invoke-Wsl "wslpath -a '$Root'"
    $WslRoot = (($wslPathOutput -split "`r?`n") | Where-Object { $_ -match '^/' } | Select-Object -Last 1)
    if ($WslRoot) { $WslRoot = $WslRoot.Trim() }
} catch {
    if (-not $Stop) { throw }
    Warn "WSL is unavailable: $($_.Exception.Message)"
}
if (-not $WslRoot -and -not $Stop) { Die "cannot resolve the WSL path for $Root" }
if (-not $WslRoot) { Warn "cannot resolve the WSL path for $Root; WSL-side cleanup will be skipped" }

# Quick subcommands run entirely in the WSL half; they do not need the plan and
# must not prepare credentials just to print status.
if ($Command) {
    $name = $Command.ToLowerInvariant()
    if ($name -notin @('status', 'logs', 'doctor', 'token', 'mcp-key')) {
        Die "unknown command '$Command'; use status|logs|doctor|token|mcp-key"
    }
    if (-not $WslRoot) { Die "WSL is not available; cannot run '$name'" }
    & wsl.exe -d $WslDistro -- bash "$WslRoot/start-all.sh" $name
    exit $LASTEXITCODE
}

$Transcript = Join-Path $LogDir ("onekey-" + (Get-Date).ToString('yyyyMMdd-HHmmss') + ".log")
try { Start-Transcript -Path $Transcript -Append | Out-Null } catch { }

# --------------------------------------------------------------------------
# Stop mode: deliberately does not read the plan or prepare credentials. It is
# the escape hatch when WSL networking is broken and the normal path cannot run.
# --------------------------------------------------------------------------
if ($Stop) {
    $StatePath = Join-Path $Root 'data\agentrag-launcher.json'
    $Token = ''
    $SupervisorAddr = '127.0.0.1:8092'
    $Models = @()
    if (Test-Path $StatePath) {
        try {
            $State = Get-Content $StatePath -Raw | ConvertFrom-Json
            $Token = $State.MODEL_SUPERVISOR_TOKEN
            if ($State.MODEL_SUPERVISOR_ADDR) { $SupervisorAddr = $State.MODEL_SUPERVISOR_ADDR }
            if ($State.MODEL_SERVICES) {
                $Models = if ($State.MODEL_SERVICES -is [string]) { @($State.MODEL_SERVICES | ConvertFrom-Json) } else { @($State.MODEL_SERVICES) }
            }
        } catch { Warn "cannot read $StatePath; model API stop will be skipped: $($_.Exception.Message)" }
    }

    if ($WslRoot) {
        Step "Stopping Core and Extension Host in WSL"
        try {
            $wslStop = Invoke-Wsl "cd '$WslRoot' && bash ./start-all.sh stop"
            if ($wslStop) { $wslStop -split "`n" | ForEach-Object { Write-Host "      $_" } }
            Ok "WSL-side Core/Host stopped"
        } catch { Warn "WSL stop failed: $($_.Exception.Message)" }
    } else {
        Warn "skipping WSL-side Core/Host cleanup (WSL unavailable)"
    }

    if (Test-Http "http://$SupervisorAddr/healthz" 2) {
        Step "Stopping models through the supervisor"
        foreach ($model in $Models) {
            try {
                Invoke-RestMethod -Method Post -Uri "http://$SupervisorAddr/extensions/models/stop" `
                    -Headers (Get-SupervisorHeaders $Token) -ContentType 'application/json' `
                    -Body (@{ id = $model.id } | ConvertTo-Json) -TimeoutSec 30 | Out-Null
                Ok "stopped $($model.id)"
            } catch { Warn "stop $($model.id) failed: $($_.Exception.Message)" }
        }
    } else {
        Step "Supervisor not running; skipping model API stop"
    }

    Get-Process -Name 'model-supervisor' -ErrorAction SilentlyContinue | ForEach-Object {
        Stop-Process -Id $_.Id -Force; Ok "supervisor $($_.Id) stopped"
    }
    # The supervisor normally owns llama-server, but a crashed supervisor leaves
    # the children behind; sweep only the binary this repo started.
    $llamaBin = Join-Path $Root 'data\bin\llama-server.exe'
    Get-Process -Name 'llama-server' -ErrorAction SilentlyContinue | Where-Object {
        $_.Path -and ($_.Path -ieq $llamaBin)
    } | ForEach-Object { Stop-Process -Id $_.Id -Force; Ok "orphan llama-server $($_.Id) stopped" }

    Step "Stopping MinerU"
    Get-Process -Name 'mineru-api' -ErrorAction SilentlyContinue | ForEach-Object {
        if ($_.Path -and ($_.Path -ieq $MineruExe)) {
            Stop-Process -Id $_.Id -Force; Ok "MinerU $($_.Id) stopped"
        }
    }

    $dockerReady = $false
    if (Get-Command docker -ErrorAction SilentlyContinue) {
        try {
            # A stopped Docker Desktop makes `docker info` write to stderr; in
            # Windows PowerShell 5.1 that can surface as a script error even
            # though this is only a probe. Suppress both streams and inspect the
            # exit code so -Stop always finishes its other cleanup steps.
            $null = & cmd.exe /c 'docker info >NUL 2>&1'
            $dockerReady = ($LASTEXITCODE -eq 0)
        } catch { $dockerReady = $false }
    }
    if ($dockerReady) {
        $stopCode = 1
        $composePrevious = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        try {
            & docker.exe compose -f (Join-Path $Root 'docker-compose.yml') -f (Join-Path $Root 'docker-compose.docreader-host.yml') stop docreader 2>&1 |
                ForEach-Object { Write-Host "      $_" }
            $stopCode = $LASTEXITCODE
        } finally { $ErrorActionPreference = $composePrevious }
        if ($stopCode -eq 0) { Ok "docreader container stopped" } else { Warn "docreader stop failed" }
    } else {
        Warn "Docker is not running; skipping docreader stop (container state left unchanged)"
    }
    exit 0
}

Step "Reading launcher plan"
$planJson = Invoke-Wsl "cd '$WslRoot' && python3 extensions/scripts/launcher_config.py prepare '$WslRoot/data/onekey-private.env' >/dev/null && python3 extensions/scripts/launcher_config.py plan"
$Plan = $planJson | ConvertFrom-Json
# The WSL half re-prepares into its own run directory; this copy only existed to
# materialise the state and must not linger with the machine credential in it.
Remove-Item (Join-Path $Root 'data\onekey-private.env') -Force -ErrorAction SilentlyContinue
$StatePath = Join-Path $Root 'data\agentrag-launcher.json'
if (-not (Test-Path $StatePath)) { Die "launcher state missing: $StatePath" }
$State = Get-Content $StatePath -Raw | ConvertFrom-Json
$Token = $State.MODEL_SUPERVISOR_TOKEN
if (-not $Token) { Die "no supervisor token in $StatePath (run prepare)" }
$SupervisorAddr = if ($State.MODEL_SUPERVISOR_ADDR) { $State.MODEL_SUPERVISOR_ADDR } else { '127.0.0.1:8092' }
$SupervisorBin = Join-Path $Root ($State.MODEL_SUPERVISOR_BIN -replace '/', '\')
$Models = @($Plan.models)

Ok ("plan: core :{0}, supervisor {1}, models {2}, docker [{3}]" -f $Plan.core_port, $SupervisorAddr, (($Models | ForEach-Object { "$($_.id):$($_.port)" }) -join ' '), ($Plan.docker_services -join ','))

if ($DryRun) {
    Step "Dry run: nothing is started"
    $Plan | ConvertTo-Json -Depth 6
    exit 0
}

# --------------------------------------------------------------------------
# WSL networking: mirrored puts the Windows loopback on the WSL side too, which
# is the only mode where Core can reach the model endpoints unchanged.
# --------------------------------------------------------------------------
function Get-WslConfiguredMode {
    $file = Join-Path $env:USERPROFILE '.wslconfig'
    if (-not (Test-Path $file)) { return 'nat' }
    $match = Select-String -Path $file -Pattern '^\s*networkingMode\s*=\s*(\w+)'
    if ($match) { return $match.Matches[0].Groups[1].Value.ToLower() }
    return 'nat'
}

function Get-WslModeValue([object[]]$Output) {
    # `wsl.exe` can print a UTF-16 diagnostic before the actual mode; keep only
    # the trailing mode token so warnings and status output stay compact.
    $text = (($Output | ForEach-Object { "$_" }) -join '')
    $compact = ($text -replace '[^\w]', '').ToLower()
    foreach ($mode in @('mirrored', 'nat', 'none')) {
        if ($compact.EndsWith($mode)) { return $mode }
    }
    return ''
}

function Get-WslNetworkingMode {
    # Ask the running distro first: the config file says what was requested, not
    # what WSL actually managed to set up. Mirrored can fail at boot and fall
    # back to nat or none, and reading the file would hide exactly that.
    $live = ''
    try {
        $result = Invoke-Native { wsl.exe -d $WslDistro -- wslinfo --networking-mode }
        if ($result.Code -eq 0) { $live = Get-WslModeValue $result.Output }
    } catch { }
    if ($live) { return $live }
    return (Get-WslConfiguredMode)
}

function Wait-WslNetworkingMode([int]$Seconds = 60) {
    # `wsl --shutdown` returns before the distro is usable again. Poll the live
    # mode instead of sleeping a fixed three seconds, which is both too short on
    # a cold boot and needlessly slow when WSL comes back immediately.
    $deadline = (Get-Date).AddSeconds($Seconds)
    while ((Get-Date) -lt $deadline) {
        $result = Invoke-Native { wsl.exe -d $WslDistro -- wslinfo --networking-mode }
        $value = Get-WslModeValue $result.Output
        if ($result.Code -eq 0 -and $value) { return $value }
        Start-Sleep -Milliseconds 500
    }
    return ''
}

function Set-WslMirrored {
    $file = Join-Path $env:USERPROFILE '.wslconfig'
    $lines = @()
    if (Test-Path $file) { $lines = @(Get-Content $file -Encoding UTF8) }
    Copy-Item $file "$file.bak" -Force -ErrorAction SilentlyContinue
    $inWsl2 = $false
    $sawSection = $false
    $out = New-Object System.Collections.Generic.List[string]
    foreach ($line in $lines) {
        if ($line -match '^\s*\[(.+)\]\s*$') {
            if ($Matches[1].Trim() -eq 'wsl2') { $inWsl2 = $true; $sawSection = $true }
            else { $inWsl2 = $false }
            $out.Add($line); continue
        }
        if ($inWsl2 -and $line -match '^\s*networkingMode\s*=') { $out.Add('networkingMode=mirrored'); continue }
        if ($inWsl2 -and $line -match '^\s*autoProxy\s*=') { $out.Add('autoProxy=false'); continue }
        # dnsTunneling=true turns a mirrored boot failure into networkingMode=none
        # (WSL loses every interface, so the web UI becomes unreachable too);
        # with it off the fallback is NAT, which at least keeps the UI working.
        if ($inWsl2 -and $line -match '^\s*dnsTunneling\s*=') { $out.Add('dnsTunneling=false'); continue }
        $out.Add($line)
    }
    if (-not $sawSection) {
        if ($out.Count -gt 0 -and $out[$out.Count - 1].Trim() -ne '') { $out.Add('') }
        $out.Add('[wsl2]')
    }
    if (-not ($out | Where-Object { $_ -match '^\s*networkingMode\s*=' })) { $out.Add('networkingMode=mirrored') }
    if (-not ($out | Where-Object { $_ -match '^\s*autoProxy\s*=' })) { $out.Add('autoProxy=false') }
    # ASCII mangled any non-English comment into question marks. Write UTF-8
    # without a BOM so WSL's config parser sees the same file Windows editors do.
    $utf8 = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllLines($file, $out, $utf8)
    Ok "wrote mirrored networking to $file (backup: $file.bak)"
}

$mode = Get-WslNetworkingMode
$configured = Get-WslConfiguredMode
if ($mode -ne 'mirrored') {
    if ($NoNetworkFix) {
        Warn "WSL is in '$mode' mode; Core cannot reach Windows loopback 127.0.0.1:8081/8082/8000. Run start.cmd without -NoNetworkFix."
    } elseif ((Test-Path $NetworkMarker) -and -not $FixNetwork) {
        Warn "A previous mirrored switch failed; skipping it (running mode=$mode). Reboot Windows, then run start.cmd -FixNetwork once to restore mirrored."
    } elseif ($configured -eq 'mirrored' -and -not $FixNetwork) {
        # Requested but not achieved: switching again only costs another WSL
        # restart and fails the same way. Say what is wrong and what fixes it.
        Warn "WSL is configured for mirrored but runs '$mode' (boot-time networking failure, usually 0x8007054f). Models, MinerU and docreader stay unreachable from Core; reboot Windows and run start.cmd again."
    } else {
        Step "WSL networking is '$mode'; switching to mirrored (restarts WSL)"
        Set-WslMirrored
        Invoke-Native { wsl.exe --shutdown } | Out-Null
        $actual = Wait-WslNetworkingMode 60
        if ($actual -eq 'mirrored') {
            Remove-Item $NetworkMarker -Force -ErrorAction SilentlyContinue
            Ok "WSL restarted in mirrored mode"
        } else {
            # WSL refuses mirrored when its networking layer is unhealthy
            # (0x8007054f) and falls back, sometimes to none. Remember that so the
            # next launch does not repeat a switch that makes things worse.
            Set-Content -Path $NetworkMarker -Value ((Get-Date).ToString('o')) -Encoding ASCII
            if (-not $actual) {
                Warn "WSL did not report a networking mode within 60s after the switch; run 'wsl --status', then reboot Windows and retry start.cmd -FixNetwork."
            } else {
                Warn "WSL mirrored networking failed (running mode=$actual). Models, MinerU and docreader are unreachable from Core until mirrored is restored; reboot Windows, then run start.cmd -FixNetwork."
                if ($actual -ne 'nat') {
                    Warn "WSL has no usable network (mode=$actual): even the web UI will not be reachable."
                }
            }
        }
    }
} else {
    Ok "WSL networking already mirrored"
}

# --------------------------------------------------------------------------
# Docker Desktop + optional containers
# --------------------------------------------------------------------------
function Test-Docker {
    try { return ((Invoke-Native { docker info }).Code -eq 0) } catch { return $false }
}

$DockerWanted = (-not $SkipDocker) -and (@($Plan.docker_services).Count -gt 0)
if ($DockerWanted) {
    if (Test-Docker) {
        Ok "Docker daemon running"
    } else {
        $desktop = Join-Path $env:ProgramFiles 'Docker\Docker\Docker Desktop.exe'
        if (-not (Test-Path $desktop)) { Warn "Docker Desktop not installed; skipping containers ($($Plan.docker_services -join ','))" }
        else {
            Step "Starting Docker Desktop"
            Start-Process -FilePath $desktop -WindowStyle Hidden | Out-Null
            $deadline = (Get-Date).AddSeconds(240)
            while ((Get-Date) -lt $deadline -and -not (Test-Docker)) { Start-Sleep -Seconds 3 }
            if (Test-Docker) { Ok "Docker daemon ready" } else { Warn "Docker Desktop did not become ready in 240s" }
        }
    }
    if (Test-Docker) {
        foreach ($service in @($Plan.docker_services)) {
            $name = switch ($service) { 'docreader' { 'WeKnora-docreader' } default { $service } }
            $running = (((Invoke-Native { docker ps --filter "name=$name" --format '{{.State}}' }).Output) -join '').Trim()
            if ($running -eq 'running') { Ok "container $service already running"; continue }
            Step "Starting container $service (this may pull the image once)"
            # The host override publishes docreader's gRPC port on loopback and
            # lets the image be pinned (a mirror copy often exists when docker.io
            # is unreachable). --no-build keeps compose from building an image
            # whose base layers would need the same registry.
            $env:AGENTRAG_DOCREADER_IMAGE = if ($State.AGENTRAG_DOCREADER_IMAGE) { $State.AGENTRAG_DOCREADER_IMAGE } else { 'wechatopenai/weknora-docreader:latest' }
            $composeFiles = @('-f', (Join-Path $Root 'docker-compose.yml'), '-f', (Join-Path $Root 'docker-compose.docreader-host.yml'))
            $compose = Invoke-Native { docker compose @composeFiles up -d --wait --no-build $service }
            $compose.Output | ForEach-Object { Write-Host "      $_" }
            if ($compose.Code -eq 0) {
                Ok "container $service up"
            } else {
                $candidates = ((Invoke-Native { docker images --format '{{.Repository}}:{{.Tag}}' }).Output | Where-Object { $_ -match 'docreader' })
                if ($candidates) {
                    Warn "container $service failed; local candidate image(s): $($candidates -join ', '). Either 'docker tag <candidate> wechatopenai/weknora-docreader:latest' and rerun, or set AGENTRAG_DOCKER_SERVICES= to skip."
                } else {
                    Warn "container $service failed (registry unreachable and no local image). Set AGENTRAG_DOCKER_SERVICES= to skip."
                }
            }
        }
    }
} else {
    Step "Skipping Docker services"
}

# --------------------------------------------------------------------------
# MinerU (Windows executable, PDF parsing)
# --------------------------------------------------------------------------
if (-not $SkipMinerU) {
    if (Test-Http 'http://127.0.0.1:8000/v1/health' 2) {
        Ok "MinerU already serving on :8000"
    } elseif (-not (Test-Path $MineruExe)) {
        Warn "MinerU not installed at $MineruExe; PDF parsing stays unavailable"
    } else {
        Step "Starting MinerU"
        Start-Process -FilePath $MineruExe -ArgumentList '--host', '127.0.0.1', '--port', '8000' `
            -WindowStyle Hidden -RedirectStandardOutput (Join-Path $LogDir 'mineru.out.log') `
            -RedirectStandardError (Join-Path $LogDir 'mineru.err.log') | Out-Null
        if (-not (Wait-Http 'http://127.0.0.1:8000/v1/health' 90 'MinerU')) { Warn "MinerU did not answer on :8000 within 90s" }
    }
} else {
    Step "Skipping MinerU"
}

# --------------------------------------------------------------------------
# Model supervisor + the instances it owns
# --------------------------------------------------------------------------
$env:MODEL_SUPERVISOR_TOKEN = $Token
$env:MODEL_SUPERVISOR_ADDR = $SupervisorAddr
$env:MODEL_SUPERVISOR_WSL_DISTRO = $WslDistro
if ($State.MODEL_SUPERVISOR_STATE) { $env:MODEL_SUPERVISOR_STATE = $State.MODEL_SUPERVISOR_STATE }

if (Test-Http "http://$SupervisorAddr/healthz" 2) {
    Ok "supervisor already running on $SupervisorAddr"
} else {
    if (-not (Test-Path $SupervisorBin)) { Die "supervisor binary missing: $SupervisorBin (build it for Windows, see docs 4.6)" }
    Step "Starting model supervisor"
    Start-Process -FilePath $SupervisorBin -WorkingDirectory $Root -WindowStyle Hidden `
        -RedirectStandardOutput (Join-Path $LogDir 'supervisor.out.log') `
        -RedirectStandardError (Join-Path $LogDir 'supervisor.err.log') | Out-Null
    if (-not (Wait-Http "http://$SupervisorAddr/healthz" 20 'supervisor')) { Die "supervisor did not answer on $SupervisorAddr; see data\logs\supervisor.err.log" }
}

$headers = Get-SupervisorHeaders $Token
foreach ($model in $Models) {
    $port = [int]$model.port
    if (Test-Http "http://127.0.0.1:$port/health" 2) {
        Ok "model $($model.id) already serving on :$port"
        continue
    }
    Step "Starting model $($model.id) (:${port})"
    $body = @{
        id = $model.id; model = $model.model; task_type = $model.task_type; port = $port
        ctx_size = $model.ctx_size; threads = $model.threads
    }
    try {
        Invoke-RestMethod -Method Post -Uri "http://$SupervisorAddr/extensions/models/start" `
            -Headers $headers -ContentType 'application/json' -Body ($body | ConvertTo-Json) -TimeoutSec 60 | Out-Null
    } catch {
        $errText = "$($_.Exception.Message) $($_.ErrorDetails.Message)"
        if ($model.task_type -eq 'rerank' -and $errText -match 'capacity_exceeded|Precondition Required|428') {
            Warn "rerank GPU capacity refused; retrying CPU-only (gpu_layers=0)"
            $body.gpu_layers = 0
            try {
                Invoke-RestMethod -Method Post -Uri "http://$SupervisorAddr/extensions/models/start" `
                    -Headers $headers -ContentType 'application/json' -Body ($body | ConvertTo-Json) -TimeoutSec 60 | Out-Null
            } catch {
                Warn "supervisor refused $($model.id) on CPU: $($_.Exception.Message)"
                continue
            }
        } else {
            Warn "supervisor refused $($model.id): $($_.Exception.Message)"
            continue
        }
    }
    if (Wait-Http "http://127.0.0.1:$port/health" 120 "model $($model.id)") {
        if ($model.task_type -eq 'embedding') {
            try {
                $vector = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$port/v1/embeddings" -ContentType 'application/json' `
                    -Body (@{ input = 'launcher readiness probe' } | ConvertTo-Json) -TimeoutSec 60
                $dim = @($vector.data[0].embedding).Count
                if ($dim -gt 0) { Ok "embedding capability verified (dim=$dim)" } else { Warn "embedding returned no vector" }
            } catch { Warn "embedding capability check failed: $($_.Exception.Message)" }
        } elseif ($model.task_type -eq 'rerank') {
            try {
                $scores = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$port/v1/rerank" -ContentType 'application/json' `
                    -Body (@{ model = $model.model; query = 'capital of France'; documents = @('Paris is the capital of France', 'Bananas are yellow') } | ConvertTo-Json) -TimeoutSec 60
                if (@($scores.results).Count -ge 2) { Ok "rerank capability verified" } else { Warn "rerank returned no results" }
            } catch { Warn "rerank capability check failed: $($_.Exception.Message)" }
        }
    } else {
        Warn "model $($model.id) did not answer on :$port within 120s; check its log in the console"
    }
}

# --------------------------------------------------------------------------
# Hand off to the WSL half (Core + Extension Host + MCP registration)
# --------------------------------------------------------------------------
if ($script:Problems.Count -gt 0) {
    Write-Host "[--] degraded: $($script:Problems.Count) Windows-side service(s) unavailable:" -ForegroundColor Yellow
    $script:Problems | ForEach-Object { Write-Host "     - $_" -ForegroundColor Yellow }
    if ($Strict) { Write-Host "[err] strict mode: refusing to start Core with missing services" -ForegroundColor Red; exit 2 }
}

Step "Starting Core and Extension Host in WSL"
Write-Host "     (Ctrl+C stops Core and the Host; Windows services stay up unless you run start.cmd -Stop)"
# stderr from the WSL half is normal output (warnings), so it must not abort this
# script; the exit code is the contract.
$handoff = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
& wsl.exe -d $WslDistro -- bash "$WslRoot/start-all.sh"
$code = $LASTEXITCODE
$ErrorActionPreference = $handoff
if ($code -ne 0) { Write-Host "[err] WSL launcher exited with $code" -ForegroundColor Red }
exit $code
