$ErrorActionPreference = 'Stop'

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$demoRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("infraflow-demo-" + [guid]::NewGuid().ToString('N'))
$output = Join-Path $demoRoot 'generated'
$infraOutput = Join-Path $demoRoot 'infra'
$multisiteOutput = Join-Path $demoRoot 'multisite'
$unsupportedOutput = Join-Path $demoRoot 'unsupported'

function Invoke-Provider {
    param([string[]] $Arguments)

    & go run ./provider/cmd @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Provider command failed (exit $LASTEXITCODE): $($Arguments -join ' ')"
    }
}

function Read-Json {
    param([string] $Path)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Missing JSON file: $Path"
    }
    $document = Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json
    return $document
}

function Assert-GeneratedManifests {
    param([string] $Root)

    $globalManifest = Read-Json (Join-Path $Root 'manifest.json')
    $globalReport = Read-Json (Join-Path $Root $globalManifest.generation_report)
    if ($globalManifest.status -ne 'succeeded' -or $globalReport.status -ne 'succeeded' -or
        $globalManifest.input_hash -ne $globalReport.input_hash -or
        $globalManifest.sites.Count -ne $globalReport.sites.Count) {
        throw 'Root manifest and generation report disagree'
    }
    $siteNames = @($globalManifest.sites | ForEach-Object { $_.site_name })
    if (@($siteNames | Select-Object -Unique).Count -ne $siteNames.Count) {
        throw 'Root manifest contains colliding site names'
    }

    foreach ($site in $globalManifest.sites) {
        $siteDirectory = Join-Path $Root $site.site_name
        $catalog = Read-Json (Join-Path $siteDirectory 'manifest.json')
        $siteManifestPath = Join-Path $siteDirectory 'site-manifest.json'
        $siteManifest = Read-Json $siteManifestPath
        $siteReport = Read-Json (Join-Path $siteDirectory 'generation-report.json')
        $reportSite = @($globalReport.sites | Where-Object { $_.site_id -eq $site.site_id })
        $manifestHash = (Get-FileHash -LiteralPath $siteManifestPath -Algorithm SHA256).Hash.ToLowerInvariant()

        if ($site.status -ne 'succeeded' -or $catalog.status -ne 'succeeded' -or
            $siteManifest.status -ne 'succeeded' -or $siteReport.status -ne 'succeeded' -or
            $catalog.site_id -ne $site.site_id -or $siteManifest.site_id -ne $site.site_id -or
            $siteManifest.site_name -ne $site.site_name -or
            $catalog.input_hash -ne $siteManifest.input_hash -or
            $siteReport.input_hash -ne $siteManifest.input_hash -or
            $manifestHash -ne $site.site_manifest_sha256 -or $reportSite.Count -ne 1 -or
            $reportSite[0].status -ne $site.status -or
            $reportSite[0].artifact_count -ne $site.artifact_count -or
            $reportSite[0].site_manifest_path -ne $site.site_manifest_path -or
            $reportSite[0].site_manifest_sha256 -ne $site.site_manifest_sha256 -or
            $catalog.artifacts.Count -ne $site.artifact_count -or
            $siteManifest.artifacts.Count + 1 -ne $site.artifact_count) {
            throw "Manifest/report mismatch for site $($site.site_name)"
        }

        $detailsByPath = @{}
        foreach ($artifact in $siteManifest.artifacts) {
            $detailsByPath[$artifact.path] = $artifact
        }
        $catalogPaths = @{}
        foreach ($artifact in $catalog.artifacts) {
            if ($catalogPaths.ContainsKey($artifact.path)) {
                throw "Duplicate catalog artifact path: $($artifact.path)"
            }
            $catalogPaths[$artifact.path] = $true
            if (-not $artifact.path.StartsWith($site.site_name + '/', [StringComparison]::OrdinalIgnoreCase)) {
                throw "Artifact is outside its site directory: $($artifact.path)"
            }
            $file = Join-Path $Root $artifact.path.Replace('/', '\')
            if (-not (Test-Path -LiteralPath $file -PathType Leaf)) {
                throw "Manifest artifact is missing: $($artifact.path)"
            }
            $hash = (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLowerInvariant()
            if ($hash -ne $artifact.output_hash) {
                throw "Catalog SHA-256 mismatch: $($artifact.path)"
            }
            if ($artifact.path -ne $site.site_manifest_path) {
                $detail = $detailsByPath[$artifact.path]
                if ($null -eq $detail -or $detail.sha256 -ne $hash -or
                    (Get-Item -LiteralPath $file).Length -ne $detail.size_bytes) {
                    throw "Site-manifest size/SHA-256 mismatch: $($artifact.path)"
                }
            }
        }

        $expectedPaths = @{}
        foreach ($artifact in $catalog.artifacts) {
            $expectedPaths[$artifact.path] = $true
        }
        $expectedPaths[$site.site_name + '/manifest.json'] = $true
        $diskPaths = @{}
        Get-ChildItem -LiteralPath $siteDirectory -Recurse -File | ForEach-Object {
            $relativePath = $_.FullName.Substring($Root.Length + 1).Replace('\', '/')
            $diskPaths[$relativePath] = $true
        }
        if ($catalogPaths.Count -ne $catalog.artifacts.Count -or
            $diskPaths.Count -ne $expectedPaths.Count) {
            throw "Site file/catalog count mismatch for $($site.site_name)"
        }
        foreach ($path in $diskPaths.Keys) {
            if (-not $expectedPaths.ContainsKey($path)) {
                throw "Uncataloged site file: $path"
            }
        }
        Write-Output "$($site.site_name): PASS ($($site.artifact_count) cataloged artifacts)"
    }

    $rootFiles = @(Get-ChildItem -LiteralPath $Root -File | ForEach-Object { $_.Name } | Sort-Object)
    if (($rootFiles -join ',') -ne 'generation-report.json,manifest.json') {
        throw "Unexpected files at generation root: $($rootFiles -join ', ')"
    }
}

Push-Location $repositoryRoot
try {
    Write-Output '1. Validate and plan the provisioning input'
    Invoke-Provider -Arguments @('validate', '-f', 'examples/provisioning-generation.yaml')
    Invoke-Provider -Arguments @('plan', '-f', 'examples/provisioning-generation.yaml')

    Write-Output "2. Generate into fresh output: $output"
    Invoke-Provider -Arguments @('generate-all', '-f', 'examples/provisioning-generation.yaml', '-out', $output)
    Assert-GeneratedManifests $output

    Write-Output 'Validate and generate the standard infrastructure example'
    Invoke-Provider -Arguments @('validate', '-f', 'examples/infra.yaml')
    Invoke-Provider -Arguments @('generate-all', '-f', 'examples/infra.yaml', '-out', $infraOutput)
    Assert-GeneratedManifests $infraOutput

    Write-Output 'Validate and generate the multisite example'
    Invoke-Provider -Arguments @('validate', '-f', 'examples/multisite-preparation.yaml')
    Invoke-Provider -Arguments @('generate-all', '-f', 'examples/multisite-preparation.yaml', '-out', $multisiteOutput)
    Assert-GeneratedManifests $multisiteOutput

    $ciscoPath = Join-Path $output 'cisco-render-demo\native\device-cisco-r1.cfg'
    $routerOSPath = Join-Path $output 'routeros-auto-rsc-demo\native\device-mikrotik-r1.auto.rsc'
    foreach ($path in @($ciscoPath, $routerOSPath)) {
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            throw "Expected native artifact is missing: $path"
        }
    }
    foreach ($site in @('cisco-render-demo', 'routeros-auto-rsc-demo')) {
        if (Test-Path -LiteralPath (Join-Path $output "$site\bootstrap")) {
            throw "Bootstrap artifacts were generated without an explicitly enabled service: $site"
        }
    }

    Write-Output '3. Generated tree'
    Get-ChildItem -LiteralPath $output -Recurse -File |
        ForEach-Object { $_.FullName.Substring($output.Length + 1) }

    Write-Output 'Generation report'
    Get-Content -LiteralPath (Join-Path $output 'generation-report.json')
    Write-Output '4. Cisco native configuration (rendered text only)'
    Get-Content -LiteralPath $ciscoPath
    Write-Output '5. MikroTik RouterOS .auto.rsc (rendered text only)'
    Get-Content -LiteralPath $routerOSPath
    Write-Output '6. Native and site manifests'
    Get-Content -LiteralPath (Join-Path (Split-Path -Parent $ciscoPath) 'native-manifest.json')
    Get-Content -LiteralPath (Join-Path $output 'cisco-render-demo\site-manifest.json')
    Write-Output '7. Verified artifact SHA-256'
    Get-FileHash -LiteralPath $routerOSPath -Algorithm SHA256

    Write-Output '8. Unsupported OSPF intent must fail without publishing output'
    & go run ./provider/cmd generate-all -f examples/network-deployment-unsupported-ospf.yaml -out $unsupportedOutput
    if ($LASTEXITCODE -eq 0) {
        throw 'Unsupported OSPF intent unexpectedly succeeded'
    }
    if (Test-Path -LiteralPath $unsupportedOutput) {
        throw 'Unsupported intent left a published output directory'
    }
    Write-Output "PASS: unsupported intent rejected (exit $LASTEXITCODE); no output published"

    Write-Output "Demo output retained at: $demoRoot"
    Write-Output 'Generated files are not automatically delivered, executed, or verified on devices.'
}
finally {
    Pop-Location
}
