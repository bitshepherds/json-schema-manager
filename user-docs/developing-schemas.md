# Developing Schemas with JSON Schema Manager

- [Developing Schemas with JSON Schema Manager](#developing-schemas-with-json-schema-manager)
  - [Installation](#installation)
- [Quickstart](#quickstart)
- [Registry Root](#registry-root)
- [Commands](#commands)
  - [Help](#help)
  - [Registry commands](#registry-commands)
    - [Initialising a new JSM Registry](#initialising-a-new-jsm-registry)
  - [Creating a completely new schema family](#creating-a-completely-new-schema-family)
    - [Creating a new version of an existing schema](#creating-a-new-version-of-an-existing-schema)
  - [Testing Schemas](#testing-schemas)
- [Why semantic versioning?](#why-semantic-versioning)
  - [Innocuous Changes which are actually breaking](#innocuous-changes-which-are-actually-breaking)
    - [Adding enum values](#adding-enum-values)
    - [additionalProperties](#additionalproperties)


## Installation

TODO: Move JSM to bitshepherds.com and update this section.

Download the latest binary for your platform from the [GitHub Releases](https://github.com/bitshepherds/json-schema-manager/releases) page.

Alternatively, if you have Go installed, you can install it directly:

```bash
go install github.com/bitshepherds/json-schema-manager/cmd/jsm@latest
```

TODO: After moving the project to bitshepherds update the section above.

# Quickstart

TODO

# Registry Root

The Registry Root is the root directory of the JSON Schema registry. It is the directory that contains all the JSON schemas and their associated files, and will also usually be the root directory of the local git repository where the schemas are stored.

You can specify the Registry Root in the following ways:

- with the `jsm` CLI flag: `--registry` 
- with the environment variable: `JSM_REGISTRY_ROOT`.

The flag overrides the environment variable.

# Commands

## Help

- `jsm help` - shows help for all commands
- `jsm help <command>` - shows help for the given command

## Registry commands

### Initialising a new JSM Registry

A JSM Registry is a directory containing all the schemas in an organisation, and the tests for those schemas.
It should be the root of a git repository.

The first time you use JSON Schema Manager, you will need to initialise a new JSM Registry.

`jsm init` - creates or initialises a new JSM Registry at the path described by the `--registry` flag or the `JSM_REGISTRY_ROOT` environment variable. (See [Registry root](#registry-root))

E.g. 

To create a new registry at the path defined by `JSM_REGISTRY_ROOT`:
```bash
jsm init
```

To create a new registry at `/path/to/registry`:
```bash
jsm init --registry="/path/to/registry"
```

The command will fail if the registry was already initialised or the path cannot be created.

## Creating a completely new schema family

A schema family is a collection of different versions of the same schema which validate different versions of a JSON document serving a specific purpose, such as:

- a config file
- An entity in a document database
- a message in an event driven architecture
- an API request or response body
- any other JSON, YAML, or TOML document.
- It is identified by a domain path and a family name.

To create the first schema: 

```bash
jsm create-schema "<domain path>/<family name>"
```

Where

- **`<domain-path>`**: is a list of one or more domains separated by  `/` 
- **`<family-name>`**: a name which describes the purpose of the schema family.

Valid characters are: `a-z`, `0-9`, `-`, and `/`.

The initial schema in a family will have version 1.0.0.

For example, if an organisation has a customer success team, and the team wants a JSON Schema to represent the entity `B2C Customer` in their system, they might choose to create a new family `b2c-customer` within domain `customer-success` and subdomain `entity`, using the command:

```bash
jsm create-schema "customer-success/entity/b2c-customer"
                   ________________/______/___________
                        domain        |         |
                                  subdomain     |
                                            family name                                    
```
Use as many domain levels as you need to make it easy to find specific schema families.

This command will create the following JSON schema (and its supporting files):

`[registry root]/customer-success/entity/b2c-customer/1/0/0/customer-success_entity_b2c-customer_1_0_0.schema.json`

Any missing directories will be created on the fly.

### Creating a new version of an existing schema

Once published, JSON schemas are immutable. If you need to augment or change a published schema, the idiomatic approach is to create a new semantic version of the schema. 

Use the following command to create a new version of an existing schema:

`jsm create-schema-version <major|minor|patch> <existing schema file> `

It will:
- Create any necessary folders
- Create the schema file, initialising it to be a copy of the previous version
- Copy the passing and failing tests from the previous version

The actual semantic version of the new version of the schema depends on which versions currently exist within the schema family. It doesn't matter which version of a schema you provide to the command. It just needs to identify the schema family.

E.g. Given a family 'my-schema' with the following 5 versions:

```javascript
<registry root>/domain-a/my-schema/1/0/0/domain-a_my-schema_1_0_0.schema.json
<registry root>/domain-a/my-schema/1/1/0/domain-a_my-schema_1_1_0.schema.json
<registry root>/domain-a/my-schema/1/1/1/domain-a_my-schema_1_1_1.schema.json
<registry root>/domain-a/my-schema/2/0/0/domain-a_my-schema_2_0_0.schema.json
<registry root>/domain-a/my-schema/2/0/1/domain-a_my-schema_2_0_1.schema.json
```

- Creating a new **major** version:

  `jsm create-schema-version --version=major domain-a_my-schema_1_0_0.schema.json`

  creates the following schema, as there already exists major versions 1 and 2: 

  `<registry root>/domain-a/my-schema/3/0/0/domain-a_my-schema_3_0_0.schema.json`

- Creating a new **minor** version:
  
  `jsm create-schema-version --version=minor domain-a_my-schema_1_0_0.schema.json`
  
  creates the following schema, as for the major version 1 there exists minor versions 0 and 1:
  
  `<registry root>/domain-a/my-schema/1/2/0/domain-a_my-schema_1_2_0.schema.json`
      
- Creating a new **patch** version:

  `jsm create-schema-version --version=patch domain-a_my-schema_1_0_0.schema.json`
  
  creates the following schema, as for the major version 1 and minor version 0, there exists only one patch version 0:
      
  `<registry root>/domain-a/my-schema/1/0/1/domain-a_my-schema_1_0_1.schema.json`

## Testing Schemas

- `jsm test` - tests all the schemas in the registry.
- `jsm test <schema file>` - tests the given JSON Schema. The schema must be within the registry.
- `jsm test <directory>` - finds all JSON Schemas anywhere within the directory or its subdirectories and tests them.   

Note that JSON Schema Manager will automatically calculate which test documents to use in testing. For a given version of a schema in a family, it will also automatically apply test documents from certain other versions of the family to ensure that no inadvertent breaking changes have been introduced.


---

# Why semantic versioning?

Imagine an event is written by a provider to an event bus which has multiple consumers, and imagine the provider and consumers are separate, individually released components in a distributed system.

A JSON Schema is used to define the contract for the message. This allows both the provider and consumers to validate - in test and production - that what they send or receive is valid.

Over time, the provider may want to change the contract. However, consumers will expect the messages they receive to continue to conform to the contract they have.

[Semantic versioning](https://semver.org/) is an established pattern for managing changes to something over time. JSON Schema Manager utilises semantic versioning to manage breaking and non-breaking changes to JSON schemas. Semantic versions have three components: major, minor and patch.

`<major>.<minor>.<patch>` e.g. `1.2.3`

JSON Schema Manager explicity tests that supposedly non-breaking changes to a schema are genuinely non-breaking. This allows you to prevent accidentally breaking consumers when you deploy a new version of a provider

JSON Schema Manager interprets semantic versioning as follows:

- **major** version changes signal breaking changes. Unless you are in charge of both the provider and consumers, you should plan to deploy breaking changes separately - e.g. create a new API or message bus, and give consumers a deprecation window to migrate.
- **minor** version changes signal non-breaking feature changes which can be deployed by the provider without breaking a consumer who is using a JSON Schema with an earlier variant of the same major version. Typically, this might include adding new properties to a schema. For example, if the consumer is checking against version `1.2.3`, it should be safe for the provider to start sending messages conforming to version `1.3.0`.
- **patch** version changes signal adjustments to the schema which have no new features, are non-breaking, but represent a change nevertheless. For example, loosening a constraint on a property, or adding a description to a property.

## Innocuous Changes which are actually breaking

Be aware of the following changes to a schema which act as breaking changes, even though they seem innocuous. Although JSON Schema Manager will highlight such breaking changes during testing, if you have already deployed the initial schema, then you will be forced to deal with a breaking change that you could have avoided.

### Adding enum values

E.g. If a property `status` is defined as `enum ["pending", "approved", "rejected"]`, and you add `"cancelled"`, consumers on an earlier minor version will break when they receive a message with a `status` of `"cancelled"`. 

### additionalProperties

Only ever set `"additionalProperties": false` in a schema when you are absolutely certain that you will not need to create a new version of the schema with additional properties. 

If omitted, the default is to allow additional properties, which is usually what you want. It is strongly advised to configure consumers to ignore properties they're not expecting. This allows for the the safe deployment of individual components in a distributed system.