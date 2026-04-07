#!/usr/bin/env pwsh
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Schema,
    [string]$ConfigPath = "goframe.yaml",
    [string]$GoFrameBin = "goframe",
    [string]$MigrationsDir = "migrations",
    [string]$BaselineName = "baseline_existing_schema",
    [string]$ModelsOutput = "internal/models/legacy_models.go",
    [string]$ModelsPackage = "models",
    [string]$Tables = "",
    [string]$Exclude = "",
    [switch]$SkipImport,
    [switch]$SkipInspectdb,
    [switch]$SkipBaseline,
    [switch]$SkipStamp
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Normalize-Slug {
    param([string]$Raw)
    if ([string]::IsNullOrWhiteSpace($Raw)) {
        return "baseline_existing_schema"
    }
    $out = $Raw.ToLowerInvariant()
    $out = [regex]::Replace($out, "[^a-z0-9_]+", "_")
    $out = $out.Trim("_")
    if ([string]::IsNullOrWhiteSpace($out)) {
        return "baseline_existing_schema"
    }
    return $out
}

function Quote-SqlString {
    param([string]$Raw)
    if ($null -eq $Raw) {
        return ""
    }
    return $Raw.Replace("'", "''")
}

if ($SkipBaseline.IsPresent -and -not $SkipStamp.IsPresent) {
    throw "--SkipBaseline cannot be combined with baseline stamping. Add --SkipStamp too or keep baseline creation enabled."
}

if (-not (Test-Path -LiteralPath $Schema -PathType Leaf)) {
    throw "Schema file not found: $Schema"
}

if (-not (Test-Path -LiteralPath $ConfigPath -PathType Leaf)) {
    throw "Config file not found: $ConfigPath"
}

try {
    $null = Get-Command $GoFrameBin -ErrorAction Stop
}
catch {
    throw "goframe executable not found: $GoFrameBin. Install it or pass --GoFrameBin <path>."
}

$baselineSlug = Normalize-Slug -Raw $BaselineName

Write-Host "==> Existing schema import automation (PowerShell)"
Write-Host "    schema:      $Schema"
Write-Host "    config:      $ConfigPath"
Write-Host "    goframe:     $GoFrameBin"
Write-Host "    migrations:  $MigrationsDir"
Write-Host "    baseline:    $baselineSlug"
Write-Host "    models out:  $ModelsOutput"

if (-not $SkipImport.IsPresent) {
    Write-Host "==> Step 1/4: Import schema SQL into configured database"
    Get-Content -LiteralPath $Schema -Raw | & $GoFrameBin shell --config $ConfigPath
}
else {
    Write-Host "==> Step 1/4: Skipped (--SkipImport)"
}

if (-not $SkipInspectdb.IsPresent) {
    Write-Host "==> Step 2/4: Generate models with inspectdb"
    $inspectArgs = @("inspectdb", "--config", $ConfigPath, "--package", $ModelsPackage, "--output", $ModelsOutput)
    if (-not [string]::IsNullOrWhiteSpace($Tables)) {
        $inspectArgs += @("--tables", $Tables)
    }
    if (-not [string]::IsNullOrWhiteSpace($Exclude)) {
        $inspectArgs += @("--exclude", $Exclude)
    }
    & $GoFrameBin @inspectArgs
}
else {
    Write-Host "==> Step 2/4: Skipped (--SkipInspectdb)"
}

$migrationId = ""
$migrationUp = ""
$migrationDown = ""

if (-not $SkipBaseline.IsPresent) {
    Write-Host "==> Step 3/4: Create baseline migration and copy schema SQL"
    New-Item -Path $MigrationsDir -ItemType Directory -Force | Out-Null
    & $GoFrameBin migrate --config $ConfigPath --migrations $MigrationsDir create $baselineSlug

    $migrationUpObj = Get-ChildItem -Path $MigrationsDir -Filter "*_$baselineSlug.up.sql" -File |
        Sort-Object Name |
        Select-Object -Last 1

    if ($null -eq $migrationUpObj) {
        throw "Could not resolve generated baseline .up.sql for $baselineSlug"
    }

    $migrationUp = $migrationUpObj.FullName
    $migrationId = [System.IO.Path]::GetFileNameWithoutExtension($migrationUp)
    $migrationDown = Join-Path $MigrationsDir "$migrationId.down.sql"

    $schemaContent = Get-Content -LiteralPath $Schema -Raw
    $utcNow = [DateTime]::UtcNow.ToString("yyyy-MM-ddTHH:mm:ssZ")
    $upContent = @(
        "-- Baseline schema imported from $Schema"
        "-- Generated at (UTC): $utcNow"
        "-- NOTE: This baseline is intended for new environments."
        ""
        $schemaContent
        ""
    ) -join [Environment]::NewLine
    Set-Content -LiteralPath $migrationUp -Value $upContent -NoNewline

    if (Test-Path -LiteralPath $migrationDown -PathType Leaf) {
        $downContent = @(
            "-- Baseline down migration for $migrationId"
            "-- WARNING: automatic rollback of imported schema is intentionally not provided."
            "-- Add manual rollback SQL for your database engine if your process requires it."
            ""
            "-- Write your SQL here"
            ""
        ) -join [Environment]::NewLine
        Set-Content -LiteralPath $migrationDown -Value $downContent -NoNewline
    }

    Write-Host "    baseline up:   $migrationUp"
    Write-Host "    baseline down: $migrationDown"
}
else {
    Write-Host "==> Step 3/4: Skipped (--SkipBaseline)"
}

if (-not $SkipStamp.IsPresent) {
    Write-Host "==> Step 4/4: Mark baseline migration as applied"
    if ([string]::IsNullOrWhiteSpace($migrationId)) {
        throw "Baseline migration id is empty; cannot stamp applied state."
    }

    $migrationIdSql = Quote-SqlString -Raw $migrationId
    $appliedAtSql = Quote-SqlString -Raw ([DateTime]::UtcNow.ToString("yyyy-MM-ddTHH:mm:ssZ"))
    $stampSql = @"
CREATE TABLE IF NOT EXISTS goframe_schema_migrations (
  id VARCHAR(255) PRIMARY KEY,
  applied_at TEXT NOT NULL
);
DELETE FROM goframe_schema_migrations WHERE id = '$migrationIdSql';
INSERT INTO goframe_schema_migrations (id, applied_at) VALUES ('$migrationIdSql', '$appliedAtSql');
"@
    & $GoFrameBin shell --config $ConfigPath --command $stampSql
}
else {
    Write-Host "==> Step 4/4: Skipped (--SkipStamp)"
}

Write-Host "==> Completed"
Write-Host "    models file: $ModelsOutput"
if (-not [string]::IsNullOrWhiteSpace($migrationId)) {
    Write-Host "    baseline id: $migrationId"
}
