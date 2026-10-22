## jsm validate

Validate one or more JSON schemas

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
  -o, --output <format>       Output format (text, json) (default text)
  -s, --search-scope string   Validate all schemas identified by JSM Search Scope
      --skip-compatible       Skip checking each schema's pass documents against earlier versions in its major family
  -t, --test-scope string     Test Docs Selection (local, pass-only, fail-only, consumer-breaking, all) (default "local")
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

