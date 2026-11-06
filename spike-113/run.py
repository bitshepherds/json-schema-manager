#!/usr/bin/env python3
"""Builds one registry per change kind, and runs jsm against it.

Every schema carries the same root `not: {required: [__sentinel__]}`, so each version has an own
fail document unrelated to the change, and --strict does not warn about a missing one.
"""
import json, os, shutil, subprocess

P = os.path.dirname(os.path.abspath(__file__))
JSM = P + "/jsm"
ROOT = P + "/cases"
D7 = "http://json-schema.org/draft-07/schema#"
D20 = "https://json-schema.org/draft/2020-12/schema"
CFG = '''environments:
  production:
    privateUrlRoot: "https://json-schemas.internal.myorg.io/"
    publicUrlRoot: "https://json-schemas.myorg.io/"
    isProduction: true
'''


def obj(**props):
    return {"type": "object", "properties": props}


REQ_V = {"required": ["v"]}
# name, old, new, old pass docs, old fail docs, new's own pass docs, optional drafts (old, new)
CASES = [
    ("enum_add", obj(v={"enum": ["a", "b"]}), obj(v={"enum": ["a", "b", "c"]}), [{"v": "a"}], [{"v": "c"}], [{"v": "a"}]),
    ("enum_remove", obj(v={"enum": ["a", "b"]}), obj(v={"enum": ["a"]}), [{"v": "b"}], [{"v": "c"}], [{"v": "a"}]),
    ("required_drop", {**obj(v={"type": "string"}), **REQ_V}, obj(v={"type": "string"}), [{"v": "a"}], [{}], [{"v": "a"}]),
    ("required_add", obj(v={"type": "string"}), {**obj(v={"type": "string"}), **REQ_V}, [{}], [{"v": 1}], [{"v": "a"}]),
    ("anyOf_arm_add", obj(v={"anyOf": [{"type": "string"}]}), obj(v={"anyOf": [{"type": "string"}, {"type": "number"}]}), [{"v": "a"}], [{"v": 1}], [{"v": "b"}]),
    ("anyOf_arm_add_root", {"anyOf": [{"required": ["a"]}]}, {"anyOf": [{"required": ["a"]}, {"required": ["b"]}]}, [{"a": 1}], [{"b": 1}], [{"a": 2}]),
    ("anyOf_arm_drop", obj(v={"anyOf": [{"type": "string"}, {"type": "number"}]}), obj(v={"anyOf": [{"type": "string"}]}), [{"v": 1}], [{"v": True}], [{"v": "a"}]),
    ("anyOf_arm_reorder", obj(v={"anyOf": [{"type": "string"}, {"type": "number"}]}), obj(v={"anyOf": [{"type": "number"}, {"type": "string"}]}), [{"v": 1}], [{"v": True}], [{"v": "a"}]),
    ("oneOf_arm_constraint_add", obj(v={"oneOf": [{"type": "number"}, {"minimum": 0}]}), obj(v={"oneOf": [{"type": "number"}, {"minimum": 0, "maximum": 10}]}), [{"v": -1}, {"v": "s"}], [{"v": 50}], [{"v": "t"}]),
    ("oneOf_arm_drop_overlap", obj(v={"oneOf": [{"type": "number"}, {"minimum": 0}]}), obj(v={"oneOf": [{"type": "number"}]}), [{"v": -1}], [{"v": 50}], [{"v": -2}]),
    ("oneOf_arm_add_overlap", obj(v={"oneOf": [{"type": "number"}]}), obj(v={"oneOf": [{"type": "number"}, {"minimum": 0}]}), [{"v": 5}], [{"v": "s"}], [{"v": -5}]),
    ("oneOf_arm_relax", obj(v={"oneOf": [{"type": "string", "maxLength": 2}, {"type": "number"}]}), obj(v={"oneOf": [{"type": "string", "maxLength": 9}, {"type": "number"}]}), [{"v": "ab"}], [{"v": "abcde"}], [{"v": "ab"}]),
    ("allOf_refactor_neutral", obj(v={"maxLength": 5}), obj(v={"allOf": [{"maxLength": 5}]}), [{"v": "a"}], [{"v": "aaaaaaa"}], [{"v": "a"}]),
    ("not_inner_constraint_add", obj(v={"not": {"type": "string"}}), obj(v={"not": {"type": "string", "maxLength": 2}}), [{"v": 1}], [{"v": "abcde"}], [{"v": 2}]),
    ("not_inner_constraint_add_noprop", obj(v={"not": {"type": "string"}}), obj(v={"not": {"type": "string", "maxLength": 2}}), [{"v": 1}], [{"v": "abcde"}], [{}]),
    ("if_then_inner_relax", {"if": REQ_V, "then": {"properties": {"v": {"maxLength": 2}}}}, {"if": REQ_V, "then": {"properties": {"v": {"maxLength": 9}}}}, [{"v": "x"}], [{"v": "abcde"}], [{"v": "x"}]),
    ("if_cond_change", {"if": {"required": ["a"]}, "then": {"required": ["b"]}}, {"if": {"required": ["a", "c"]}, "then": {"required": ["b"]}}, [{"a": 1, "b": 1}], [{"a": 1}], [{"a": 1, "b": 1}]),
    ("pattern_change_widen", obj(v={"pattern": "^a"}), obj(v={"pattern": "^[ab]"}), [{"v": "a"}], [{"v": "b"}], [{"v": "a"}]),
    ("pattern_change_tighten", obj(v={"pattern": "^[ab]"}), obj(v={"pattern": "^a"}), [{"v": "b"}], [{"v": "c"}], [{"v": "a"}]),
    ("max_relax", obj(v={"maximum": 10}), obj(v={"maximum": 20}), [{"v": 1}], [{"v": 15}], [{"v": 1}]),
    ("min_to_exclusive", obj(v={"minimum": 0}), obj(v={"exclusiveMinimum": 0}), [{"v": 1}], [{"v": -1}], [{"v": 1}]),
    ("addlProps_true", {**obj(v={}), "additionalProperties": False}, obj(v={}), [{"v": 1}], [{"x": 1}], [{"v": 1}]),
    ("addlProps_new_prop", {**obj(v={}), "additionalProperties": False}, {**obj(v={}, w={}), "additionalProperties": False}, [{"v": 1}], [{"w": 1}], [{"v": 1}]),
    ("addlProps_schema_relax", {**obj(v={}), "additionalProperties": {"type": "string"}}, {**obj(v={}), "additionalProperties": {"type": ["string", "number"]}}, [{"x": "s"}], [{"x": 1}], [{"x": "s"}]),
    ("patternProps_relax", {"patternProperties": {"^x-": {"type": "string"}}}, {"patternProperties": {"^x-": {"type": ["string", "number"]}}}, [{"x-a": "s"}], [{"x-a": 1}], [{"x-a": "s"}]),
    ("patternProps_drop", {"patternProperties": {"^x-": {"type": "string"}}}, {"type": "object"}, [{"x-a": "s"}], [{"x-a": 1}], [{"x-a": "s"}]),
    ("local_ref_target_relax", {**obj(v={"$ref": "#/definitions/d"}), "definitions": {"d": {"maximum": 10}}}, {**obj(v={"$ref": "#/definitions/d"}), "definitions": {"d": {"maximum": 20}}}, [{"v": 1}], [{"v": 15}], [{"v": 1}]),
    ("ref_sibling_d7", {**obj(v={"$ref": "#/definitions/d", "maximum": 1}), "definitions": {"d": {"type": "number"}}}, {**obj(v={"$ref": "#/definitions/d", "maximum": 9}), "definitions": {"d": {"type": "number"}}}, [{"v": 5}], [{"v": "s"}], [{"v": 5}]),
    ("ref_sibling_2020", {**obj(v={"$ref": "#/$defs/d", "maximum": 1}), "$defs": {"d": {"type": "number"}}}, {**obj(v={"$ref": "#/$defs/d", "maximum": 9}), "$defs": {"d": {"type": "number"}}}, [{"v": 1}], [{"v": 5}], [{"v": 1}], (D20, D20)),
    ("prop_constraint_removed", obj(v={"type": "string"}), obj(), [{"v": "a"}], [{"v": 1}], [{"v": "a"}]),
    ("contains_relax", obj(v={"contains": {"const": 1}}), obj(v={"contains": {"enum": [1, 2]}}), [{"v": [1]}], [{"v": [2]}], [{"v": [1]}]),
    ("propertyNames_relax", {"propertyNames": {"maxLength": 2}}, {"propertyNames": {"maxLength": 9}}, [{"a": 1}], [{"abcde": 1}], [{"a": 1}]),
    ("format_drop_d7", obj(v={"type": "string", "format": "email"}), obj(v={"type": "string"}), [{"v": "a@b.co"}], [{"v": "nope"}], [{"v": "a@b.co"}]),
    ("draft_change_format", obj(v={"format": "email"}), obj(v={"format": "email"}), [{"v": "a@b.co"}], [{"v": "nope"}], [{"v": "a@b.co"}], (D7, D20)),
    ("additionalItems_drop_d7", obj(v={"items": [{"type": "string"}], "additionalItems": False}), obj(v={"items": [{"type": "string"}]}), [{"v": ["a"]}], [{"v": ["a", 1]}], [{"v": ["a"]}]),
    ("default_change", obj(v={"type": "number", "default": 10}), obj(v={"type": "number", "default": 20}), [{"v": 1}], [{"v": "s"}], [{"v": 1}]),
    ("description_units", obj(v={"type": "number", "description": "dollars"}), obj(v={"type": "number", "description": "cents"}), [{"v": 1}], [{"v": "s"}], [{"v": 1}]),
    ("masked_loosen_plus_required", obj(v={"enum": ["a"]}, t={}), {**obj(v={}, t={}), "required": ["t"]}, [{"v": "a"}], [{"v": "lost"}], [{"t": 1, "v": "a"}]),
    ("multifault_fail_doc", obj(v={"enum": ["a"]}, t={"type": "number"}), obj(v={}, t={"type": "number"}), [{"v": "a"}], [{"v": "lost", "t": "x"}], [{"v": "a"}]),
    ("masked_anyof_multifault", obj(v={"anyOf": [{"type": "string"}]}, t={"type": "number"}), obj(v={"anyOf": [{"type": "string"}, {"type": "number"}]}, t={"type": "number"}), [{"v": "a"}], [{"v": 1, "t": "x"}], [{"v": "b"}]),
    ("oneOf_disjoint_arm_constraint_add", obj(v={"oneOf": [{"type": "string"}, {"type": "number"}]}), obj(v={"oneOf": [{"type": "string"}, {"type": "number", "maximum": 10}]}), [{"v": 50}], [{"v": True}], [{"v": "s"}]),
    ("anyOf_arm_constraint_add", obj(v={"anyOf": [{"type": "number"}, {"minimum": 0}]}), obj(v={"anyOf": [{"type": "number", "maximum": 10}, {"minimum": 0}]}), [{"v": 5}], [{"v": True, "x": 1}], [{"v": 5}]),
    ("untouched_region_tighten", obj(v={"type": "string"}, w={"type": "number", "maximum": 10}), obj(v={"type": "string"}, w={"type": "number", "maximum": 5}), [{"v": "a"}], [{"v": 1}], [{"v": "b"}]),
]

