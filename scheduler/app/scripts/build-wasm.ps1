param(
  [string]$Workflow = ""
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$WorkflowsDir = Join-Path $ScriptDir "..\workflows"
$StaticRootDir = Join-Path $ScriptDir "..\static"

if (!(Test-Path $WorkflowsDir)) {
  throw "workflows directory missing: $WorkflowsDir"
}

if (!(Test-Path $StaticRootDir)) {
  New-Item -ItemType Directory -Path $StaticRootDir -Force | Out-Null
}

function Build-Wasm([string]$SourcePath, [string]$OutputPath) {

  if (!(Test-Path $SourcePath)) {
    throw "source file missing: $SourcePath"
  }

  $outputName = [System.IO.Path]::GetFileName($OutputPath)
  Write-Host "Building $outputName from $([System.IO.Path]::GetFileName($SourcePath)) ..."
  & emcc $SourcePath `
    -O3 `
    -s STANDALONE_WASM=1 `
    "-Wl,--no-entry" `
    "-Wl,--export=run" `
    -o $outputPath

  if ($LASTEXITCODE -ne 0) {
    throw "emcc failed for $SourcePath with exit code $LASTEXITCODE"
  }
}

$workflowDirs = @()
if ([string]::IsNullOrWhiteSpace($Workflow)) {
  $workflowDirs = @(Get-ChildItem -Path $WorkflowsDir -Directory | Sort-Object Name)
} else {
  $workflowPath = Join-Path $WorkflowsDir $Workflow
  if (!(Test-Path $workflowPath)) {
    throw "workflow directory not found: $workflowPath"
  }
  $workflowDirs = @(Get-Item $workflowPath)
}

if (@($workflowDirs).Count -eq 0) {
  throw "no workflow directories found in $WorkflowsDir"
}

foreach ($workflowDir in $workflowDirs) {
  $sources = Get-ChildItem -Path $workflowDir.FullName -Filter *.cpp -File | Sort-Object Name
  if ($sources.Count -eq 0) {
    Write-Warning "Skipping $($workflowDir.Name): no .cpp files found"
    continue
  }

  $workflowOutputDir = Join-Path $StaticRootDir $workflowDir.Name
  if (!(Test-Path $workflowOutputDir)) {
    New-Item -ItemType Directory -Path $workflowOutputDir -Force | Out-Null
  }

  Write-Host "Building workflow: $($workflowDir.Name)"
  foreach ($source in $sources) {
    $outputName = "$([System.IO.Path]::GetFileNameWithoutExtension($source.Name)).wasm"
    $outputPath = Join-Path $workflowOutputDir $outputName
    Build-Wasm $source.FullName $outputPath
  }
}

Write-Host "Done. Artifacts:"
Get-ChildItem $StaticRootDir -Recurse -Filter *.wasm | Select-Object DirectoryName, Name, Length, LastWriteTime
