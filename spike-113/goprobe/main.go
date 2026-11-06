// Differential mutation, and failure-reason comparison, with the validator jsm uses.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func load(path string) any {
	b, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		panic(err)
	}
	return v
}

func compile(raw any, ptr string) *jsonschema.Schema {
	m := raw.(map[string]any)
	m["$id"] = "https://x.example/s.json"
	c := jsonschema.NewCompiler()
	if err := c.AddResource("https://x.example/s.json", m); err != nil {
		panic(err)
	}
	s, err := c.Compile("https://x.example/s.json" + ptr)
	if err != nil {
		panic(err)
	}
	return s
}

// leaves are the failure reasons: instance location and keyword path.
func leaves(err error) []string {
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return nil
	}
	var out []string
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) > 0 {
			for _, c := range e.Causes {
				walk(c)
			}
			return
		}
		kp := e.ErrorKind.KeywordPath()
		kw := ""
		if len(kp) > 0 {
			kw = kp[len(kp)-1]
		}
		out = append(out, "/"+strings.Join(e.InstanceLocation, "/")+" "+kw)
	}
	walk(ve)
	sort.Strings(out)
	return out
}

func clone(v any) any {
	b, _ := json.Marshal(v)
	var o any
	_ = json.Unmarshal(b, &o)
	return o
}

// harvest collects values from the schemas to build the value pool and property names.
func harvest(v any, pool *[]any, names map[string]bool) {
	switch t := v.(type) {
	case map[string]any:
		for k, c := range t {
			switch k {
			case "enum":
				if a, ok := c.([]any); ok {
					*pool = append(*pool, a...)
				}
			case "const":
				*pool = append(*pool, c)
			case "maximum", "minimum", "exclusiveMaximum", "exclusiveMinimum":
				if n, ok := c.(float64); ok {
					*pool = append(*pool, n, n-1, n+1)
				}
			case "maxLength", "minLength":
				if n, ok := c.(float64); ok {
					*pool = append(*pool, strings.Repeat("a", int(n)+1), strings.Repeat("a", int(n)))
				}
			case "properties":
				if m, ok := c.(map[string]any); ok {
					for n := range m {
						names[n] = true
					}
				}
			case "required":
				if a, ok := c.([]any); ok {
					for _, n := range a {
						names[n.(string)] = true
					}
				}
			}
			harvest(c, pool, names)
		}
	case []any:
		for _, c := range t {
			harvest(c, pool, names)
		}
	}
}

// mutate calls f with each single-edit mutant of root.
func mutate(root any, pool []any, names []string, f func(any)) {
	var walk func(path []any)
	walk = func(path []any) {
		cur := root
		for _, p := range path {
			switch t := cur.(type) {
			case map[string]any:
				cur = t[p.(string)]
			case []any:
				cur = t[p.(int)]
			}
		}
		set := func(val any) any {
			c := clone(root)
			if len(path) == 0 {
				return clone(val)
			}
			n := c
			for _, p := range path[:len(path)-1] {
				switch t := n.(type) {
				case map[string]any:
					n = t[p.(string)]
				case []any:
					n = t[p.(int)]
				}
			}
			last := path[len(path)-1]
			switch t := n.(type) {
			case map[string]any:
				t[last.(string)] = clone(val)
			case []any:
				t[last.(int)] = clone(val)
			}
			return c
		}
		for _, v := range pool {
			f(set(v))
		}
		switch t := cur.(type) {
		case map[string]any:
			for k := range t {
				c := clone(t).(map[string]any)
				delete(c, k)
				f(set(c))
			}
			for _, n := range append([]string{"zz", "x-a", "abcde"}, names...) {
				for _, v := range pool {
					c := clone(t).(map[string]any)
					c[n] = v
					f(set(c))
				}
			}
			for k := range t {
				walk(append(append([]any{}, path...), k))
			}
		case []any:
			for i := range t {
				walk(append(append([]any{}, path...), i))
			}
			for _, v := range pool {
				c := append(clone(t).([]any), v)
				f(set(c))
			}
		}
	}
	walk(nil)
}

func docs(dir string) []any {
	files, _ := filepath.Glob(dir + "/*.json")
	var out []any
	for _, f := range files {
		out = append(out, load(f))
	}
	return out
}

