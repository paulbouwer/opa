// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/types"
	"github.com/open-policy-agent/opa/util"
	"github.com/open-policy-agent/opa/util/test"
)

func TestCheckInference(t *testing.T) {

	// fake_builtin_1([str1,str2])
	RegisterBuiltin(&Builtin{
		Name: String("fake_builtin_1"),
		Args: []types.Type{
			types.NewArray(
				[]types.Type{types.S, types.S}, nil,
			),
		},
		TargetPos: []int{0},
	})

	// fake_builtin_2({"a":str1,"b":str2})
	RegisterBuiltin(&Builtin{
		Name: String("fake_builtin_2"),
		Args: []types.Type{
			types.NewObject(
				[]*types.Property{
					{"a", types.S},
					{"b", types.S},
				}, nil,
			),
		},
		TargetPos: []int{0},
	})

	// fake_builtin_3({str1,str2,...})
	RegisterBuiltin(&Builtin{
		Name: String("fake_builtin_3"),
		Args: []types.Type{
			types.NewSet(types.S),
		},
		TargetPos: []int{0},
	})

	tests := []struct {
		note     string
		query    string
		expected map[Var]types.Type
	}{
		{"trivial", `x = 1`, map[Var]types.Type{
			Var("x"): types.N,
		}},
		{"one-level", "y = 1; x = y", map[Var]types.Type{
			Var("x"): types.N,
			Var("y"): types.N,
		}},
		{"two-level", "z = 1; y = z; x = y", map[Var]types.Type{
			Var("x"): types.N,
			Var("y"): types.N,
			Var("z"): types.N,
		}},
		{"array-nested", "[x, 1] = [true, y]", map[Var]types.Type{
			Var("x"): types.B,
			Var("y"): types.N,
		}},
		{"array-transitive", "y = [[2], 1]; [[x], 1] = y", map[Var]types.Type{
			Var("x"): types.N,
			Var("y"): types.NewArray(
				[]types.Type{
					types.NewArray([]types.Type{types.N}, nil),
					types.N,
				}, nil),
		}},
		{"array-embedded", `[1, "2", x] = data.foo`, map[Var]types.Type{
			Var("x"): types.A,
		}},
		{"object-nested", `{"a": "foo", "b": {"c": x}} = {"a": y, "b": {"c": 2}}`, map[Var]types.Type{
			Var("x"): types.N,
			Var("y"): types.S,
		}},
		{"object-transitive", `y = {"a": "foo", "b": 2}; {"a": z, "b": x} = y`, map[Var]types.Type{
			Var("x"): types.N,
			Var("z"): types.S,
		}},
		{"object-embedded", `{"1": "2", "2": x} = data.foo`, map[Var]types.Type{
			Var("x"): types.A,
		}},
		{"sets", `x = {1, 2}; y = {{"foo", 1}, x}`, map[Var]types.Type{
			Var("x"): types.NewSet(types.N),
			Var("y"): types.NewSet(
				types.NewAny(
					types.NewSet(
						types.NewAny(types.N, types.S),
					),
					types.NewSet(
						types.N,
					),
				),
			),
		}},
		{"sets-nested", `{"a", 1, 2} = {1,2,3}`, nil},
		{"empty-composites", `
				obj = {};
				arr = [];
				set = set();
				obj[i] = v1;
				arr[j] = v2;
				set[v3];
				obj = {"foo": "bar"}
				arr = [1];
				set = {1,2,3}
				`, map[Var]types.Type{
			Var("obj"): types.NewObject(nil, types.A),
			Var("i"):   types.S,
			Var("v1"):  types.A,
			Var("arr"): types.NewArray(nil, types.A),
			Var("j"):   types.N,
			Var("v2"):  types.A,
			Var("set"): types.NewSet(types.A),
			Var("v3"):  types.A,
		}},
		{"empty-composite-property", `
			obj = {};
			obj.foo = x;
			obj[i].foo = y
		`, map[Var]types.Type{
			Var("x"): types.A,
			Var("y"): types.A,
		}},
		{"local-reference", `
			a = [
				1,
				{
					"foo": [
						{"bar": null},
						-1,
						{"bar": true}
					]
				},
				3];

			x = a[1].foo[_].bar`, map[Var]types.Type{
			Var("x"): types.NewAny(types.NewNull(), types.B),
		}},
		{"local-reference-var", `

			a = [
				{
					"a": null,
					"b": {
						"foo": {
							"c": {1,},
						},
						"bar": {
							"c": {"hello",},
						},
					},
				},
				{
					"a": null,
					"b": {
						"foo": {
							"c": {1,},
						},
						"bar": {
							"c": {true,},
						},
					},
				},
			];
			x = a[i].b[j].c[k]
			`, map[Var]types.Type{
			Var("i"): types.N,
			Var("j"): types.S,
			Var("k"): types.NewAny(types.S, types.N, types.B),
			Var("x"): types.NewAny(types.S, types.N, types.B),
		}},
		{"local-reference-var-any", `
			a = [[], {}];
			a[_][i]
		`, map[Var]types.Type{
			Var("i"): types.NewAny(types.S, types.N),
		}},
		{"local-reference-nested", `
			a = [["foo"], 0, {"bar": "baz"}, 2];
			b = [0,1,2,3];
			a[b[_]][k] = v
			`, map[Var]types.Type{
			Var("k"): types.NewAny(types.S, types.N),
		}},
		{"simple-built-in", "x = 1 + 2", map[Var]types.Type{
			Var("x"): types.N,
		}},
		{"simple-built-in-exists", "x = 1 + 2; y = x + 2", map[Var]types.Type{
			Var("x"): types.N,
			Var("y"): types.N,
		}},
		{"array-builtin", `fake_builtin_1([x,"foo"])`, map[Var]types.Type{
			Var("x"): types.S,
		}},
		{"object-builtin", `fake_builtin_2({"a": "foo", "b": x})`, map[Var]types.Type{
			Var("x"): types.S,
		}},
		{"set-builtin", `fake_builtin_3({"foo", x})`, map[Var]types.Type{
			Var("x"): types.S,
		}},
		{"array-comprehension-ref-closure", `a = [1,"foo",3]; x = [ i | a[_] = i ]`, map[Var]types.Type{
			Var("x"): types.NewArray(nil, types.NewAny(types.N, types.S)),
		}},
		{"array-comprehension-var-closure", `x = 1; y = [ i | x = i ]`, map[Var]types.Type{
			Var("y"): types.NewArray(nil, types.N),
		}},
	}

	for _, tc := range tests {
		test.Subtest(t, tc.note, func(t *testing.T) {
			body := MustParseBody(tc.query)
			checker := newTypeChecker()
			env := checker.checkLanguageBuiltins()
			env, err := checker.CheckBody(env, body)
			if len(err) != 0 {
				t.Fatalf("Unexpected error: %v", err)
			}
			for k, tpe := range tc.expected {
				result := env.Get(k)
				if tpe == nil {
					if result != nil {
						t.Errorf("Expected %v type to be unset but got: %v", k, result)
					}
				} else {
					if result == nil {
						t.Errorf("Expected to infer %v => %v but got nil", k, tpe)
					} else if types.Compare(tpe, result) != 0 {
						t.Errorf("Expected to infer %v => %v but got %v", k, tpe, result)
					}
				}
			}
		})
	}
}