SENTINEL = {"not": {"required": ["__sentinel__"]}}


def write(p, o):
    os.makedirs(os.path.dirname(p), exist_ok=True)
    with open(p, "w") as f:
        json.dump(o, f, indent=1)


def sch(body, draft):
    if "not" in body:
        out = {"$schema": draft, "$id": "{{ ID }}", "allOf": [SENTINEL], **body}
    else:
        out = {"$schema": draft, "$id": "{{ ID }}", **SENTINEL, **body}
    return out


def build(root, c, cross):
    name, old, new, op, of, np = c[:6]
    do, dn = c[6] if len(c) > 6 else (D7, D7)
    shutil.rmtree(root, ignore_errors=True)
    os.makedirs(root)
    with open(root + "/json-schema-manager-config.yml", "w") as f:
        f.write(CFG)
    v0, v1 = root + "/shop/order/1/0/0", root + "/shop/order/1/1/0"
    write(v0 + "/shop_order_1_0_0.schema.json", sch(old, do))
    write(v1 + "/shop_order_1_1_0.schema.json", sch(new, dn))
    for i, d in enumerate(op):
        write(f"{v0}/pass/p{i}.json", d)
    for i, d in enumerate(of):
        write(f"{v0}/fail/f{i}.json", d)
    write(f"{v0}/fail/sentinel.json", {"__sentinel__": 1})
    for i, d in enumerate(np):
        write(f"{v1}/pass/own{i}.json", d)
    write(f"{v1}/fail/sentinel.json", {"__sentinel__": 1})
    if cross == "fail":
        for i, d in enumerate(of):
            write(f"{v1}/fail/old{i}.json", d)
    if cross == "pass":
        for i, d in enumerate(op):
            write(f"{v1}/pass/old{i}.json", d)


