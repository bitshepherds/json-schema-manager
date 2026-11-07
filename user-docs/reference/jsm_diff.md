## jsm diff

List the structural changes from one schema version to another

### Synopsis


List the structural changes from one schema version to another, such as enum values added,
types allowed, properties added, and constraints relaxed or tightened. Every change is listed,
whatever the family's scenario. Each is marked:

  widens   the newer version accepts documents which the older one rejects
  narrows  the newer version rejects documents which the older one accepts
  alters   JSM cannot tell which way the change goes: it may widen, narrow, or do both

Notes beside a change name the documents which show it, from any version after the older one up to
the newer one, and flag a direction the family's scenario protects which no document shows:

  used by pass/x.json        this pass document has a value there which the older version rejects
  shown by fail/y.json       this fail document has a value there which the older version accepts,
                             and the newer one rejects
  no pass document uses it   flagged in a writer-ahead or either family, for a change which widens
                             or alters
  no fail document shows it  flagged in a reader-ahead or either family, for a change which narrows
                             or alters

A family with neither side ahead flags nothing. Nor does a pair which validate doesn't compare, such
as two majors or a version below oldestLive, and only the newer version's documents are named there.

Where a change has a precise test, such as an enum value added, that test decides whether a pass
document uses it. Otherwise the older version's subschema at that location decides.

'jsm validate' warns about what these notes flag, under the version which made the change. This
command doesn't read compat-exceptions.yml, so it still flags a change an entry there acknowledges,
which validate no longer warns about: what it says stays true, that no document shows the change.

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
  -h, --help[=true|false]   help for diff
```

### Options inherited from parent commands

```
  -d, --debug[=true|false]      Enable debug logging
  -c, --nocolour[=true|false]   Disable colour in output
  -r, --registry string         path to the registry root directory (default: the current directory, if it holds a registry, or else $JSM_REGISTRY_ROOT_DIR)
```

### SEE ALSO

* [jsm](jsm.md)	 - A professional tool for managing JSON schemas