func TestCheckInferenceRules(t *testing.T) {

	// Rules must have refs resolved, safe ordering, etc. Each pair is a
	// (package path, rule) tuple. The test constructs the Rule objects to
	// run the inference on from these inputs.
	ruleset1 := [][2]string{
		{`a`, `trivial = true { true }`},
		{`a`, `complete = [{"foo": x}] { x = 1 }`},
		{`a`, `partialset[{"foo": x}] { y = "bar"; x = y }`},
		{`a`, `partialobj[x] = {"foo": y} { y = "bar"; x = y }`},
		{`b`, `trivial_ref = x { x = data.a.trivial }`},
		{`b`, `transitive_ref = [x] { y = data.b.trivial_ref; x = y }`},
		{`c`, `else_kw = null { false } else = 100 { true } else = "foo" { true }`},
		{`iteration`, `arr = [[1], ["two"], {"x": true}, ["four"]] { true }`},
		{`iteration`, `values[x] { data.iteration.arr[_][_] = x } `},
		{`iteration`, `keys[i] { data.iteration.arr[_][i] = _ } `},
		{`disjunction`, `partialset[1] { true }`},
		{`disjunction`, `partialset[x] { x = "foo" }`},
		{`disjunction`, `partialset[3] { true }`},
		{`disjunction`, `partialobj[x] = y { y = "bar"; x = "foo" }`},
		{`disjunction`, `partialobj[x] = y { y = 100; x = "foo" }`},
		{`disjunction`, `complete = 1 { true }`},
		{`disjunction`, `complete = x { x = "foo" }`},
		{`prefix.a.b.c`, `d = true { true }`},
		{`prefix.i.j.k`, `p = 1 { true }`},
		{`prefix.i.j.k`, `p = "foo" { true }`},
		{`default_rule`, `default x = 1`},
		{`default_rule`, `x = "foo" { true }`},
		{`unknown_type`, `p = [x] { x = data.deadbeef }`},
		{`nested_ref`, `inner = {"a": 0, "b": "1"} { true }`},
		{`nested_ref`, `middle = [[1, true], ["foo", false]] { true }`},
		{`nested_ref`, `p = x { data.nested_ref.middle[data.nested_ref.inner.a][0] = x }`},
		{`number_key`, `q[x] = y { a = ["a", "b"]; y = a[x] }`},
		{`non_leaf`, `p[x] { data.prefix.i[x][_] }`},
	}

	tests := []struct {
		note     string
		rules    [][2]string
		ref      string
		expected types.Type
	}{
		{"trivial", ruleset1, `data.a.trivial`, types.B},

		{"complete-doc", ruleset1, `data.a.complete`, types.NewArray(
			[]types.Type{types.NewObject(
				[]*types.Property{{
					"foo", types.N,
				}},
				nil,
			)},
			nil,
		)},

		{"complete-doc-suffix", ruleset1, `data.a.complete[0].foo`, types.N},

		{"else-kw", ruleset1, "data.c.else_kw", types.NewAny(types.NewNull(), types.N, types.S)},

		{"partial-set-doc", ruleset1, `data.a.partialset`, types.NewSet(
			types.NewObject(
				[]*types.Property{{
					"foo", types.S,
				}},
				nil,
			),
		)},

		{"partial-object-doc", ruleset1, "data.a.partialobj", types.NewObject(
			nil,
			types.NewObject(
				[]*types.Property{{
					"foo", types.S,
				}},
				nil,
			),
		)},

		{"partial-object-doc-suffix", ruleset1, `data.a.partialobj.somekey.foo`, types.S},

		{"partial-object-doc-number-suffix", ruleset1, "data.number_key.q[1]", types.S},

		{"iteration", ruleset1, "data.iteration.values", types.NewSet(
			types.NewAny(
				types.S,
				types.N,
				types.B),
		)},

		{"iteration-keys", ruleset1, "data.iteration.keys", types.NewSet(
			types.NewAny(
				types.S,
				types.N,
			),
		)},

		{"disj-complete-doc", ruleset1, "data.disjunction.complete", types.NewAny(
			types.S,
			types.N,
		)},

		{"disj-partial-set-doc", ruleset1, "data.disjunction.partialset", types.NewSet(
			types.NewAny(
				types.S,
				types.N),
		)},

		{"disj-partial-obj-doc", ruleset1, "data.disjunction.partialobj", types.NewObject(
			nil,
			types.NewAny(
				types.S,
				types.N),
		)},

		{"ref", ruleset1, "data.b.trivial_ref", types.B},

		{"ref-transitive", ruleset1, "data.b.transitive_ref", types.NewArray(
			[]types.Type{
				types.B,
			},
			nil,
		)},

		{"prefix", ruleset1, `data.prefix.a.b`, types.NewObject(
			[]*types.Property{{
				"c", types.NewObject(
					[]*types.Property{{
						"d", types.B,
					}},
					types.A,
				),
			}},
			types.A,
		)},

		// Check that prefixes that iterate fallback to any.
		{"prefix-iter", ruleset1, `data.prefix.i.j[k]`, types.A},

		// Check that iteration targetting a rule (but nonetheless prefixed) falls back to any.
		{"prefix-iter-2", ruleset1, `data.prefix.i.j[k].p`, types.A},

		{"default-rule", ruleset1, "data.default_rule.x", types.NewAny(
			types.S,
			types.N,
		)},

		{"unknown-type", ruleset1, "data.unknown_type.p", types.NewArray(
			[]types.Type{
				types.A,
			},
			nil,
		)},

		{"nested-ref", ruleset1, "data.nested_ref.p", types.NewAny(
			types.S,
			types.N,
		)},

		{"non-leaf", ruleset1, "data.non_leaf.p", types.NewSet(
			types.S,
		)},
	}

	for _, tc := range tests {
		test.Subtest(t, tc.note, func(t *testing.T) {
			var elems []util.T

			// Convert test rules into rule slice for call.
			for i := range tc.rules {
				pkg := MustParsePackage(`package ` + tc.rules[i][0])
				rule := MustParseRule(tc.rules[i][1])
				module := &Module{
					Package: pkg,
					Rules:   []*Rule{rule},
				}
				rule.Module = module
				elems = append(elems, rule)
				for next := rule.Else; next != nil; next = next.Else {
					next.Module = module
					elems = append(elems, next)
				}
			}

			ref := MustParseRef(tc.ref)
			checker := newTypeChecker()
			env, err := checker.CheckTypes(nil, elems)

			if err != nil {
				t.Fatalf("Unexpected error %v:", err)
			}

			result := env.Get(ref)
			if tc.expected == nil {
				if result != nil {
					t.Errorf("Expected %v type to be unset but got: %v", ref, result)
				}
			} else {
				if result == nil {
					t.Errorf("Expected to infer %v => %v but got nil", ref, tc.expected)
				} else if types.Compare(tc.expected, result) != 0 {
					t.Errorf("Expected to infer %v => %v but got %v", ref, tc.expected, result)
				}
			}
		})
	}

}

