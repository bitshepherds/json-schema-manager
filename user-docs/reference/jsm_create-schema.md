## jsm create-schema

Create a new JSON schema

### Synopsis

Create a completely new JSON schema family in the registry. The 1.0.0 version of the schema will be created.

The command asks which scenario the family serves, unless --scenario says, and writes its family.yml.

Every family declares how it is used in family.yml, in the family's directory beside its version
directories. It names a scenario, and may override the facts the scenario implies:

  scenario: provider-first
  # Optional overrides of what the scenario implies:
  ahead: writer             # writer | reader | either | neither
  documentsLive: moment     # moment | retention | indefinitely
  readerPicksSchema: false
  schema: prescribes        # prescribes | describes
  # Required when documentsLive is retention:
  oldestLive: 1.2.0

The scenarios, and what to choose, are in user-docs/scenarios.md. They are provider-first,
consumer-first, config-file, stored-one-schema, stored-self-naming, retained-stream, uncoordinated,
lock-step and third-party-format.

How long documents live varies in the scenarios uncoordinated and third-party-format, so a family in
either must state documentsLive. If you don't know how long they live, choose indefinitely.

Every command which reads a family fails if it has no declaration, or an invalid one, except
render-schema and tag-deployment. Once any version of a family is deployed, check-changes refuses a
change to its declaration, other than moving oldestLive forward: a changed scenario means a new family.


```
jsm create-schema [domain/family] [flags]
```

### Examples

```

jsm create-schema "domain-a/family-a"
jsm create-schema "domain-a/family-a" --scenario provider-first
jsm create-schema "domain-a/subdomain-b/family-c" --scenario uncoordinated --documents-live indefinitely

```

### Options

```
      --documents-live string   How long documents outlive their writer: moment, retention or indefinitely. Required for the scenarios uncoordinated and third-party-format. If you don't know, choose indefinitely
  -h, --help                    help for create-schema
      --scenario string         The scenario the family serves: provider-first, consumer-first, config-file, stored-one-schema, stored-self-naming, retained-stream, uncoordinated, lock-step, third-party-format
```

### Options inherited from parent commands

```
  -d, --debug             Enable debug logging
  -c, --nocolour          Disable colour in output
  -r, --registry string   path to the registry root directory (default: $JSM_REGISTRY_ROOT_DIR, or else the current directory)
```

### SEE ALSO

* [jsm](jsm.md)	 - A professional tool for managing JSON schemas

