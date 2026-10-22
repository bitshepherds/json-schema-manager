# Generating Typed Models from JSM Schemas <!-- omit in toc -->

- [Generate from rendered, collapsed schemas](#generate-from-rendered-collapsed-schemas)
- [One schema](#one-schema)
  - [TypeScript](#typescript)
  - [Go, and other languages](#go-and-other-languages)
- [A whole environment](#a-whole-environment)
- [Naming the generated types](#naming-the-generated-types)
- [Unknown properties](#unknown-properties)
- [How this guide is tested](#how-this-guide-is-tested)

Services often want typed models, such as TypeScript interfaces or Go structs, as well as validators. Generating both from the registry keeps providers and consumers on one definition of each document.

## Generate from rendered, collapsed schemas

Never generate from the source files in the registry. They contain templates, such as `"$id": "{{ ID }}"` and `"$ref": "{{ JSM ... }}"`, which code generators do not understand.

Instead, generate from a rendered schema with `--collapse`. Rendering fills in the templates. Collapsing inlines the other JSM schemas a schema refers to into its `$defs`, so the generator needs nothing but the one file. JSM checks that the collapsed schema compiles, and treats the schema's test documents as the original does, before it writes it.

## One schema

```bash
jsm render-schema domain-c_domain-c-a_customer_1_0_0 --collapse > customer.schema.json
```

### TypeScript

With [json-schema-to-typescript](https://github.com/bcherny/json-schema-to-typescript):

```bash
npx json-schema-to-typescript -i customer.schema.json -o customer.ts
```

This gives an interface for the customer, and one for each schema it refers to:

```ts
export interface DomainCCustomer {
  customerNumber: string;
  person: PersonSchema;
  tier: "bronze" | "silver" | "gold";
  [k: string]: unknown;
}
```

### Go, and other languages

[quicktype](https://quicktype.io/) generates Go, and many other languages, from JSON Schema:

```bash
npx quicktype --src-lang schema --lang go --package customer --top-level Customer \
  -o customer/customer.go customer.schema.json
```

```go
type Customer struct {
	CustomerNumber string `json:"customerNumber"`
	Person         Person `json:"person"`
	Tier           Tier   `json:"tier"`
}
```

Use `--lang` to choose another language, such as `csharp`, `kotlin`, `python`, `rust` or `swift`.

## A whole environment

`jsm build-dist` with `--collapse` renders and collapses every schema for an environment, into `dist/<env>` at the root of the git repository. Generate from those files:

```bash
jsm build-dist prod --all --collapse
npx json-schema-to-typescript -i 'dist/prod/private/*.schema.json' -o models/
```

Public schemas, those with `"x-public": true`, are in `dist/prod/public`.

In a deployment pipeline, leave out `--all`, so only the schemas changed since the last deployment are built, after `check-changes` and the tests pass.

## Naming the generated types

Generators name a type after the schema's `title`, or, without one, after its `$id`, which gives long names such as `HttpsJsonSchemasInternalExampleComDomainCDomainCACustomer_1_0_0SchemaJson`. Give each schema a `title` to choose its name: the demo customer schema has `"title": "DomainCCustomer"`. quicktype's `--top-level` names the top-level type.

## Unknown properties

json-schema-to-typescript adds an index signature, `[k: string]: unknown`, to each object which allows additional properties, which is the JSON Schema default. That suits how JSM expects consumers to behave: ignore the properties they do not know, so that providers can add properties in a minor version. See [Compatibility runs one way](./developing-schemas.md#compatibility-runs-one-way-providers-first).

Do not set `"additionalProperties": false` just to get tighter types, since it means a new property is a breaking change.

## How this guide is tested

`make check-models` runs this guide's commands against the [demo registry](../demo-registry), whose customer schema refers to two other schemas, and checks that the generated TypeScript compiles with `tsc --strict`, and the generated Go with `go vet`. It needs Node.js.
