# Developing Schemas with JSON Schema Manager <!-- omit in toc -->

- [Installation](#installation)
- [Quickstart](#quickstart)
- [Registry Root](#registry-root)
- [Commands](#commands)
  - [Help](#help)
  - [Creating a new registry](#creating-a-new-registry)
  - [Creating a completely new schema family](#creating-a-completely-new-schema-family)
  - [Creating a new version of an existing schema](#creating-a-new-version-of-an-existing-schema)
  - [Testing schemas](#testing-schemas)
  - [Rendering and building schemas](#rendering-and-building-schemas)
  - [Checking a consumer's schema versions](#checking-a-consumers-schema-versions)
- [Exit codes](#exit-codes)
- [Running JSM in CI](#running-jsm-in-ci)
  - [Test results in CI](#test-results-in-ci)
  - [Git history in CI](#git-history-in-ci)
- [Why semantic versioning?](#why-semantic-versioning)
  - [Compatibility when the provider moves first](#compatibility-when-the-provider-moves-first)
  - [Innocuous changes which are actually breaking](#innocuous-changes-which-are-actually-breaking)
    - [Adding enum values](#adding-enum-values)
    - [Relaxing a constraint](#relaxing-a-constraint)
    - [additionalProperties](#additionalproperties)

Every command and flag is listed in the [command reference](./reference/jsm.md), which is generated from the CLI itself.

## Installation

Download the latest binary for your platform from the [GitHub Releases](https://github.com/bitshepherds/json-schema-manager/releases) page.

Alternatively, if you have Go installed, you can install it directly:

```bash
go install github.com/bitshepherds/json-schema-manager/cmd/jsm@latest
```

## Quickstart

```bash
# Create a registry, and use it by default
jsm create-registry ./my-registry
export JSM_REGISTRY_ROOT_DIR="$PWD/my-registry"

# Create the first version, 1.0.0, of a schema family
jsm create-schema "customer/b2c-customer" --scenario provider-first
```

The scenario says how the family is used: see [Schema Family Scenarios](./scenarios.md#how-to-choose-a-scenario). Leave out `--scenario` and the command asks for it.

Edit `my-registry/customer/b2c-customer/1/0/0/customer_b2c-customer_1_0_0.schema.json`, add JSON documents the schema should accept to the `pass` directory next to it, and documents it should reject to the `fail` directory. Then test it:

```bash
jsm validate customer_b2c-customer_1_0_0
```

To change the schema once it is deployed, create a new version and test the whole registry, which also checks the new version does not break consumers of the old one:

```bash
jsm create-schema-version customer_b2c-customer_1_0_0 minor
jsm validate all
```

## Registry Root

The Registry Root is the root directory of the JSON Schema registry. It is the directory that contains all the JSON schemas and their associated files, and will also usually be the root directory of the local git repository where the schemas are stored.

`jsm` finds the Registry Root from, in order:

1. the `--registry` (`-r`) flag;
2. the environment variable `JSM_REGISTRY_ROOT_DIR`;
3. the current directory.

## Commands

### Help

- `jsm help` - shows help for all commands
- `jsm help <command>` - shows help for the given command

### Creating a new registry

A JSM Registry is a directory containing all the schemas in an organisation, and the tests for those schemas.
It should be the root of a git repository.

`jsm create-registry <directory>` creates the directory and writes a default configuration file, `json-schema-manager-config.yml`, into it. Edit the file to set your environments and their URL roots. See [Registry](./registry.md#schema-registry-configuration-file).

```bash
jsm create-registry /path/to/registry
```

The command fails if the directory already contains a registry.

### Creating a completely new schema family

A schema family is a collection of different versions of the same schema which validate different versions of a JSON document serving a specific purpose, such as:

- a config file
- an entity in a document database
- a message in an event driven architecture
- an API request or response body
- any other JSON, YAML, or TOML document.

These are used in different ways, and what can break a family depends on which: see [Schema Family Scenarios](./scenarios.md).

It is identified by a domain path and a family name. To create the first schema:

```bash
jsm create-schema "<domain path>/<family name>"
```

Where

- **`<domain path>`**: is a list of one or more domains separated by `/`
- **`<family name>`**: a name which describes the purpose of the schema family.

Valid characters are: `a-z`, `0-9`, `-`, and `/`.

The initial schema in a family will have version 1.0.0, and use the `defaultJsonSchemaVersion` from the registry's configuration file.

The command also asks which scenario the family serves, or takes it as `--scenario`, and writes the family's [declaration](./scenarios.md#declaring-a-familys-scenario), `family.yml`, beside its version directories. Most commands fail on a family without one.

For example, if an organisation has a customer success team, and the team wants a JSON Schema to represent the entity `B2C Customer` in their system, they might choose to create a new family `b2c-customer` within domain `customer-success` and subdomain `entity`, using the command:

```bash
jsm create-schema "customer-success/entity/b2c-customer"
                   ________________/______/___________
                        domain        |         |
                                  subdomain     |
                                            family name
```
Use as many domain levels as you need to make it easy to find specific schema families.

This command will create the following JSON schema, with empty `pass` and `fail` directories beside it:

`[registry root]/customer-success/entity/b2c-customer/1/0/0/customer-success_entity_b2c-customer_1_0_0.schema.json`

Any missing directories will be created on the fly.

Git does not keep empty directories, so until you add test documents, the `pass` and `fail` directories are missing from other clones of the repository. `jsm validate` treats a missing directory like an empty one, and warns that the schema has no pass or fail documents.

The schema's **key** is its filename without `.schema.json`, e.g. `customer-success_entity_b2c-customer_1_0_0`. Most commands identify a schema by its key, its file path, or its canonical ID.

### Creating a new version of an existing schema

Once deployed to an environment which does not allow schema mutation, JSON schemas are immutable. If you need to augment or change a deployed schema, create a new semantic version of the schema.

```bash
jsm create-schema-version <schema> <major|minor|patch>
```

where `<schema>` is the key, file path or canonical ID of an existing version of the family. It will:
- create any necessary folders
- create the schema file, as a copy of the given version
- create its `pass` and `fail` directories, and copy test documents into them if the family needs them: see [What a version carries](#what-a-version-carries)

The new version number depends on which versions currently exist within the schema family, not on the version you give.

E.g. Given a family 'my-schema' with the following 5 versions:

```
<registry root>/domain-a/my-schema/1/0/0/domain-a_my-schema_1_0_0.schema.json
<registry root>/domain-a/my-schema/1/1/0/domain-a_my-schema_1_1_0.schema.json
<registry root>/domain-a/my-schema/1/1/1/domain-a_my-schema_1_1_1.schema.json
<registry root>/domain-a/my-schema/2/0/0/domain-a_my-schema_2_0_0.schema.json
<registry root>/domain-a/my-schema/2/0/1/domain-a_my-schema_2_0_1.schema.json
```

- Creating a new **major** version:

  `jsm create-schema-version domain-a_my-schema_1_0_0 major`

  creates the following schema, as there already exist major versions 1 and 2:

  `<registry root>/domain-a/my-schema/3/0/0/domain-a_my-schema_3_0_0.schema.json`

- Creating a new **minor** version:

  `jsm create-schema-version domain-a_my-schema_1_0_0 minor`

  creates the following schema, as for the major version 1 there exist minor versions 0 and 1:

  `<registry root>/domain-a/my-schema/1/2/0/domain-a_my-schema_1_2_0.schema.json`

- Creating a new **patch** version:

  `jsm create-schema-version domain-a_my-schema_1_0_0 patch`

  creates the following schema, as for the major version 1 and minor version 0, there exists only one patch version 0:

  `<registry root>/domain-a/my-schema/1/0/1/domain-a_my-schema_1_0_1.schema.json`

Since the new version's schema is a copy of the one you give, you will usually give the newest version in the family.

#### What a version carries

A new version carries only the documents for what it changes. Earlier documents in the major already run on it, as the family's [scenario](./scenarios.md#the-rule) says, so a copy would only run the same check twice, and hide which document is there for which change.

| Bump | Family with cross-version runs | Family with neither side ahead, or whose reader picks its schema |
| --- | --- | --- |
| Patch or minor | The schema only | The schema and the given version's documents |
| Major | The schema and the given version's documents | The schema and the given version's documents |

- **A major bump copies documents as a starting point to edit**, since no document crosses a major and some will legitimately stop holding. It copies the documents of the version you give, not its whole major, so if that version carries few, add what the new major needs: `--strict` tells you if it has no `pass` or no `fail` document.
- **A family with no cross-version runs keeps copying**, such as [lock-step](./scenarios.md#8-lock-step) or [a family whose documents name their version](./scenarios.md#5-stored-data-each-document-names-its-version), because nothing else would test the new version.
- **Only documents are copied**: the `.json` files directly in `pass` and `fail`. Any other file in the version's directory stays behind, and `compat-exceptions.yml` never comes along, as an exception belongs to one version.
- **Documents already copied into a version are left alone, and still run.** You can delete them from a version which isn't deployed yet, if you want to.

So for a minor version which adds an enum value, add one `pass` document which uses the value and, in a writer-ahead family, nothing else.

### Testing schemas

- `jsm validate all` - tests every schema in the registry.
- `jsm validate <schema>` - tests one schema, given by its key, file path or canonical ID.
- `jsm validate <directory>` - tests every schema within the directory or its subdirectories.
- `jsm validate <domain>/<family>` - tests every version of a family (a search scope).

Each schema is tested against its own test documents: every document in its `pass` directory must be accepted, and every document in its `fail` directory must be rejected.

`jsm validate` also checks each version against every other version with the same major version. Which documents run across versions depends on the scenario the family [declares](./scenarios.md#declaring-a-familys-scenario), as [the rule](./scenarios.md#the-rule) says. The same pairs run however you select a version, so validating an earlier version also shows a later version that breaks it, and the failure appears under the later version. In a family where the provider moves first, see [Compatibility when the provider moves first](#compatibility-when-the-provider-moves-first).

If a break is deliberate, or is between versions already deployed, list that one run in the later version's `compat-exceptions.yml`, with a reason: see [Deliberate exceptions](./scenarios.md#deliberate-exceptions). The report shows it as excepted, and it doesn't fail.

#### Unexercised changes

The checks across versions are only as strong as the documents. If a minor version of a writer-ahead family adds an enum value, and no `pass` document uses it, every document still passes the earlier version, and the check cannot see that readers on the earlier version would reject the new value.

So `jsm validate` also compares the same pairs of versions structurally, each version with every earlier one in its major, however you selected them. It warns about each change in a direction the family protects which no document shows, under the later version:

```
[WARN] shop_order_1_1_0.schema.json since shop_order_1_0_0:
  ! /status: enum adds "refunded" (widens): needs a pass document
```

Which changes need a document follows the family's [scenario](./scenarios.md#the-scenarios):

| Who can be ahead | A change that widens | A change that narrows | A change it can't judge (`alters`) |
| --- | --- | --- | --- |
| Writer | `pass` document | Nothing | `pass` document |
| Reader | Nothing | `fail` document | `fail` document |
| Either | `pass` document | `fail` document | Both |
| Neither | Nothing | Nothing | Nothing |

A document shows a change if it has a value at the change's location that:

- for a `pass` document, the earlier version rejects. A value both versions accept shows nothing. Where the change has a precise test, such as an enum value added, that test decides; otherwise the real validator judges the earlier version's subschema at that location.
- for a `fail` document, the earlier version's subschema at that location accepts, and the later version's rejects.

Documents of any version after the earlier one, up to the later one, count. A change which a version carries over unchanged from the version before it warns only under the version that made it.

Add the document. If the rule then fails, the failure says what to do. If no document can show the change, such as a `maxLength` moved into an `allOf`, [acknowledge it](./scenarios.md#acknowledging-a-warning-no-document-can-clear) in `compat-exceptions.yml`.

**It's good practice to add a `fail` document for a new constraint in a writer-ahead family too**, though nothing warns if you don't. Making `customerId` required is safe there, so it needs no document. But a `fail` document with no `customerId` runs on every later version, so a later version dropping the requirement then fails outright, rather than only warning. The same goes for a `pass` document for a new option in a reader-ahead family.

**A `fail` document should hold one fault.** Where a family runs documents across versions, a document rejected for several independent reasons can hide a change to one of them, so `validate` warns, naming the locations. An `anyOf` or `oneOf` counts once however its arms fail.

`jsm diff <older> <newer>` lists every structural change between two versions, and which `pass` documents use each.

**What the comparison can tell.** It's structural, and it doesn't try to prove one schema contains another.

- It follows local `$ref`s, and from draft 2019-09 on, compares the keywords beside one too. It compares `patternProperties`, `format` where the validator asserts it (with `assertFormat`, or in a draft before 2019-09) and a change of `$schema`.
- `allOf` and `anyOf` keep a change's direction, and `not` flips it. Reordering arms is no change.
- Where it cannot tell which way a change goes, it says `alters`: a change inside `oneOf`, `if`, `then` or `else`, or to a `pattern`. The exception is a `oneOf` whose arms can't overlap: every pair has different `type`s (`integer` and `number` do overlap), or different `const`s on the same required property. Those keep exact labels.

#### Coverage

Every schema `jsm validate` tests is listed in its report, and one with no `pass` document, or no `fail` document, is marked `[WARN]`.

Documents count if they are in the version's own directory, or inherited from an earlier version in its major. The first version of a major, including `1.0.0`, has nothing to inherit, so it needs its own, and the copies a major bump makes are its own. A family with neither side ahead, or whose reader picks its schema from the document, inherits nothing, so each of its versions needs its own.

Inheriting a document is not the same as running it on that version. Which documents run is for [the rule](./scenarios.md#the-rule) to say. In a writer-ahead family, for example, earlier `pass` documents count here but don't run on later versions, so a version with no `pass` document of its own has nothing showing what it accepts. Add one when that matters: [unexercised-change warnings](#unexercised-changes) ask for one when a change needs it.

With `--verbose`, the report also shows how many documents a schema inherits, how much of the schema its own `pass` documents reach, and which keywords reject each of its own `fail` documents:

```
[PASS] shop_order_1_0_0.schema.json (pass: 3, fail: 0)
  coverage: 3 of 5 points reached by pass documents
    - /: property note
    - /status: enum "refunded"
  .../fail/lost.json is rejected by #/properties/status/enum
```

The points are the properties a schema declares, its enum values, the types of a `type` list, its `anyOf` and `oneOf` subschemas, and the two paths through each `if`. The JSON report (`--output json`) always includes this, under each schema's `summary`, with `inheritedPassDocuments` and `inheritedFailDocuments`.

The warnings, for unexercised changes, for missing documents and for `fail` documents with several reasons, do not change the exit code unless you add `--strict`, which makes `jsm validate` exit `1` on any warning.

By default, `jsm validate` stops at the first failing test. Use `--continue-on-error` (`-C`) to run them all, `--output json` for a machine-readable report, `--output junit` for a JUnit XML report, and `--watch` to rerun the tests when files change. See [jsm validate](./reference/jsm_validate.md) for every option.

### Rendering and building schemas

Source schemas contain templates, such as `"$id": "{{ ID }}"`, which JSM fills in for a given environment. See [Special Properties](./registry.md#special-properties).

- `jsm render-schema <schema> [--env <env>]` - prints the rendered schema for an environment (production by default).
- `jsm build-dist <env>` - tests every schema, then writes the rendered schemas changed since the last deployment to `dist/<env>/private` and `dist/<env>/public`, next to the registry root. So the registry must be in a subdirectory of its git repository; every other command works with it at the git root. It finds the changes with git, so it leaves out, and lists, schemas which git does not track yet. Use `--all` to build every schema, for example when trying out schemas locally.
- `jsm check-changes <env>` - fails if a deployed schema or its test documents were changed, in an environment which does not allow schema mutation.
- `jsm tag-deployment <env>` - records a successful deployment with a git tag, which `check-changes` and `build-dist` compare against.

Add `--collapse` to `render-schema` or `build-dist` to inline the other JSM schemas a schema refers to, making each schema self-contained. Generate typed models from collapsed schemas: see [Generating Typed Models](./generating-models.md). AsyncAPI and OpenAPI documents can refer to them too: see [Referring to JSM Schemas from AsyncAPI and OpenAPI](./api-documents.md).

JSM does not publish the `dist` directory: copy it to wherever your schemas are served from, at the URL roots in the configuration file.

### Checking a party's schema versions

A party is a service or tool which reads or writes a family's documents, and can lag behind later versions. It can ask whether those later versions are safe for it. List the versions it uses in a party manifest, `jsm-party.yml` by default, by key or canonical URL, and state its `role`, which is required:

```yaml
role: reader
schemas:
  - shop_order_1_0_0
  - https://json-schemas.myorg.io/customer_1_2_0.schema.json
```

A service which both reads and writes a family has a manifest for each role.

Then check them against the registry, in the party's CI with the registry checked out, or in the registry's CI for each party:

```bash
jsm check-party path/to/jsm-party.yml
```

The family's [scenario](./scenarios.md) decides what can harm the party. The check fails if a listed version isn't in the registry. Otherwise, it runs the documents [the rule](./scenarios.md#the-rule) runs between that version and each later version in its major, for the party's side:

| Party | Family | What runs | A failure means |
|-------|--------|-----------|-----------------|
| Reader | Writer-ahead or either | Later `pass` documents on the listed version, and the listed version's `fail` documents on later versions | A writer on the later version can send this reader a document it rejects |
| Writer | Reader-ahead or either | The listed version's `pass` documents on later versions, and later `fail` documents on the listed version | A reader on the later version rejects a document this writer sends |
| Any other combination | | Nothing | No later version can harm this party, so it gets a notice saying why, and the check exits `0` |

For example, a team running a service which takes requests from old clients declares `consumer-first`. The client team is a writer, and lags behind the service, so its check is the one that can fail: if `1.1.0` rejects a document `1.0.0` accepted, the client on `1.0.0` is broken. The service team's own check, as a reader, gives a notice, since it always moves first.

A failure is reported as `validate` reports it, under the later version, naming the rule, both versions and the document. A [deliberate exception](./scenarios.md#deliberate-exceptions) applies here too, and the report shows it with its reason. A stale exception entry fails `validate`, not this check, since it doesn't harm the party.

The check doesn't look across majors. It warns if the family has a newer major version, so you can plan the party's move to it. `--strict` makes warnings fail, as on `validate`, and `--output json` gives the result as JSON.

## Exit codes

Every `jsm` command exits with one of these codes, so CI can gate on it without parsing the output:

| Code | Meaning |
|------|---------|
| `0`  | The command succeeded, and every test and check passed. |
| `1`  | JSM ran, and a test or check found a problem: a test document failed in `validate` or `build-dist` (including a newer version breaking an earlier one), or an entry in a `compat-exceptions.yml` is stale, `check-changes` found a deployed schema or exception modified, or `check-party` found a listed version missing or harmed by a later one. With `--strict`, a warning also exits `1`. The report says which. |
| `2`  | JSM could not run: an unknown command or flag, a missing registry or configuration, a schema that cannot be found or compiled, or a git error. |
| `130` | JSM was interrupted, by Ctrl+C (SIGINT), before it finished. |
| `143` | JSM was stopped by SIGTERM, for example when a CI job times out, before it finished. |

## Running JSM in CI

Run every check on each pull request, and build, deploy and tag on the main branch. A GitHub Actions example, for a repository with its registry in `schemas/`:

```yaml
env:
  JSM_REGISTRY_ROOT_DIR: schemas # build-dist writes dist/ next to it, so it must be a subdirectory

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0 # check-changes needs the deployment tags
      - uses: actions/setup-go@v7
        with:
          go-version: stable
      - run: go install github.com/bitshepherds/json-schema-manager/cmd/jsm@latest
      - run: jsm validate all --continue-on-error
      - run: jsm check-changes prod

  deploy:
    if: github.ref == 'refs/heads/main'
    needs: check
    runs-on: ubuntu-latest
    permissions:
      contents: write # tag-deployment pushes a tag
    steps:
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v7
        with:
          go-version: stable
      - run: go install github.com/bitshepherds/json-schema-manager/cmd/jsm@latest
      - run: jsm build-dist prod # runs check-changes and the tests first
      - run: ./publish.sh dist/prod # your own step: copy the schemas to where they are served
      - run: | # tag-deployment makes an annotated tag, which needs a git identity
          git config user.name "github-actions[bot]"
          git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
      - run: jsm tag-deployment prod
```

Pin `jsm` to a release version, rather than `@latest`, so a new release cannot change your pipeline unannounced. `jsm --version` prints the version installed.

### Test results in CI

Most CI systems can show JUnit XML test results, with each failing test on the pull request. `jsm validate all --continue-on-error --output junit > jsm-results.xml` writes one: each schema is a test suite, and each test document run against it a test case. It exits with the same code as the text report, so it still fails the job. JUnit has no warnings, so a schema's warnings are in its suite's `system-out`, or, with `--strict`, are failing test cases. A run a [deliberate exception](./scenarios.md#deliberate-exceptions) names is a skipped test case, whose message gives the reason, and a stale exception is a failing one.

In GitHub Actions, upload the file even when the step fails, then show it with a test reporter action of your choice:

```yaml
- run: jsm validate all --continue-on-error --output junit > jsm-results.xml
- uses: actions/upload-artifact@v7
  if: ${{ !cancelled() }}
  with:
    name: jsm-results
    path: jsm-results.xml
```

### Git history in CI

`check-changes` and `build-dist` compare the registry with the last deployment to an environment, which is the newest `jsm-deploy/<env>/*` tag created by `jsm tag-deployment`. They need the history and tags back to that tag. CI systems often clone with only the latest commit, so fetch the full history. In GitHub Actions:

```yaml
- uses: actions/checkout@v7
  with:
    fetch-depth: 0
```

In a shallow clone without the tag, both commands fail with exit code `2` rather than guess. With no deployment tag at all, every schema is treated as new.

`jsm tag-deployment <env>` pushes the tag to `origin`, so the deploy job needs permission to push tags (in GitHub Actions, `permissions: contents: write`). If the push fails, the command fails with exit code `2` and prints the `git push` command to retry. Use `--no-push` to create the tag locally only.


---

# Why semantic versioning?

Imagine an event is written by a provider to an event bus which has multiple consumers, and imagine the provider and consumers are separate, individually released components in a distributed system.

A JSON Schema is used to define the contract for the message. This allows both the provider and consumers to validate - in test and production - that what they send or receive is valid.

Over time, the provider may want to change the contract. However, consumers will expect the messages they receive to continue to conform to the contract they have.

[Semantic versioning](https://semver.org/) is an established pattern for managing changes to something over time. JSON Schema Manager utilises semantic versioning to manage breaking and non-breaking changes to JSON schemas. Semantic versions have three components: major, minor and patch.

`<major>.<minor>.<patch>` e.g. `1.2.3`

JSON Schema Manager uses your test documents to look for changes which would break consumers, before you deploy a new version of a provider. Documents only sample what a schema does, so a check which passes shows that none of them exposed a break, not that no break exists. Which changes break a family also depends on how it is used: see [Schema Family Scenarios](./scenarios.md).

JSON Schema Manager interprets semantic versioning as follows:

- **major** version changes signal breaking changes. Unless you are in charge of both the provider and consumers, you should plan to deploy breaking changes separately - e.g. create a new API or message bus, and give consumers a deprecation window to migrate.
- **minor** version changes signal non-breaking feature changes which can be deployed by the provider without breaking a consumer who is using a JSON Schema with an earlier variant of the same major version. Typically, this might include adding new optional properties to a schema. For example, if the consumer is checking against version `1.2.3`, it should be safe for the provider to start sending messages conforming to version `1.3.0`.
- **patch** version changes signal adjustments to the schema which have no new features, are non-breaking, but represent a change nevertheless. For example, adding a description to a property.

## Compatibility when the provider moves first

This is one way of using a schema: a writer-ahead family, where data flows from a provider to its consumers, and the provider moves first. It starts sending documents which conform to the new version, while its consumers are still checking against an earlier one. Consumers catch up later, in their own time. That is scenario 1 of the [Schema Family Scenarios](./scenarios.md), which describes the others, and what JSM does and doesn't do yet for each.

For such a family, JSM checks compatibility in that one direction. For every minor or patch version, its `pass` documents must be accepted by each earlier version with the same major version, and each earlier version's `fail` documents must be rejected by it. If not, a consumer still on the earlier version would reject what the provider now sends, and `jsm validate` fails, naming both versions.

A family declared this way doesn't check the other way, that documents valid under an earlier version are accepted by a newer one, so it does not protect:

- a consumer which upgrades before its provider does, and rejects what the provider still sends;
- a client whose requests a newly upgraded service now refuses;
- a new consumer replaying events kept from earlier versions;
- stored data or config files, read long after they were written.

If any of those describes your family, declare the [scenario](./scenarios.md) it serves, and JSM checks what that scenario needs instead.

In a writer-ahead family, consumers should follow the robustness principle: be strict in what you send, and tolerant in what you accept. A consumer which ignores the fields it does not need can stay on its version while a provider adds new ones. That covers additions only. It does not help a consumer which upgrades to a version that requires a new field, when the provider has not started sending it.

So in a writer-ahead family, if a change needs consumers to move first, for example because a newer version would reject documents which the current one accepts, treat it as a breaking change. Make it a new major version, run the old and new major versions side by side, and give consumers a deprecation window to move to the new one before the provider stops sending the old.

## Innocuous changes which are actually breaking

Be aware of the following changes to a schema which act as breaking changes, even though they seem innocuous. Although JSON Schema Manager will highlight such breaking changes during testing, if you have already deployed the initial schema, then you will be forced to deal with a breaking change that you could have avoided.

### Adding enum values

E.g. If a property `status` is defined as `enum ["pending", "approved", "rejected"]`, and you add `"cancelled"`, consumers on an earlier minor version will break when they receive a message with a `status` of `"cancelled"`.

### Relaxing a constraint

E.g. raising a property's `maxLength` from 10 to 20 lets the provider send values which consumers on the earlier version reject. Tightening a constraint, by contrast, is safe for those consumers.

### additionalProperties

Only ever set `"additionalProperties": false` in a schema when you are absolutely certain that you will not need to create a new version of the schema with additional properties.

If omitted, the default is to allow additional properties, which is usually what you want. It is strongly advised to configure consumers to ignore properties they're not expecting. This allows for the safe deployment of individual components in a distributed system.
