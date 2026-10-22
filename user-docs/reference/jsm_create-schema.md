## jsm create-schema

Create a new JSON schema

### Synopsis

Create a completely new JSON schema family in the registry. The 1.0.0 version of the schema will be created.

```
jsm create-schema [domain/family] [flags]
```

### Examples

```

jsm create-schema "domain-a/family-a"
jsm create-schema "domain-a/subdomain-b/family-c"

```

### Options

```
  -h, --help   help for create-schema
```

### Options inherited from parent commands

```
  -d, --debug             Enable debug logging
  -c, --nocolour          Disable colour in output
  -r, --registry string   path to the registry root directory (default: $JSM_REGISTRY_ROOT_DIR, or else the current directory)
```

### SEE ALSO

* [jsm](jsm.md)	 - A professional tool for managing JSON schemas

