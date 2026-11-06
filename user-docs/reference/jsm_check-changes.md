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
  - changing or removing the family.yml of a family with a deployed version, other than moving its
    oldestLive forward: a changed scenario means a new family
  - editing or removing an entry of a compat-exceptions.yml which was there at the deployment, unless
    the version or document it names has gone, or the version has fallen below oldestLive
  - deleting a deployed version when documents written under it may still exist: any version if the
    family's documentsLive is indefinitely, or a version at or above oldestLive if it is retention
Adding new schemas and test documents is allowed, and so is deleting a whole version if its documents
can't still exist: any version if documentsLive is moment, or one below oldestLive if it is retention.
A family with no family.yml may gain one. A family's versions going all at once are judged as above, by the
family.yml they were deployed with. A deployed version may gain a compat-exceptions.yml, or new entries in
one, and a family.yml may gain or change migration statements.

In every environment, the check fails if any family has no family.yml, or an invalid one.

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

