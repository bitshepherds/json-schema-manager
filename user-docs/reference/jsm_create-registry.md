## jsm create-registry

Create a new JSON schema registry

### Synopsis

Create a new directory and initialise it with a default JSON schema manager configuration file.

```
jsm create-registry [dirpath] [flags]
```

### Examples

```

jsm create-registry ./my-registry

```

### Options

```
  -h, --help   help for create-registry
```

### Options inherited from parent commands

```
  -d, --debug             Enable debug logging
  -c, --nocolour          Disable colour in output
  -r, --registry string   path to the registry root directory (default: $JSM_REGISTRY_ROOT_DIR, or else the current directory)
```

### SEE ALSO

* [jsm](jsm.md)	 - A professional tool for managing JSON schemas