func TestCheckErrorSuppression(t *testing.T) {

	query := `arr = [1,2,3]; arr[0].deadbeef = 1`

	_, errs := newTypeChecker().CheckBody(nil, MustParseBody(query))
	if len(errs) != 1 {
		t.Fatalf("Expected exactly one error but got: %v", errs)
	}

	_, ok := errs[0].Details.(*RefErrUnsupportedDetail)
	if !ok {
		t.Fatalf("Expected ref error but got: %v", errs)
	}
}

func TestCheckBadCardinality(t *testing.T) {
	tests := []struct {
		body string
		exp  []types.Type
	}{
		{
			body: "plus(1, 2)",
			exp:  []types.Type{types.N, types.N},
		},
		{
			body: "plus(1, 2, 3, 4)",
			exp:  []types.Type{types.N, types.N, types.N, types.N},
		},
	}
	for _, test := range tests {
		body := MustParseBody(test.body)
		tc := newTypeChecker()
		env := tc.checkLanguageBuiltins()
		_, err := tc.CheckBody(env, body)
		if len(err) != 1 || err[0].Code != TypeErr {
			t.Fatalf("Expected 1 type error from %v but got: %v", body, err)
		}
		detail, ok := err[0].Details.(*ArgErrDetail)
		if !ok {
			t.Fatalf("Expected argument error details but got: %v", err)
		}
		if len(test.exp) != len(detail.Have) {
			t.Fatalf("Expected arg types %v but got: %v", test.exp, detail.Have)
		}
		for i := range test.exp {
			if types.Compare(test.exp[i], detail.Have[i]) != 0 {
				t.Fatalf("Expected types for %v to be %v but got: %v", body[0], test.exp, detail.Have)
			}
		}
	}
}

