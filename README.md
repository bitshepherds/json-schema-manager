# JSON Schema Manager (JSM) <!-- omit in toc -->

[![Build Status](https://github.com/bitshepherds/json-schema-manager/actions/workflows/ci.yml/badge.svg)](https://github.com/bitshepherds/json-schema-manager/actions/workflows/ci.yml)
![Coverage Status](https://github.com/bitshepherds/json-schema-manager/wiki/coverage.svg#badge)
[![Go Report Card](https://goreportcard.com/badge/github.com/bitshepherds/json-schema-manager)](https://goreportcard.com/report/github.com/bitshepherds/json-schema-manager)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://www.apache.org/licenses/LICENSE-2.0)

JSON Schemas can be used as powerful contracts to validate data within an organisation - whether it be objects in memory, data at rest in databases or object storage, or data moving between between components across a network via API calls or event streams.

JSON Schema Manager (JSM) is a CLI framework that provides firm foundations, and a robust workflow for developing, testing, deploying, and evolving JSON Schemas over time, especially in large enterprise environments.

This repo contains the implementation of the `jsm` CLI tool.

> [!TIP]
>
> If you'd like to learn about or use JSM, see [jsm-demo](https://github.com/bitshepherds/jsm-demo).

## Usage

```bash
go install github.com/bitshepherds/json-schema-manager/cmd/jsm@latest

jsm create-registry ./my-registry                  # a registry, ideally the root of a git repo
export JSM_REGISTRY_ROOT_DIR="$PWD/my-registry"
jsm create-schema "customer/b2c-customer"          # version 1.0.0, with pass/ and fail/ test directories
jsm validate all                                   # test every schema, and that no version breaks an earlier one
jsm create-schema-version customer_b2c-customer_1_0_0 minor
```

In CI, run the checks on every pull request. Each exits non-zero when a test or check fails:

```yaml
- uses: actions/checkout@v7
  with:
    fetch-depth: 0 # check-changes and build-dist compare against the last deployment's tag
- run: jsm validate all --continue-on-error
- run: jsm check-changes prod
```

See the user docs:

- [Developing Schemas](./user-docs/developing-schemas.md): the workflow, CI, exit codes, and how JSM checks compatibility.
- [Registry](./user-docs/registry.md): how a registry is laid out and configured.
- [Generating Typed Models](./user-docs/generating-models.md): TypeScript, Go and other models from the registry's schemas.
- [Referring to JSM Schemas from AsyncAPI and OpenAPI](./user-docs/api-documents.md): message and body schemas for API documents.
- [Command reference](./user-docs/reference/jsm.md): every command and flag, generated from the CLI.

## Contributing

If you would like to contribute to the development of JSM, see [CONTRIBUTING](./CONTRIBUTING.md) and the [CODE_OF_CONDUCT](./CODE_OF_CONDUCT.md).
