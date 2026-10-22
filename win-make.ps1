param (
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidateSet("build", "run", "clean", "test", "test-race", "test-cover", "test-race-coverage", "cover-html", "lint", "fmt", "docs", "check-models", "check-apis", "snapshot", "release-check", "setup")]
    [string]$Target
)

$hasRaceSupport = $true
if ($env:CGO_ENABLED -eq "0" -or ($null -eq (Get-Command gcc -ErrorAction SilentlyContinue))) {
    $hasRaceSupport = $false
}

function Invoke-GoTest {
    param([string[]]$extraArgs)
    $allArgs = @("./...", "-v")
    if ($hasRaceSupport) {
        $allArgs = @("-race") + $allArgs
    }
    else {
        Write-Host "⚠️  Warning: Race detection requires a C compiler (gcc) and CGO_ENABLED=1. Running without -race." -ForegroundColor Yellow
    }
    if ($extraArgs) { $allArgs = $extraArgs + $allArgs }
    go run scripts/tester/main.go @allArgs
}

switch ($Target) {
    "build" {
        go run scripts/build/main.go
    }
    "run" {
        go run cmd/jsm/main.go
    }
    "clean" {
        go run scripts/clean/main.go
        go clean
    }
    "test" {
        go run scripts/tester/main.go ./... -v
    }
    "test-race" {
        Invoke-GoTest
    }
    "test-cover" {
        Invoke-GoTest @("--summary", "-count=1")
    }
    "test-race-coverage" {
        Invoke-GoTest @("--test-race-coverage", "-count=1")
    }
    "cover-html" {
        Invoke-GoTest @("--browser", "-count=1")
    }
    "lint" {
        go run scripts/lint/main.go
    }
    "fmt" {
        go run scripts/fmt/main.go
    }
    "docs" {
        go run scripts/docs/main.go
    }
    "check-models" {
        go run scripts/models/main.go
    }
    "check-apis" {
        go run scripts/apis/main.go
    }
    "snapshot" {
        go run scripts/snapshot/main.go
    }
    "release-check" {
        go run scripts/release_check/main.go
    }
    "setup" {
        go run scripts/setup/main.go
    }
}
if ($Target -ne "setup" -and $LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}