func TestCheckMatchErrors(t *testing.T) {
	tests := []struct {
		note  string
		query string
	}{
		{"null", "{ null = true }"},
		{"boolean", "{ true = null }"},
		{"number", "{ 1 = null }"},
		{"string", `{ "hello" = null }`},
		{"array", "{[1,2,3] = null}"},
		{"array-nested", `{[1,2,3] = [1,2,"3"]}`},
		{"array-nested-2", `{[1,2] = [1,2,3]}`},
		{"array-dynamic", `{ [ true | true ] = [x | a = [1, "foo"]; x = a[_]] }`},
		{"object", `{{"a": 1, "b": 2} = null}`},
		{"object-nested", `{ {"a": 1, "b": "2"} = {"a": 1, "b": 2} }`},
		{"object-nested-2", `{ {"a": 1} = {"a": 1, "b": "2"} }`},
		{"object-dynamic", `{ obj2 = obj1 }`},
		{"set", "{{1,2,3} = null}"},
	}

	for _, tc := range tests {
		test.Subtest(t, tc.note, func(t *testing.T) {
			body := MustParseBody(tc.query)
			checker := newTypeChecker()
			_, err := checker.CheckBody(nil, body)
			if len(err) != 1 {
				t.Fatalf("Expected exactly one error from %v, but got:\n%v", body, err)
			}
		})
	}

}

