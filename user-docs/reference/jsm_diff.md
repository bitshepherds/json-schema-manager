## jsm diff

List the structural changes from one schema version to another

### Synopsis


List the structural changes from one schema version to another, such as enum values added,
types allowed, properties added, and constraints relaxed or tightened. Each change is marked:

  widens   the newer version accepts documents which the older one rejects
  narrows  the newer version rejects documents which the older one accepts
  alters   the change may widen what the schema accepts, and JSM cannot tell

A change which widens or alters a schema may break consumers of the older version. For each, the
newer version's pass documents which use it are listed: those with a value there which the older
version rejects or, for a change where JSM cannot tell which values those are, any value there.
'jsm validate' warns about such a change in a version when none of its pass documents uses it,
since its compatibility check cannot then tell whether the change breaks consumers.

The comparison is structural: it follows local $refs, and compares other schemas' $refs by URL.


```
jsm diff [older] [newer] [flags]
```

### Examples

```

  jsm diff shop_order_1_0_0 shop_order_1_1_0
  jsm diff "./shop/order/1/0/0/shop_order_1_0_0.schema.json" "./shop/order/1/1/0/shop_order_1_1_0.schema.json"

```

### Options

```
  -h, --help   help for diff
```

### Options inherited from parent commands

```
  -d, --debug             Enable debug logging
  -c, --nocolour          Disable colour in output
  -r, --registry string   path to the registry root directory (default: $JSM_REGISTRY_ROOT_DIR, or else the current directory)
```

### SEE ALSO

* [jsm](jsm.md)	 - A professional tool for managing JSON schemas

