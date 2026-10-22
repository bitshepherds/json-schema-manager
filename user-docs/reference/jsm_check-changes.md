## jsm check-changes

Check for schema changes against the latest deployment anchor for the given environment

### Synopsis


Check for schema changes against the latest deployment anchor for the given environment.
Use this command in a CI/CD pipeline to prevent deploying a schema change to an environment which does not permit
schema mutation. Your production environment should never permit schema mutation, but it is recommended to allow 
mutations in dev environments to allow teams to iterate on a new schema together before the contract is locked down.

In an environment which does not permit schema mutation, these changes since the deployment fail the check:
  - modifying a deployed schema file
  - modifying or deleting a pass/ or fail/ test document of a deployed version
  - changing the environment's privateUrlRoot or publicUrlRoot, which give deployed schemas their IDs
Adding new schemas and test documents is allowed, and so is deleting a whole version.

The latest deployment anchor is the newest jsm-deploy/[env]/* tag in the history of HEAD, created by
'jsm tag-deployment'. This needs the history and tags back to that deployment: in GitHub Actions, check out
with 'fetch-depth: 0'. In a shallow clone without the tag, the command fails rather than guess. With no
deployment tag at all, every schema is treated as new.

```
jsm check-changes [environment] [flags]
```

### Options

```
  -h, --help   help for check-changes
```

### Options inherited from parent commands

```
  -d, --debug             Enable debug logging
  -c, --nocolour          Disable colour in output
  -r, --registry string   path to the registry root directory (default: $JSM_REGISTRY_ROOT_DIR, or else the current directory)
```

### SEE ALSO

* [jsm](jsm.md)	 - A professional tool for managing JSON schemas

