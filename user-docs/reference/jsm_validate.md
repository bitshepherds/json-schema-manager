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

The report counts runs, not documents. A run is one document checked against one schema version. A
version's own documents give one run each. A run across versions is counted under the later version of
the pair, and happens only in the directions the table above gives for the family's scenario, so the
count depends on the family: a version can show more runs than it has documents.

A version's compat-exceptions.yml can list a deliberate break of the rule, with its reason. The report
shows that run as excepted, and it doesn't fail. An entry which clears nothing is stale, and fails.

A family whose documents live indefinitely, and whose reader doesn't pick its schema from the document,
needs a migration statement in family.yml for each major past its first. Without one, validate fails,
under that major's first version.

These checks are only as strong as the documents, so validate also compares the same pairs of
versions structurally, and warns about each change in a direction the family protects which no
document shows. A writer-ahead family needs a pass document for a change which widens what's accepted,
such as an enum value added. A reader-ahead family needs a fail document for one which narrows it, a
family with either ahead needs both, and a family with neither ahead needs none. Where structdiff can't
tell which way a change goes, it needs the documents for the direction the family protects. A change no
document can show is acknowledged in compat-exceptions.yml. 'jsm diff' lists every structural change
between two versions.

It also warns about each schema with no pass document, or no fail document, and each fail document
rejected for several independent reasons, where its family runs documents across versions. Documents
inherited from an earlier version in the major count as a schema's own here, though not all of them run on
it: in a writer-ahead family an inherited pass document doesn't run on the later version, and in a
reader-ahead family nor does an inherited fail document. With --verbose, the
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
  -C, --continue-on-error[=true|false]   Continue testing even if a schema test fails (default is to stop on first error)
  -h, --help[=true|false]                help for validate
  -i, --id string                        Validate a schema identified by its canonical ID
  -k, --key string                       Validate a schema identified by its key
  -o, --output <format>                  Output format (text, json, junit) (default text)
  -s, --search-scope string              Validate all schemas identified by JSM Search Scope
      --strict[=true|false]              Fail on warnings: a schema with no pass or no fail document, a fail document rejected for several reasons, or a change in a direction the family protects which no document shows
  -v, --verbose[=true|false]             Show detailed test results
  -w, --watch[=true|false]               Watch for changes and rerun tests
```

### Options inherited from parent commands

```
  -d, --debug[=true|false]      Enable debug logging
  -c, --nocolour[=true|false]   Disable colour in output
  -r, --registry string         path to the registry root directory (default: the current directory, if it holds a registry, or else $JSM_REGISTRY_ROOT_DIR)
```

### SEE ALSO

* [jsm](jsm.md)	 - A professional tool for managing JSON schemas

