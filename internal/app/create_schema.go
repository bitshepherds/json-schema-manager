package app

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

// familyFileHelp documents family.yml, for the command reference.
const familyFileHelp = `
Every family declares how it is used in family.yml, in the family's directory beside its version
directories. It names a scenario, and may override the facts the scenario implies:

  scenario: retained-stream
  # Optional overrides of what the scenario implies:
  ahead: either             # writer | reader | either | neither
  documentsLive: retention  # moment | retention | indefinitely
  readerPicksSchema: false
  schema: prescribes        # prescribes | describes
  # Required when documentsLive is retention, and refused otherwise:
  oldestLive: 1.2.0
  # For each major past the first. Required when documents live indefinitely and the reader doesn't
  # pick its schema, and allowed otherwise:
  migrations:
    2: Readers replaying the log handle both majors until the old events have gone.

The scenarios, and what to choose, are in user-docs/scenarios.md. They are provider-first,
consumer-first, config-file, stored-one-schema, stored-self-naming, retained-stream, uncoordinated,
lock-step and third-party-format.

How long documents live varies in the scenarios uncoordinated and third-party-format, so a family in
either must state documentsLive. If you don't know how long they live, choose indefinitely.

Every command which reads a family fails if it has no declaration, or an invalid one, except
render-schema and tag-deployment. Once any version of a family is deployed, where an environment doesn't
allow schema mutation, check-changes refuses a change to its declaration, other than moving oldestLive
forward, adding or changing a migration statement, or adding a declaration to a family with none: a
changed scenario means a new family.
`

// NewCreateSchemaCmd returns a new cobra command for creating a schema.
func NewCreateSchemaCmd(mgr Manager) *cobra.Command {
	var scenario, documentsLive string

	cmd := &cobra.Command{
		Use:   "create-schema [domain/family]",
		Short: "Create a new JSON schema",
		Long: `Create a completely new JSON schema family in the registry. The 1.0.0 version of the schema will be created.

The command asks which scenario the family serves, unless --scenario says, and writes its family.yml.
` + familyFileHelp,
		Args: cobra.ExactArgs(1),
		Example: `
jsm create-schema "domain-a/family-a"
jsm create-schema "domain-a/family-a" --scenario provider-first
jsm create-schema "domain-a/subdomain-b/family-c" --scenario uncoordinated --documents-live indefinitely
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			domainAndFamily := args[0]
			spec, err := familySpec(cmd, scenario, documentsLive)
			if err != nil {
				return err
			}

			key, err := mgr.CreateSchema(domainAndFamily, spec)
			if err != nil {
				return err
			}

			cmd.Printf("Successfully created new schema with key: %s\n\n", key)
			s, err := mgr.Registry().GetSchemaByKey(key)
			if err == nil {
				cmd.Println("The schema and its test documents can be found here:")
				cmd.Printf("  %s\n\n", s.Path(schema.HomeDir))
				cmd.Printf("How the family is used is declared in %s.\n",
					mgr.Registry().FamilyDeclarationPath(key))
				cmd.Println("Add JSON documents to the `pass` directory that you expect to PASS validation.")
				cmd.Println("Add JSON documents to the `fail` directory that you expect to FAIL validation.")
				cmd.Printf("Then run `jsm validate %s` to test the schema with these documents.\n", key)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&scenario, "scenario", "",
		"The scenario the family serves: "+schema.ScenarioNames())
	cmd.Flags().StringVar(&documentsLive, "documents-live", "",
		"How long documents outlive their writer: moment, retention or indefinitely. Required for the scenarios "+
			"uncoordinated and third-party-format. If you don't know, choose indefinitely")

	return cmd
}

// familySpec returns what the flags say about a new family, asking for anything they leave out.
func familySpec(cmd *cobra.Command, scenario, documentsLive string) (schema.FamilySpec, error) {
	spec := schema.FamilySpec{Scenario: schema.Scenario(scenario), DocumentsLive: schema.DocumentsLive(documentsLive)}
	in := bufio.NewReader(cmd.InOrStdin())

	if spec.Scenario == "" {
		cmd.Println("Which scenario does this family serve? See user-docs/scenarios.md to choose one.")
		for i, info := range schema.Scenarios() {
			cmd.Printf("  %d. %s: %s\n", i+1, info.Name, info.Title)
		}
		answer, err := ask(cmd, in, "Scenario (number or name): ",
			"no scenario given: use --scenario, with one of: "+schema.ScenarioNames())
		if err != nil {
			return spec, err
		}
		spec.Scenario = scenarioFromAnswer(answer)
	}

	if spec.DocumentsLive == "" && spec.Scenario.DocumentsLiveVaries() {
		cmd.Printf("How long documents live varies in scenario %s. If you don't know, choose indefinitely.\n",
			spec.Scenario)
		answer, err := ask(cmd, in, "How long do documents outlive their writer (moment, retention, indefinitely)? ",
			"no answer given: use --documents-live, with moment, retention or indefinitely. "+
				"If you don't know how long documents live, choose indefinitely")
		if err != nil {
			return spec, err
		}
		spec.DocumentsLive = schema.DocumentsLive(answer)
	}
	return spec, nil
}

// ask prompts for one line, and returns an error with the given message if there's no answer to read.
func ask(cmd *cobra.Command, in io.Reader, prompt, noAnswer string) (string, error) {
	cmd.Print(prompt)
	var answer string
	if _, err := fmt.Fscanln(in, &answer); err != nil {
		cmd.Println()
		return "", errors.New(noAnswer)
	}
	return strings.TrimSpace(answer), nil
}

// scenarioFromAnswer turns a menu number into its scenario. Any other answer is taken as a name, for
// the declaration's own check to refuse if it isn't one.
func scenarioFromAnswer(answer string) schema.Scenario {
	all := schema.Scenarios()
	if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(all) {
		return all[n-1].Name
	}
	return schema.Scenario(answer)
}
