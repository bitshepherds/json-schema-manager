## jsm create-schema-version

Create the next major, minor, or patch version of the given schema family

### Synopsis


Create the next major, minor, or patch version of the given schema family.
The new version is numbered after the highest existing version: for example, if 1.0.0,
1.1.0 and 2.0.0 exist, a minor version of 1.0.0 is 1.2.0, and a major version of any of
them is 3.0.0. The new version starts as a copy of the target's schema, so the target is
usually the newest version in the family.

What else it carries depends on the bump and on the family's declaration:

- A patch or minor bump copies the schema only. Earlier documents in the major run on the
  new version as the family's scenario says, and copies would run the same checks twice, so
  add only the documents for what this version changes.
- A major bump also copies the target's own pass/ and fail/ documents, not the rest of its
  major, as a starting point to edit, since some of them may legitimately stop holding.
- A family with no pairs of versions to run, whose scenario has neither side ahead or whose
  reader picks its schema from the document, copies the documents on every bump, because
  nothing else would test the new version.

Other files in the target's directory, such as compat-exceptions.yml, belong to that version
and are never copied.

A major bump in a family whose documents live indefinitely, and whose reader doesn't pick its
schema from the document, needs a migration statement in family.yml before validate passes.


```
jsm create-schema-version [target] [release-type] [flags]
```

### Examples

```

IDENTIFYING A TARGET SCHEMA
- By Key:
  jsm create-schema-version -k "domain_family_1_0_0" major
  jsm create-schema-version "domain_family_1_0_0" major
- By Canonical ID (i.e. its eventual production URL):
  jsm create-schema-version -i "https://js.myorg.com/domain_family_1_0_0.schema.json" minor
  jsm create-schema-version "https://js.myorg.com/domain_family_1_0_0.schema.json" minor
- By File Path:
  jsm create-schema-version "./path/to/domain_family_1_0_0.schema.json" major

```

### Options

```
  -h, --help         help for create-schema-version
  -i, --id string    Identify a target schema by its canonical ID
  -k, --key string   Identify a target schema by its key
```

### Options inherited from parent commands

```
  -d, --debug             Enable debug logging
  -c, --nocolour          Disable colour in output
  -r, --registry string   path to the registry root directory (default: $JSM_REGISTRY_ROOT_DIR, or else the current directory)
```

### SEE ALSO

* [jsm](jsm.md)	 - A professional tool for managing JSON schemas

