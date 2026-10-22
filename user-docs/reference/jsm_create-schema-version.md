## jsm create-schema-version

Create the next major, minor, or patch version of the given schema family

### Synopsis


Create the next major, minor, or patch version of the given schema family.
The new version is numbered after the highest existing version: for example, if 1.0.0,
1.1.0 and 2.0.0 exist, a minor version of 1.0.0 is 1.2.0, and a major version of any of
them is 3.0.0. The new version starts as a copy of the target, with its pass/ and fail/
test documents, so the target is usually the newest version in the family.


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
  -v, --verbose      Show details of the new version
```

### Options inherited from parent commands

```
  -d, --debug             Enable debug logging
  -c, --nocolour          Disable colour in output
  -r, --registry string   path to the registry root directory (default: $JSM_REGISTRY_ROOT_DIR, or else the current directory)
```

### SEE ALSO

* [jsm](jsm.md)	 - A professional tool for managing JSON schemas

