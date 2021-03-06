// Copyright 2019 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package main

import (
	"io"
	"io/ioutil"
	"strings"
	"text/template"

	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree"
)

// mjOverload contains the overloads for both equality and less than comparisons.
type mjOverload struct {
	// The embedded overload has the shared type information for both of the
	// overloads, so that you can reference that information inside of . without
	// needing to pick Eq or Lt.
	overload
	Eq *overload
	Lt *overload
}

// selPermutation contains information about which permutation of selection vector
// state the template is materializing.
type selPermutation struct {
	IsLSel bool
	IsRSel bool

	LSelString string
	RSelString string
}

func genMergeJoinOps(wr io.Writer) error {
	d, err := ioutil.ReadFile("pkg/sql/exec/mergejoiner_tmpl.go")
	if err != nil {
		return err
	}

	s := string(d)

	// Replace the template variables.
	s = strings.Replace(s, "_GOTYPE", "{{.LTyp.GoTypeName}}", -1)
	s = strings.Replace(s, "_TYPES_T", "types.{{.LTyp}}", -1)
	s = strings.Replace(s, "_TemplateType", "{{.LTyp}}", -1)
	s = strings.Replace(s, "_L_SEL_IND", "{{$sel.LSelString}}", -1)
	s = strings.Replace(s, "_R_SEL_IND", "{{$sel.RSelString}}", -1)
	s = strings.Replace(s, "_IS_L_SEL", "{{$sel.IsLSel}}", -1)
	s = strings.Replace(s, "_IS_R_SEL", "{{$sel.IsRSel}}", -1)

	copyWithSel := makeFunctionRegex("_COPY_WITH_SEL", 5)
	s = copyWithSel.ReplaceAllString(s, `{{template "copyWithSel" . }}`)

	probeBody := makeFunctionRegex("_PROBE_BODY", 2)
	s = probeBody.ReplaceAllString(s, `{{template "probeBody" buildDict "Global" . "LSelInd" $1 "RSelInd" $2}}`)

	assignEqRe := makeFunctionRegex("_ASSIGN_EQ", 3)
	s = assignEqRe.ReplaceAllString(s, `{{.Eq.Assign $1 $2 $3}}`)

	assignLtRe := makeFunctionRegex("_ASSIGN_LT", 3)
	s = assignLtRe.ReplaceAllString(s, `{{.Lt.Assign $1 $2 $3}}`)

	// Now, generate the op, from the template.
	tmpl, err := template.New("mergejoin_op").Funcs(template.FuncMap{"buildDict": buildDict}).Parse(s)
	if err != nil {
		return err
	}

	allOverloads := intersectOverloads(comparisonOpToOverloads[tree.EQ], comparisonOpToOverloads[tree.LT])

	// Create an mjOverload for each overload, combining the two overloads so that
	// the template code can access both the LT method and the EQ method in the
	// same range loop.
	mjOverloads := make([]mjOverload, len(allOverloads[0]))
	for i := range allOverloads[0] {
		mjOverloads[i] = mjOverload{
			overload: *allOverloads[0][i],
			Eq:       allOverloads[0][i],
			Lt:       allOverloads[1][i],
		}
	}

	// Create each permutation of selection vector state.
	selPermutations := []selPermutation{
		{
			IsLSel:     true,
			IsRSel:     true,
			LSelString: "lSel[curLIdx]",
			RSelString: "rSel[curRIdx]",
		},
		{
			IsLSel:     true,
			IsRSel:     false,
			LSelString: "lSel[curLIdx]",
			RSelString: "curRIdx",
		},
		{
			IsLSel:     false,
			IsRSel:     true,
			LSelString: "curLIdx",
			RSelString: "rSel[curRIdx]",
		},
		{
			IsLSel:     false,
			IsRSel:     false,
			LSelString: "curLIdx",
			RSelString: "curRIdx",
		},
	}

	return tmpl.Execute(wr, struct {
		MJOverloads     interface{}
		SelPermutations interface{}
	}{
		MJOverloads:     mjOverloads,
		SelPermutations: selPermutations,
	})
}

func init() {
	registerGenerator(genMergeJoinOps, "mergejoiner.eg.go")
}