func TestCheckBuiltinErrors(t *testing.T) {

	RegisterBuiltin(&Builtin{
		Name: String("fake_builtin_2"),
		Args: []types.Type{
			types.NewAny(types.NewObject(
				[]*types.Property{
					{"a", types.S},
					{"b", types.S},
				}, nil,
			), types.NewObject(
				[]*types.Property{
					{"b", types.S},
					{"c", types.S},
				}, nil,
			)),
		},
		TargetPos: []int{0},
	})

	tests := []struct {
		note  string
		query string
	}{
		{"trivial", "x = true + 1"},
		{"refs", "x = [null]; y = x[0] + 1"},
		{"array comprehensions", `sum([null | true], x)`},
		{"arrays-any", `sum([1,2,"3",4], x)`},
		{"arrays-bad-input", `contains([1,2,3], "x")`},
		{"objects-any", `fake_builtin_2({"a": a, "c": c})`},
		{"objects-bad-input", `sum({"a": 1, "b": 2}, x)`},
		{"sets-any", `sum({1,2,"3",4}, x)`},
	}

	for _, tc := range tests {
		test.Subtest(t, tc.note, func(t *testing.T) {
			body := MustParseBody(tc.query)
			checker := newTypeChecker()
			_, err := checker.CheckBody(nil, body)
			if len(err) != 1 {
				t.Fatalf("Expected exactly one error from %v but got:\n%v", body, err)
			}
		})
	}
}

func newTestEnv(rs []string) *TypeEnv {
	module := MustParseModule(`
		package test
	`)

	var elems []util.T

	for i := range rs {
		rule := MustParseRule(rs[i])
		rule.Module = module
		elems = append(elems, rule)
		for next := rule.Else; next != nil; next = next.Else {
			next.Module = module
			elems = append(elems, next)
		}
	}

	env, err := newTypeChecker().CheckTypes(nil, elems)
	if len(err) > 0 {
		panic(err)
	}

	return env
}

func TestCheckRefErrUnsupported(t *testing.T) {

	query := `arr = [[1,2],[3,4]]; arr[1][0].deadbeef`

	_, errs := newTypeChecker().CheckBody(nil, MustParseBody(query))
	if len(errs) != 1 {
		t.Fatalf("Expected exactly one error but got: %v", errs)
	}

	details, ok := errs[0].Details.(*RefErrUnsupportedDetail)
	if !ok {
		t.Fatalf("Expected ref err unsupported but got: %v", errs)
	}

	wantRef := MustParseRef(`arr[1][0].deadbeef`)
	wantPos := 2
	wantHave := types.N

	if !wantRef.Equal(details.Ref) ||
		wantPos != details.Pos ||
		types.Compare(wantHave, details.Have) != 0 {
		t.Fatalf("Expected (%v, %v, %v) but got: (%v, %v, %v)", wantRef, wantPos, wantHave, details.Ref, details.Pos, details.Have)
	}

}

