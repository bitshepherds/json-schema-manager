# Referring to JSM Schemas from AsyncAPI and OpenAPI <!-- omit in toc -->

- [Refer to the collapsed schemas in dist](#refer-to-the-collapsed-schemas-in-dist)
- [AsyncAPI](#asyncapi)
- [OpenAPI](#openapi)
- [Checking the documents in CI](#checking-the-documents-in-ci)
- [Why not canonical URLs?](#why-not-canonical-urls)

JSM versions the schemas of messages and request and response bodies. [AsyncAPI](https://www.asyncapi.com/) and [OpenAPI](https://www.openapis.org/) documents describe the channels and operations which carry them. Point those documents at JSM's schemas, so there is one definition of each body, tested by JSM, and tools such as the AsyncAPI CLI, Redocly or oasdiff can check the rest.

## Refer to the collapsed schemas in dist

`jsm build-dist <env> --collapse` writes each schema rendered for an environment into `dist/<env>/private` or `dist/<env>/public`, at the root of the git repository. With `--collapse`, each file is self-contained: the other JSM schemas it refers to are inlined into its `$defs`. So an AsyncAPI or OpenAPI document can `$ref` the file by a relative path, and a tool reading the document needs nothing else, and no network.

```bash
jsm build-dist prod --all --collapse
```

In a deployment pipeline, leave out `--all`, so that only the schemas changed since the last deployment are built, after `check-changes` and the tests pass.

## AsyncAPI

A message's `payload` refers to the schema, with its `schemaFormat`:

```yaml
asyncapi: 3.1.0
info:
  title: Customer events
  version: 1.0.0
channels:
  customerRegistered:
    address: customer.registered
    messages:
      customerRegistered:
        $ref: '#/components/messages/customerRegistered'
components:
  messages:
    customerRegistered:
      payload:
        schemaFormat: application/schema+json;version=draft-07
        schema:
          $ref: '../../dist/prod/private/domain-c_domain-c-a_customer_1_0_0.schema.json'
```

Set the `schemaFormat` version to the schema's `$schema` draft, such as `draft-07` or `2020-12`.

## OpenAPI

A request or response body's `schema` refers to it. OpenAPI 3.1 uses JSON Schema 2020-12, which reads a draft-07 schema with `$schema` set correctly:

```yaml
openapi: 3.1.0
info:
  title: Customer API
  version: 1.0.0
paths:
  /customers/{customerNumber}:
    get:
      responses:
        '200':
          description: The customer
          content:
            application/json:
              schema:
                $ref: '../../dist/prod/private/domain-c_domain-c-a_customer_1_0_0.schema.json'
```

## Checking the documents in CI

Build `dist` first, then run the tools:

```bash
jsm build-dist prod --all --collapse
npx @asyncapi/cli validate customer-events.asyncapi.yaml
npx @redocly/cli lint customer.openapi.yaml
```

Both fail, exiting non-zero, if a `$ref` does not resolve, for example because the document names a schema version which is not in the registry.

The documents name exact schema versions, so a new minor version of a body means a new `$ref` in the document. Since providers move to a new version first, update the provider's document when it starts sending the new version, and let `jsm check-consumer` tell each consumer when its version falls behind. See [Checking a consumer's schema versions](./developing-schemas.md#checking-a-consumers-schema-versions).

## Why not canonical URLs?

A document could instead `$ref` a schema's canonical URL, such as `https://json-schemas.myorg.io/customer_1_0_0.schema.json`. That works once the schema is deployed and served, but tools then fetch it over the network, and a pull request which adds a new version cannot be checked until that version is deployed. A relative path to `dist` can be checked in the same pipeline which builds it.

The examples on this page are in [examples/apis](../examples/apis), and `make check-apis` checks them against the demo registry in CI.
