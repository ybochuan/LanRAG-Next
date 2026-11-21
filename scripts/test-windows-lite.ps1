param(
    [string] $ProfileDirectory = (Join-Path $env:LOCALAPPDATA 'LanRAG Lite')
)

$ErrorActionPreference = 'Stop'
$address = (Get-Content -Raw -LiteralPath (Join-Path $ProfileDirectory 'core.url')).Trim()
if ($address -notmatch '^http://127\.0\.0\.1:[0-9]+$') {
    throw 'The desktop Core address is not a local loopback origin.'
}
$core = Invoke-RestMethod -Uri "$address/health" -TimeoutSec 5
$hostURL = 'http://127.0.0.1:8090'
$ready = Invoke-RestMethod -Uri "$hostURL/readyz" -TimeoutSec 5
if ($core.status -ne 'ok' -or $ready.status -ne 'ready' -or $ready.scope -ne 'local') {
    throw 'The Core and worker Host are not ready.'
}
$state = Get-Content -Raw -LiteralPath (Join-Path $ProfileDirectory 'agentrag-desktop.json') | ConvertFrom-Json
if (!$state.api_key_id -or !$state.knowledge_base_ids) {
    throw 'Create an empty local knowledge base before running the governance probe.'
}
foreach ($name in @('weknora.db', 'agentrag_ext.db')) {
    Get-Item -LiteralPath (Join-Path $ProfileDirectory $name) | Out-Null
}
$headers = @{ 'X-API-Key' = $state.api_key; 'X-Tenant-ID' = [string]$state.tenant_id }
$list = Invoke-RestMethod -Uri "$address/api/v1/knowledge-bases" -Headers $headers -TimeoutSec 5
$expected = ($state.knowledge_base_ids | Sort-Object) -join ','
$actual = ($list.data | Where-Object { $_.tenant_id -eq $state.tenant_id } | ForEach-Object { $_.id } | Sort-Object) -join ','
if (!$list.success -or $actual -ne $expected) {
    throw 'The machine credential does not match its configured scope.'
}
$request = @{
    request_id = 'windows-lite-probe'
    session_id = 'windows-lite-probe'
    mode = 'rag'
    tenant_id = $state.tenant_id
    knowledge_base_ids = @($state.knowledge_base_ids[0])
    knowledge_ids = @('00000000-0000-0000-0000-000000000099')
} | ConvertTo-Json -Compress
$decision = Invoke-RestMethod -Method Post -Uri "$hostURL/bridge/before-retrieve" -ContentType 'application/json' `
    -Headers @{ 'X-AgentRAG-Bridge-Version' = '1' } -Body $request -TimeoutSec 5
if ($decision.deny_all -ne $true) {
    throw 'The worker did not deny an unknown document.'
}
$mcpRequest = @{ jsonrpc = '2.0'; id = 1; method = 'tools/list' } | ConvertTo-Json -Compress
$anonymous = Invoke-WebRequest -Method Post -Uri "$hostURL/mcp" -ContentType 'application/json' `
    -Body $mcpRequest -TimeoutSec 5 -SkipHttpErrorCheck
if ($anonymous.StatusCode -ne 401) {
    throw 'The installed Host did not reject an unauthenticated MCP request.'
}
$mcp = Invoke-RestMethod -Method Post -Uri "$hostURL/mcp" -ContentType 'application/json' `
    -Headers @{ Authorization = 'Bearer ' + $state.management_key } -Body $mcpRequest -TimeoutSec 5
$toolNames = @($mcp.result.tools | ForEach-Object { $_.name })
if ($toolNames -notcontains 'query_related_docs' -or $toolNames -notcontains 'search_archived_documents') {
    throw 'The installed Host MCP catalog is missing a bundled tool.'
}
[pscustomobject]@{
    Core = $core.status
    Host = $ready.status
    KnowledgeBases = @($list.data).Count
    ScopedMachineKeyID = $state.api_key_id
    UnknownDocumentDenied = $decision.deny_all
    MCPTools              = $toolNames -join ', '
    AnonymousMCPDenied    = $anonymous.StatusCode -eq 401
}
