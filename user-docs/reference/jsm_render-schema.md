## jsm render-schema

Output the rendered version of a schema

```
jsm render-schema [target] [flags]
```

### Examples

```

  jsm render-schema "domain_family_1_0_0"
  jsm render-schema -k "domain_family_1_0_0" --env dev
  jsm render-schema "https://js.myorg.com/domain_family_1_0_0.schema.json"
  jsm render-schema "domain_family_1_0_0" --collapse

```

### Options

```
      --collapse[=true|false]   Inline external $ref dependencies into the schema's $defs block, failing if the result does not compile or its test documents give different results
  -e, --env string              The environment to use for rendering (defaults to production)
  -h, --help[=true|false]       help for render-schema
  -i, --id string               Identify a target schema by its canonical ID
  -k, --key string              Identify a target schema by its key
```

### Options inherited from parent commands

```
  -d, --debug[=true|false]      Enable debug logging
  -c, --nocolour[=true|false]   Disable colour in output
  -r, --registry string         path to the registry root directory (default: the current directory, if it holds a registry, or else $JSM_REGISTRY_ROOT_DIR)
```

### SEE ALSO

* [jsm](jsm.md)	 - A professional tool for managing JSON schemas

