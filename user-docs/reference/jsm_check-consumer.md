## jsm check-consumer

Check the schema versions a consuming service validates against

### Synopsis


Check the schema versions a consuming service validates documents against, listed in its consumer
manifest (jsm-consumer.yml by default), against the registry. Run it in the consumer's CI, with the
registry checked out, or in the registry's CI for each consumer.

A manifest lists schema keys or canonical URLs:

  schemas:
    - shop_order_1_0_0
    - https://json-schemas.myorg.io/customer_1_2_0.schema.json

Providers move to new versions before their consumers do. So for each listed version, the check
fails if the version is not in the registry, or if a later version with the same major version has
a pass document which the listed version rejects: a provider sending that document would break the
consumer. It warns if the family has a newer major version, which providers will move to: plan to
move the consumer to it during the deprecation window, while providers send both.

Use --strict to fail on warnings too.


```
jsm check-consumer [manifest] [flags]
```

### Examples

```

  jsm check-consumer
  jsm check-consumer ./service/jsm-consumer.yml -o json

```

### Options

```
  -h, --help              help for check-consumer
  -o, --output <format>   Output format (text, json) (default text)
      --strict            Fail if the family of a listed version has a newer major version
```

### Options inherited from parent commands

```
  -d, --debug             Enable debug logging
  -c, --nocolour          Disable colour in output
  -r, --registry string   path to the registry root directory (default: $JSM_REGISTRY_ROOT_DIR, or else the current directory)
```

### SEE ALSO

* [jsm](jsm.md)	 - A professional tool for managing JSON schemas