func TestCheckRefErrMissing(t *testing.T) {

	query := `arr = [1,2,3]; arr[0].deadbeef = elem; elem[0]`

	_, errs := newTypeChecker().CheckBody(nil, MustParseBody(query))
	if len(errs) != 2 {
		t.Fatalf("Expected exactly two errors but got: %v", errs)
	}

	details, ok := errs[1].Details.(*RefErrMissingDetail)
	if !ok {
		t.Fatalf("Expected ref err missing but got: %v", errs)
	}

	if !details.Ref.Equal(MustParseRef("elem[0]")) {
		t.Fatalf("Expected ref elem[0] but got: %v", errs)
	}
}

func TestCheckRefErrInvalid(t *testing.T) {

	env := newTestEnv([]string{
		`p { true }`,
		`q = {"foo": 1, "bar": 2} { true }`,
	})

	tests := []struct {
		note  string
		query string
		ref   string
		pos   int
		have  types.Type
		want  types.Type
		oneOf []Value
	}{
		{
			note:  "bad non-leaf var",
			query: `x = 1; data.test[x]`,
			ref:   `data.test[x]`,
			pos:   2,
			have:  types.N,
			want:  types.S,
			oneOf: []Value{String("p"), String("q")},
		},
		{
			note:  "bad non-leaf ref",
			query: `arr = [1]; data.test[arr[0]]`,
			ref:   `data.test[arr[0]]`,
			pos:   2,
			have:  types.N,
			want:  types.S,
			oneOf: []Value{String("p"), String("q")},
		},
		{
			note:  "bad leaf ref",
			query: `arr = [1]; data.test.q[arr[0]]`,
			ref:   `data.test.q[arr[0]]`,
			pos:   3,
			have:  types.N,
			want:  types.S,
			oneOf: []Value{String("bar"), String("foo")},
		},
		{
			note:  "bad leaf var",
			query: `x = 1; data.test.q[x]`,
			ref:   `data.test.q[x]`,
			pos:   3,
			have:  types.N,
			want:  types.S,
			oneOf: []Value{String("bar"), String("foo")},
		},
		{
			note:  "bad array index value",
			query: "arr = [[1,2],[3],[4]]; arr[0].dead.beef = x",
			ref:   "arr[0].dead.beef",
			pos:   2,
			want:  types.N,
		},
		{
			note:  "bad set element value",
			query: `s = {{1,2},{3,4}}; x = {1,2}; s[x].deadbeef`,
			ref:   "s[x].deadbeef",
			pos:   2,
			want:  types.N,
		},
		{
			note:  "bad object key value",
			query: `arr = [{"a": 1, "c": 3}, {"b": 2}]; arr[0].b`,
			ref:   "arr[0].b",
			pos:   2,
			want:  types.S,
			oneOf: []Value{String("a"), String("c")},
		},
		{
			note:  "bad non-leaf value",
			query: `data.test[1]`,
			ref:   "data.test[1]",
			pos:   2,
			want:  types.S,
			oneOf: []Value{String("p"), String("q")},
		},
	}

	for _, tc := range tests {
		test.Subtest(t, tc.note, func(t *testing.T) {

			_, errs := newTypeChecker().CheckBody(env, MustParseBody(tc.query))
			if len(errs) != 1 {
				t.Fatalf("Expected exactly one error but got: %v", errs)
			}

			details, ok := errs[0].Details.(*RefErrInvalidDetail)
			if !ok {
				t.Fatalf("Expected ref error invalid but got: %v", errs)
			}

			wantRef := MustParseRef(tc.ref)

			if details.Pos != tc.pos ||
				!details.Ref.Equal(wantRef) ||
				types.Compare(details.Want, tc.want) != 0 ||
				types.Compare(details.Have, tc.have) != 0 ||
				!reflect.DeepEqual(details.OneOf, tc.oneOf) {
				t.Fatalf("Expected (%v, %v, %v, %v, %v) but got: (%v, %v, %v, %v, %v)", wantRef, tc.pos, tc.have, tc.want, tc.oneOf, details.Ref, details.Pos, details.Have, details.Want, details.OneOf)
			}
		})
	}
}

