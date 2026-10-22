## jsm

A professional tool for managing JSON schemas

### Synopsis


jsm is a CLI tool for developing and testing JSON Schemas which act as bulletproof
data contracts between services in an organisation. 
Use it to craft data contracts ahead of service implementation and validate that
changes in data contracts will not break existing services.

Exit codes:
  0  success: every test and check passed
  1  a test or check failed (validate, build-dist, check-changes, check-consumer)
  2  jsm could not run (usage, configuration, schema or git error)
  130, 143  jsm was stopped by SIGINT or SIGTERM before it finished


```
jsm [flags]
```

### Options

```
  -d, --debug             Enable debug logging
  -h, --help              help for jsm
  -c, --nocolour          Disable colour in output
  -r, --registry string   path to the registry root directory (default: $JSM_REGISTRY_ROOT_DIR, or else the current directory)
```

### SEE ALSO

* [jsm build-dist](jsm_build-dist.md)	 - Build a distribution directory of rendered schemas
* [jsm check-changes](jsm_check-changes.md)	 - Check for schema changes against the latest deployment anchor for the given environment
* [jsm check-consumer](jsm_check-consumer.md)	 - Check the schema versions a consuming service validates against
* [jsm create-registry](jsm_create-registry.md)	 - Create a new JSON schema registry
* [jsm create-schema](jsm_create-schema.md)	 - Create a new JSON schema
* [jsm create-schema-version](jsm_create-schema-version.md)	 - Create the next major, minor, or patch version of the given schema family
* [jsm diff](jsm_diff.md)	 - List the structural changes from one schema version to another
* [jsm render-schema](jsm_render-schema.md)	 - Output the rendered version of a schema
* [jsm tag-deployment](jsm_tag-deployment.md)	 - Record a successful deployment by creating a JSM deployment tag
* [jsm validate](jsm_validate.md)	 - Validate one or more JSON schemas

