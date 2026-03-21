- [Contributing to JSM](#contributing-to-jsm)
  - [Development Environment](#development-environment)
    - [Prerequisites](#prerequisites)
    - [Windows Compatibility](#windows-compatibility)
    - [Getting Started](#getting-started)
    - [Repository Structure](#repository-structure)
  - [Architecture](#architecture)
    - [Dependency Injection](#dependency-injection)
    - [Logging](#logging)
    - [Testing](#testing)
      - [Useful Commands](#useful-commands)
    - [Development Workflow](#development-workflow)
  - [Test Coverage](#test-coverage)
    - [Race Detection](#race-detection)
  - [Build Commands](#build-commands)
  - [Commit Messages](#commit-messages)
  - [Semantic Versioning (SemVer)](#semantic-versioning-semver)
  - [Publishing a Release](#publishing-a-release)
    - [Steps to Release](#steps-to-release)
    - [Cleaning Up Test Releases](#cleaning-up-test-releases)
  - [VS Code Setup](#vs-code-setup)


# Contributing to JSM

Welcome! This document provides guidelines for developing and contributing to the JSON Schema Manager (JSM) project.

## Development Environment

### Prerequisites

- **Go**: Version 1.25 or higher is required.
    - **macOS/Linux**: Follow instructions at [go.dev/doc/install](https://go.dev/doc/install).
    - **Windows**: Run `winget install -e --id GoLang.Go` or download from [go.dev/dl](https://go.dev/dl/).
    - **Note on Race Detection (Windows)**: The `test-race` command on Windows requires a C compiler (e.g., Mingw-w64). If not present, `win-make.ps1` will fallback to standard tests.

- **GO bin configured**: To run tools like `lefthook` directly, ensure Go's binary directory is in your `PATH`.

    > **Mac/linux users**: Run this command to automatically update your `~/.zshrc`:
    > ```bash
    > grep -q 'go env GOPATH' ~/.zshrc || echo 'export PATH=$PATH:$(go env GOPATH)/bin' >> ~/.zshrc && source ~/.zshrc
    > ```
    >
    > **Windows users**: The Go installer adds the compiler to your PATH, but you must also add your user's Go bin folder (usually `%USERPROFILE%\go\bin`) to run development tools.
    > 
    > You can run this PowerShell command to add it permanently for your user:
    > ```powershell
    > $goBin = Join-Path $Home "go\bin"
    > [Environment]::SetEnvironmentVariable("Path", [Environment]::GetEnvironmentVariable("Path", "User") + ";$goBin", "User")
    > ```
    > *Note: You must restart VS Code/Terminal after running this.*

- **Make**: Used for automating build and test tasks (macOS/Linux). Windows users can use the provided PowerShell script.

### Windows Compatibility

This project is cross-platform. To ensure a smooth experience for all contributors, we enforce some rules:

- **Line Endings**: We use `LF` (Unix-style) line endings. This is enforced via `.gitattributes` and `.editorconfig`. Git will automatically handle conversions on checkout/commit, but please ensure your editor is configured to use `LF`.
- **Build Commands**: Since `make` is not natively available on Windows, we provide a PowerShell script `.\win-make.ps1` that mirrors the `Makefile` commands.

### Getting Started

Assuming you have the prerequisites in place, run `make setup` (macOS/Linux) or `.\win-make.ps1 setup` (Windows) to install development tools.

### Repository Structure

- **`cmd/jsm`**: The entry point for the CLI.
- **`internal`**: Internal application logic and command implementations.
- **`schemas`**: Directory used for testing and examples of JSON schemas.

## Architecture

This project follows an **Explicit Dependency Injection** pattern using the "Assembly Point" approach.

### Dependency Injection
- **No Globals**: Avoid using global variables for state or services.
- **Constructor Functions**: Every service and command should have a `New...` function that accepts its dependencies as interfaces.
- **Assembly Point**: `cmd/jsm/main.go` is the only place where the system is "wired" together. It initializes infrastructure (config, logger), then services, then the CLI.

### Logging
We use the standard library `log/slog` for structured logging.
- **Injection**: Inject `*slog.Logger` into any service that needs it.
- **Verbosity**: The CLI includes a `--verbose` flag which dynamically updates the `slog.LevelVar` initialized in `main.go`.

### Testing

For prettier test output, we use `gotestsum`, but go test will work fine too.

- **Unit Tests**: Place `_test.go` files alongside the code they test.
- **Mocking**: Use local mock implementations of interfaces to test commands and services in isolation.
- **Commands**: Test Cobra commands by creating them with mocks and redirecting their output buffers.
- **Test Runner**: We use `gotestsum` for colorful, enhanced test output. It's automatically installed via `make setup` and used by `make test`.

Code is formatted on save.

#### Useful Commands

```bash
# Run all tests (uses gotestsum if available)
make test          # macOS/Linux
.\win-make.ps1 test # Windows

# Run tests with race detection (important for concurrent code)
make test-race     # macOS/Linux
.\win-make.ps1 test-race # Windows

# Run tests with coverage
make test-cover    # macOS/Linux
.\win-make.ps1 test-cover # Windows

# View coverage in browser
make cover-html    # macOS/Linux
.\win-make.ps1 cover-html # Windows

# Run the linter
make lint          # macOS/Linux
.\win-make.ps1 lint # Windows
```
See [Build Commands](#build-commands) below, or just checkout the `Makefile` or `win-make.ps1`.

### Development Workflow

1.  **Clone the repository**.
2.  **Set up development environment**: `make setup` (installs hooks and linters).
3.  **Make changes**.
4.  **Run tests**: `make test`
4.  **Lint your code**: `make lint`
5.  **Build locally**: `make build`
6.  **Commit your changes**: `git add . && git commit -m "<description>"` - See [Commit Messages](#commit-messages).
7.  **Push your changes**: `git push`
8.  **Create a pull request**.

## Test Coverage

We expect 100% test coverage.

### Race Detection

Go's built-in race detector helps catch data races in concurrent code. Run `make test-race` before submitting PRs that involve concurrency:

```bash
make test-race
```

**Important**: Tests that mock package-level variables (e.g., `canonicalPath`, `absFunc`) must **not** use `t.Parallel()` because concurrent modification of globals causes race conditions.

## Build Commands

| Command              | Description                                                                               |
| :------------------- | :---------------------------------------------------------------------------------------- |
| `make setup`         | Installs development tools (lefthook, golangci-lint, goreleaser, staticcheck, gotestsum). |
| `make build`         | Compiles the binary to `bin/jsm` with version injection.                                  |
| `make run`           | Runs the application directly.                                                            |
| `make test`          | Runs all unit tests with colorful output (via gotestsum).                                 |
| `make test-race`     | Runs all unit tests with Go's race detector enabled.                                      |
| `make test-cover`    | Runs tests with coverage report.                                                          |
| `make cover-html`    | Opens test coverage report in browser.                                                    |
| `make lint`          | Runs the linter.                                                                          |
| `make snapshot`      | Runs a GoReleaser snapshot build to verify release configuration locally.                 |
| `make release-check` | Validates the GoReleaser configuration file.                                              |
| `make clean`         | Removes all build and distribution artifacts.                                             |

## Commit Messages

JSM uses [Conventional Commits](https://www.conventionalcommits.org/) to automatically generate helpful changelogs for each release. 

### Commit Format

All commit messages **must** identify the domain being changed using a mandatory scope:

`<type>(<scope>): <description>`

### Release Note Policy

To keep our user-facing release notes clean and relevant:
-   **Core Changes**: Use the **`(jsm)`** scope for all features and fixes to the JSON Schema Manager itself (e.g., `feat(jsm): add validate command`). **Only `(jsm)` scoped commits will appear in the official release notes.**
-   **Other Changes**: Use descriptive scopes like **`(cicd)`**, **`(docs)`**, or **`(refactor)`** for improvements that should not appear in the user-facing changelog (e.g., `fix(cicd): update github token`).

This ensures that our users only see product-relevant changes in the changelog.

Common types:
- **feat**: A new JSM feature (e.g., `feat(jsm): add validate command`)
- **fix**: A JSM bug fix (e.g., `fix(jsm): resolve broken --verbose flag`)
- **docs**: Documentation only changes (e.g., `docs: update changelog guide`)
- **build**: Changes that affect the build system or external dependencies
- **ci**: Changes to our CI configuration files and scripts
- **chore**: Other changes that don't modify src or test files

> [!IMPORTANT]
> **Breaking Changes**: If a change breaks backwards compatibility, add a `!` after the type or scope to highlight it (e.g., `feat(jsm)!: remove deprecated flag`). This will be automatically flagged in the release notes.
> 
> *Note for zsh users*: If you use `!` in a double-quoted string, you may get an "illegal modifier" error. Use **single quotes** for your commit message instead:
> `git commit -m 'feat(jsm)!: breaking change description'`

## Semantic Versioning (SemVer)

JSM follows [Semantic Versioning 2.0.0](https://semver.org/). It is critical to choose the correct version increment based on the impact of your changes:

- **MAJOR** version when you make incompatible API changes (e.g., changing `jsm` flag behavior in a breaking way).
- **MINOR** version when you add functionality in a backwards compatible manner (e.g., adding a new command).
- **PATCH** version when you make backwards compatible bug fixes or trivial changes (e.g., fixing a typo in help output).

## Publishing a Release

Releases are automated using **GoReleaser**.

### Steps to Release

1.  **Ensure all changes are committed and pushed** to the `main` branch.
2.  **Decide on the new version** (e.g., `v1.2.3`) based on SemVer rules.
3.  **Create a git tag**:
    ```bash
    git tag -a v1.2.3 -m "Release v1.2.3"
    ```
4.  **Push the tag**:
    ```bash
    git push origin v1.2.3
    ```
5.  **Monitor the Release**: The GitHub Actions workflow will automatically pick up the tag, build the binaries, and create the release with artifacts. You can monitor the progress in the "Actions" tab of your repository.
    - If you need to run it manually (not recommended for official releases), you can use `goreleaser release --clean` locally (requires a `GITHUB_TOKEN`).

### Cleaning Up Test Releases

If you need to remove a release and tag (e.g., after a failed test release):

1.  **Delete the release on GitHub**: Go to the "Releases" section of the repository and delete the release.
2.  **Delete the tag locally**:
    ```bash
    git tag -d vX.Y.Z
    ```
3.  **Delete the tag on GitHub**:
    ```bash
    git push --delete origin vX.Y.Z
    ```

## VS Code Setup

The project includes a `.vscode/settings.json` file. We recommend the following:
- Enable "Format on Save".
- Use `goimports` for organizing imports.
- Enable `golangci-lint` integration.
