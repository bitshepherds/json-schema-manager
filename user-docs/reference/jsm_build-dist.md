## jsm build-dist

Build a distribution directory of rendered schemas

### Synopsis


Build rendered schemas for the specified environment and write them to [repo root]/dist/[env].

Before building, it runs every schema's tests and compatibility check, as
'jsm validate all' does. If any fail, it prints the report, builds nothing and exits
with code 1. Use --skip-tests if your pipeline already runs validate.

By default, this command only builds schemas that have been added or modified since 
the last successful deployment to that environment. A mutation check is performed 
first to ensure that existing schemas haven't been modified in environments where 
mutations are forbidden.

It finds them with git, so it leaves out schemas which git does not track yet, and
lists them: commit them to include them. In CI, every schema is committed.

If the --all (-a) flag is used, all schemas in the registry are rendered and written 
to the dist directory, skipping the mutation check.

If the --collapse flag is used, external $ref references to other JSM-managed
schemas in the same registry are resolved and inlined into a $defs block within each
schema. This produces self-contained schemas that can be used without loading their
dependencies. The collapse is recursive: transitive dependencies are also inlined.
Each collapsed schema is compiled on its own, and its pass and fail test documents
must give the same results as with the original, or the command fails.

WARNING: Using the --all (-a) flag is NOT recommended in a deployment pipeline, as it 
bypasses safety checks and may deploy unintended changes. It is primarily intended 
for local troubleshooting or manual overrides.

The latest deployment anchor is the newest jsm-deploy/[env]/* tag in the history of HEAD, created by
'jsm tag-deployment'. This needs the history and tags back to that deployment: in GitHub Actions, check out
with 'fetch-depth: 0'. In a shallow clone without the tag, the command fails rather than guess. With no
deployment tag at all, every schema is treated as new.

```
jsm build-dist [environment] [flags]
```

### Options

```
  -a, --all          Render and build all schemas, skipping mutation checks
      --collapse     Inline external $ref dependencies into each schema's $defs block
  -h, --help         help for build-dist
      --skip-tests   Build without first running the schemas' tests and compatibility check
```

### Options inherited from parent commands

```
  -d, --debug             Enable debug logging
  -c, --nocolour          Disable colour in output
  -r, --registry string   path to the registry root directory (default: $JSM_REGISTRY_ROOT_DIR, or else the current directory)
```

### SEE ALSO

* [jsm](jsm.md)	 - A professional tool for managing JSON schemas

