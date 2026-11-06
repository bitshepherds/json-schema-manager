## jsm validate

Validate one or more JSON schemas

### Synopsis


Validate one or more JSON schemas against their test documents: each document in a schema's pass/
directory must be accepted, and each in its fail/ directory rejected.

The family's declaration, family.yml, then decides which documents run across versions, between each
schema and every earlier and later version with the same major version:

  who can be ahead  what runs across versions
  writer            later pass documents on earlier versions, earlier fail documents on later versions
  reader            earlier pass documents on later versions, later fail documents on earlier versions
  either            all four
  neither           none, and so too when the reader picks its schema from the document

The same pairs run however a schema is selected, so validating an earlier version also runs its pairs
with later versions. A failure appears under the later version, since that's what changed, and says
which rule broke, the two versions, the document, and what that means for the family's scenario. See
user-docs/scenarios.md.

A version's compat-exceptions.yml can list a deliberate break of the rule, with its reason. The report
shows that run as excepted, and it doesn't fail. An entry which clears nothing is stale, and fails.

These checks are only as strong as the documents, so validate also compares each schema with the
version before it, and warns about each structural change which may break consumers of that version
and which none of the schema's pass documents uses: for example, an enum value added with no
document using it. Add a pass document which uses the change. 'jsm diff' lists every structural
change between two versions.

It also warns about each schema with no pass document, or no fail document. With --verbose, the
report shows how much of each schema its pass documents reach (its properties, enum values, types,
anyOf and oneOf subschemas, and the paths through each if), and which keywords reject each fail
document. The JSON report always includes these.

Use --strict to fail on any warning.


```
jsm validate [target] [flags]
```

### Examples

```

IDENTIFYING A SINGLE SCHEMA
- By Key:
  jsm validate -k "domain_family_1_0_0" 
  jsm validate "domain_family_1_0_0" 
- By Canonical ID (i.e. its eventual production URL):
  jsm validate -i "https://js.myorg.com/domain_family_1_0_0.schema.json" 
  jsm validate "https://js.myorg.com/domain_family_1_0_0.schema.json" 
- By File Path:
  jsm validate "./path/to/domain_family_1_0_0.schema.json"

IDENTIFYING MULTIPLE SCHEMAS
- By File Path:
  jsm validate "./path/to/domain/family" - targets all schema versions in the given family
- By JSM Search Scope: (a string of elements separated by '/')
  jsm validate "domain/family" - targets all schemas within the given family

ALL SCHEMAS
  jsm validate all
```

### Options

```
  -C, --continue-on-error     Continue testing even if a schema test fails (default is to stop on first error)
  -h, --help                  help for validate
  -i, --id string             Validate a schema identified by its canonical ID
  -k, --key string            Validate a schema identified by its key
  -o, --output <format>       Output format (text, json, junit) (default text)
  -s, --search-scope string   Validate all schemas identified by JSM Search Scope
      --strict                Fail on warnings: a schema with no pass or no fail document, or with a change which may break consumers of the version before it and which none of its pass documents uses
  -v, --verbose               Show detailed test results
  -w, --watch                 Watch for changes and rerun tests
```

### Options inherited from parent commands

```
  -d, --debug             Enable debug logging
  -c, --nocolour          Disable colour in output
  -r, --registry string   path to the registry root directory (default: $JSM_REGISTRY_ROOT_DIR, or else the current directory)
```

### SEE ALSO

* [jsm](jsm.md)	 - A professional tool for managing JSON schemas