func TestUserFunctionsTypeInference(t *testing.T) {
	functions := []string{
		`foo([a, b]) = y { split(a, b, y) }`,
		`bar(x) = y { count(x, y) }`,
		`baz([x, y]) = z { sprintf("%s%s", [x, y], z) }`,
		`buz({"bar": x, "foo": y}) = {a: b} { upper(y, a); json.unmarshal(x, b) }`,
		`foobar(x) = y { buz({"bar": x, "foo": x}, a); baz([a["{5: true}"], "BUZ"], y) }`,
	}
	body := strings.Join(functions, "\n")
	base := fmt.Sprintf("package base\n%s", body)

	c := NewCompiler()
	if c.Compile(map[string]*Module{"base": MustParseModule(base)}); c.Failed() {
		t.Fatalf("Failed to compile base module: %v", c.Errors)
	}

	tests := []struct {
		body string
		err  bool
	}{
		{
			`fn() = y { base.foo(["hello", 5], y) }`,
			true,
		},
		{
			`fn() = y { base.foo(["hello", "ll"], y) }`,
			false,
		},
		{
			`fn() = y { base.baz(["hello", "ll"], y) }`,
			false,
		},
		{
			`fn() = y { base.baz([5, ["foo", "bar", true]], y) }`,
			false,
		},
		{
			`fn() = y { base.baz(["hello", {"a": "b", "c": 3}], y) }`,
			false,
		},
		{
			`fn() = y { base.foobar("this is not json", y) }`,
			false,
		},
		{
			`fn(x) = y { noexist(x, a); y = a[0] }`,
			true,
		},
	}

	for n, test := range tests {
		t.Run(fmt.Sprintf("Test Case %d", n), func(t *testing.T) {
			mod := MustParseModule(fmt.Sprintf("package test\n%s", test.body))
			c := NewCompiler()
			if c.Compile(map[string]*Module{"base": MustParseModule(base), "mod": mod}); c.Failed() != test.err {
				t.Fatalf("Expected compiler to fail: %t, compiler failed: %t", test.err, c.Failed())
			}
		})
	}
}

func TestCheckErrorDetails(t *testing.T) {

	tests := []struct {
		detail   ErrorDetails
		expected []string
	}{
		{
			detail: &RefErrUnsupportedDetail{
				Ref:  MustParseRef("data.foo[x]"),
				Pos:  1,
				Have: types.N,
			},
			expected: []string{
				"data.foo[x]",
				"^^^^^^^^",
				"have: number",
			},
		},
		{
			detail: &RefErrInvalidDetail{
				Ref:  MustParseRef("data.foo[x]"),
				Pos:  2,
				Have: types.N,
				Want: types.S,
			},
			expected: []string{
				"data.foo[x]",
				"         ^",
				"         have (type): number",
				"         want (type): string",
			},
		},
		{
			detail: &RefErrInvalidDetail{
				Ref:  MustParseRef("data.foo[100]"),
				Pos:  2,
				Want: types.S,
				OneOf: []Value{
					String("a"),
					String("b"),
				},
			},
			expected: []string{
				"data.foo[100]",
				"         ^",
				"         have: 100",
				`         want (one of): ["a" "b"]`,
			},
		},
		{
			detail: &RefErrMissingDetail{
				Ref: MustParseRef("xxx.foo"),
			},
			expected: []string{
				"xxx.foo",
				"^^^",
				"missing type information",
			},
		},
		{
			detail: &ArgErrDetail{
				Have: []types.Type{
					types.N,
					types.S,
				},
				Want: []types.Type{
					types.S,
					types.S,
				},
			},
			expected: []string{
				"have: (number, string)",
				"want: (string, string)",
			},
		},
	}

	for _, tc := range tests {
		if !reflect.DeepEqual(tc.detail.Lines(), tc.expected) {
			t.Errorf("Expected %v for %v but got: %v", tc.expected, tc.detail, tc.detail.Lines())
		}
	}
}