func main() {
	root := os.Args[1]
	entries, _ := os.ReadDir(root)
	fmt.Println("case | tried | widen witnesses (shortest) | narrow witnesses (shortest)")
	for _, e := range entries {
		c := filepath.Join(root, e.Name(), "shop/order/1")
		oRaw := load(c + "/0/0/shop_order_1_0_0.schema.json")
		nRaw := load(c + "/1/0/shop_order_1_1_0.schema.json")
		oS, nS := compile(oRaw, ""), compile(nRaw, "")
		pool := []any{nil, true, false, 0.0, 1.0, -1.0, 1.5, "", "s", "abcde", "nope", []any{}, []any{1.0}, []any{2.0}, []any{"a", 1.0}, map[string]any{}}
		nameSet := map[string]bool{}
		harvest(oRaw, &pool, nameSet)
		harvest(nRaw, &pool, nameSet)
		delete(nameSet, "__sentinel__")
		var names []string
		for n := range nameSet {
			names = append(names, n)
		}
		sort.Strings(names)
		seeds := append(docs(c+"/0/0/pass"), docs(c+"/1/0/pass")...)
		seen := map[string]bool{}
		tried, wide, narrow := 0, 0, 0
		var fw, fn string
		start := time.Now()
		for _, s := range seeds {
			mutate(s, pool, names, func(m any) {
				b, _ := json.Marshal(m)
				if seen[string(b)] {
					return
				}
				seen[string(b)] = true
				tried++
				oe, ne := oS.Validate(m), nS.Validate(m)
				if oe != nil && ne == nil {
					wide++
					if fw == "" || len(b) < len(fw) {
						fw = string(b)
					}
				}
				if oe == nil && ne != nil {
					narrow++
					if fn == "" || len(b) < len(fn) {
						fn = string(b)
					}
				}
			})
		}
		fmt.Printf("%s | %d %dms | %d %s | %d %s\n", e.Name(), tried, time.Since(start).Milliseconds(), wide, fw, narrow, fn)
	}

	fmt.Println("\nFAILURE REASONS: truth, old reasons, new reasons, alarm when an old reason is gone")
	type sc struct {
		name, truth string
		old, new    string
		doc         string
	}
	wrap := func(s string) any {
		var v any
		if err := json.Unmarshal([]byte(`{"$schema":"http://json-schema.org/draft-07/schema#",`+s+`}`), &v); err != nil {
			panic(err)
		}
		return v
	}
	scs := []sc{
		{"masked: enum dropped, required added", "loosened",
			`"properties":{"v":{"enum":["a"]}}`, `"properties":{"v":{}},"required":["t"]`, `{"v":"lost"}`},
		{"multi-fault: enum dropped", "loosened",
			`"properties":{"v":{"enum":["a"]},"t":{"type":"number"}}`, `"properties":{"v":{},"t":{"type":"number"}}`, `{"v":"lost","t":"x"}`},
		{"multi-fault: anyOf arm added", "loosened",
			`"properties":{"v":{"anyOf":[{"type":"string"}]},"t":{"type":"number"}}`, `"properties":{"v":{"anyOf":[{"type":"string"},{"type":"number"}]},"t":{"type":"number"}}`, `{"v":1,"t":"x"}`},
		{"same reason: maximum 10 to 20, doc 50", "loosened",
			`"properties":{"v":{"maximum":10}}`, `"properties":{"v":{"maximum":20}}`, `{"v":50}`},
		{"same reason: enum gains c, doc d", "loosened",
			`"properties":{"v":{"enum":["a"]}}`, `"properties":{"v":{"enum":["a","c"]}}`, `{"v":"d"}`},
		{"additional reason only: required added", "not loosened",
			`"properties":{"v":{"enum":["a"]}}`, `"properties":{"v":{"enum":["a"]}},"required":["t"]`, `{"v":"lost"}`},
		{"refactor: enum one value to const", "not loosened",
			`"properties":{"v":{"enum":["a"]}}`, `"properties":{"v":{"const":"a"}}`, `{"v":"lost"}`},
		{"refactor: inline to $ref", "not loosened",
			`"properties":{"v":{"enum":["a"]}}`, `"definitions":{"d":{"enum":["a"]}},"properties":{"v":{"$ref":"#/definitions/d"}}`, `{"v":"lost"}`},
		{"refactor: maxLength into allOf", "not loosened",
			`"properties":{"v":{"maxLength":1}}`, `"properties":{"v":{"allOf":[{"maxLength":1}]}}`, `{"v":"lost"}`},
		{"short-circuit: type added beside enum", "not loosened",
			`"properties":{"v":{"enum":["a",1]}}`, `"properties":{"v":{"enum":["a"],"type":"string"}}`, `{"v":true}`},
		{"short-circuit: enum added beside maxLength", "not loosened",
			`"properties":{"v":{"maxLength":1}}`, `"properties":{"v":{"maxLength":1,"enum":["a"]}}`, `{"v":"lost"}`},
		{"replaced: maxLength by pattern", "not loosened",
			`"properties":{"v":{"maxLength":1}}`, `"properties":{"v":{"pattern":"^.$"}}`, `{"v":"lost"}`},
	}
	for _, s := range scs {
		o, n := wrap(s.old), wrap(s.new)
		var d any
		_ = json.Unmarshal([]byte(s.doc), &d)
		ol, nl := leaves(compile(o, "").Validate(d)), leaves(compile(n, "").Validate(d))
		alarm := false
		for _, l := range ol {
			found := false
			for _, m := range nl {
				found = found || l == m
			}
			alarm = alarm || !found
		}
		fmt.Printf("%-44s truth=%-13s old=%v new=%v alarm=%v\n", s.name, s.truth, ol, nl, alarm)
	}

	fmt.Println("\nSEMANTIC USE TEST: is the value at /v rejected by the older subschema?")
	old := wrap(`"properties":{"v":{"anyOf":[{"type":"string"}]}}`)
	sub := compile(old, "#/properties/v")
	for _, v := range []any{"b", 1.0} {
		fmt.Printf("value %v rejected by old #/properties/v: %v\n", v, sub.Validate(v) != nil)
	}
}
