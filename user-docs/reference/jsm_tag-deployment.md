## jsm tag-deployment

Record a successful deployment by creating a JSM deployment tag

### Synopsis


Create and push an environment-specific git tag to mark a successful deployment of schemas. 
This tag will be used as the anchor for future 'check-changes' runs to verify that 
already-deployed schemas are not modified in environments where mutations are forbidden.

If the tag cannot be pushed to origin, the command fails, since later runs elsewhere will not find it.
Use --no-push to create the tag locally only.

```
jsm tag-deployment [environment] [flags]
```

### Options

```
  -h, --help[=true|false]      help for tag-deployment
      --no-push[=true|false]   Create the deployment tag locally without pushing it to origin
```

### Options inherited from parent commands

```
  -d, --debug[=true|false]      Enable debug logging
  -c, --nocolour[=true|false]   Disable colour in output
  -r, --registry string         path to the registry root directory (default: $JSM_REGISTRY_ROOT_DIR, or else the current directory)
```

### SEE ALSO

* [jsm](jsm.md)	 - A professional tool for managing JSON schemas