def run(root, *args):
    e = dict(os.environ, JSM_REGISTRY_ROOT_DIR=root, JSM_LOG_FILE=P + "/jsm-run.log", NO_COLOR="1")
    r = subprocess.run([JSM, *args], capture_output=True, text=True, env=e, cwd=P)
    return r.returncode, r.stdout + r.stderr


K = "shop_order_1_1_0"
results = {}
for c in CASES:
    root = f"{ROOT}/{c[0]}"
    build(root, c, None)
    dc, dout = run(root, "diff", "shop_order_1_0_0", K)
    sc, sout = run(root, "validate", K, "-c", "-C", "--strict")
    build(root, c, "fail")
    fc, fout = run(root, "validate", K, "-c", "-C", "--skip-compatible")
    build(root, c, "pass")
    pc, pout = run(root, "validate", K, "-c", "-C", "--skip-compatible")
    build(root, c, None)
    results[c[0]] = dict(diff=dc, strict=sc, oldfail=fc, oldpass=pc, diffout=dout)
    print(f"## {c[0]}: strict={sc} oldfail-on-new={fc} oldpass-on-new={pc} diff-exit={dc}")
    for line in dout.splitlines():
        if not line.startswith("Changes from"):
            print("   ", line.strip())
    if sc not in (0, 1) or dc != 0:
        print(sout)
    elif sc == 1 and "unexercised" not in sout:
        print("    STRICT-FAIL:", [l for l in sout.splitlines() if "fail" in l.lower()][:3])
with open(P + "/results.json", "w") as f:
    json.dump(results, f, indent=1)
