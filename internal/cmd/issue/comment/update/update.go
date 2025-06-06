package update

import (
	"fmt"

	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/ankitpokhrel/jira-cli/api"
	"github.com/ankitpokhrel/jira-cli/internal/cmdcommon"
	"github.com/ankitpokhrel/jira-cli/internal/cmdutil"
	"github.com/ankitpokhrel/jira-cli/internal/query"
	"github.com/ankitpokhrel/jira-cli/pkg/adf"
	"github.com/ankitpokhrel/jira-cli/pkg/jira"
	"github.com/ankitpokhrel/jira-cli/pkg/md"
	"github.com/ankitpokhrel/jira-cli/pkg/surveyext"
)

const (
	helpText = `Add updates a comment to an issue.`
	examples = `$ jira issue comment update

# Pass required parameters to skip prompt 
$ jira issue comment update ISSUE-1 986745 "My comment"

# Multi-line comment
$ jira issue comment update ISSUE-1 986745 $'Supports\n\nNew line'

# Load comment body from a template file
$ jira issue comment update ISSUE-1 986745 --template /path/to/template.tmpl

# Get comment body from standard input
$ jira issue comment update ISSUE-1 986745 --template -

# Or, use pipe to read input directly from standard input
$ echo "Comment from stdin" | jira issue comment update ISSUE-1 986745

# Positional argument takes precedence over the template flag
# The example below will add "comment from arg" as a comment
$ jira issue comment update ISSUE-1 986745 "comment from arg" --template /path/to/template.tmpl`
)

// NewCmdCommentAdd is a comment update command.
func NewCmdCommentUpdate() *cobra.Command {
	cmd := cobra.Command{
		Use:     "update ISSUE-KEY COMMENT-ID [COMMENT_BODY]",
		Short:   "Update a comment to an issue",
		Long:    helpText,
		Example: examples,
		Annotations: map[string]string{
			"help:args": "ISSUE-KEY\tIssue key of the source issue, eg: ISSUE-1\n" +
				"COMMENT-ID\tComment id of the source comment, eg:986745\n" +
				"COMMENT_BODY\tBody of the comment you want to add",
		},
		Run: update,
	}

	cmd.Flags().Bool("web", false, "Open issue in web browser after adding comment")
	cmd.Flags().StringP("template", "T", "", "Path to a file to read comment body from")
	cmd.Flags().Bool("no-input", false, "Disable prompt for non-required fields")
	cmd.Flags().Bool("internal", false, "Make comment internal")

	return &cmd
}

func update(cmd *cobra.Command, args []string) {
	params := parseArgsAndFlags(args, cmd.Flags())
	client := api.DefaultClient(params.debug)
	uc := updateCmd{
		client:    client,
		linkTypes: nil,
		params:    params,
	}

	if uc.isNonInteractive() {
		uc.params.noInput = true

		if uc.isMandatoryParamsMissing() {
			cmdutil.Failed("`ISSUE-KEY` & `COMMENT-ID` are mandatory when using a non-interactive mode")
		}
	}

	cmdutil.ExitIfError(uc.setIssueKey())

	cmdutil.ExitIfError(uc.setCommentId())

	cmdutil.ExitIfError(uc.setBody())

	if !params.noInput {
		answer := struct{ Action string }{}
		err := survey.Ask([]*survey.Question{getNextAction()}, &answer)
		cmdutil.ExitIfError(err)

		if answer.Action == cmdcommon.ActionCancel {
			cmdutil.Failed("Action aborted")
		}
	}

	err := func() error {
		s := cmdutil.Info("Adding comment")
		defer s.Stop()

		return client.UpdateIssueComment(uc.params.issueKey, uc.params.commentId, uc.params.body, uc.params.internal)
	}()
	cmdutil.ExitIfError(err)

	server := viper.GetString("server")

	cmdutil.Success("Comment %q of issue %q updated", uc.params.commentId, uc.params.issueKey)
	fmt.Printf("%s?focusedCommentId=%s\n", cmdutil.GenerateServerBrowseURL(server, uc.params.issueKey), uc.params.commentId)

	if web, _ := cmd.Flags().GetBool("web"); web {
		err := cmdutil.Navigate(server, uc.params.issueKey)
		cmdutil.ExitIfError(err)
	}
}

type addParams struct {
	issueKey  string
	commentId string
	body      string
	template  string
	noInput   bool
	internal  bool
	debug     bool
}

func parseArgsAndFlags(args []string, flags query.FlagParser) *addParams {
	var issueKey, commentId, body string

	nargs := len(args)
	if nargs >= 1 {
		issueKey = cmdutil.GetJiraIssueKey(viper.GetString("project.key"), args[0])
	}
	if nargs >= 2 {
		commentId = args[1]
	}
	if nargs >= 3 {
		body = args[2]
	}

	debug, err := flags.GetBool("debug")
	cmdutil.ExitIfError(err)

	template, err := flags.GetString("template")
	cmdutil.ExitIfError(err)

	noInput, err := flags.GetBool("no-input")
	cmdutil.ExitIfError(err)

	internal, err := flags.GetBool("internal")
	cmdutil.ExitIfError(err)

	return &addParams{
		issueKey:  issueKey,
		commentId: commentId,
		body:      body,
		template:  template,
		noInput:   noInput,
		internal:  internal,
		debug:     debug,
	}
}

type updateCmd struct {
	client    *jira.Client
	linkTypes []*jira.IssueLinkType
	params    *addParams
}

func (uc *updateCmd) setIssueKey() error {
	if uc.params.issueKey != "" {
		return nil
	}

	var ans string

	qs := &survey.Question{
		Name:     "issueKey",
		Prompt:   &survey.Input{Message: "Issue key"},
		Validate: survey.Required,
	}
	if err := survey.Ask([]*survey.Question{qs}, &ans); err != nil {
		return err
	}
	uc.params.issueKey = cmdutil.GetJiraIssueKey(viper.GetString("project.key"), ans)

	return nil
}

func (uc *updateCmd) setCommentId() error {
	if uc.params.commentId != "" {
		return nil
	}

	var ans string

	qs := &survey.Question{
		Name:     "commendId",
		Prompt:   &survey.Input{Message: "Comment Id"},
		Validate: survey.Required,
	}
	if err := survey.Ask([]*survey.Question{qs}, &ans); err != nil {
		return err
	}
	uc.params.commentId = ans

	return nil
}

func (uc *updateCmd) setBody() error {
	if uc.params.body != "" {
		return nil
	}

	var (
		qs          []*survey.Question
		defaultBody string
	)

	if uc.params.template != "" || cmdutil.StdinHasData() {
		b, err := cmdutil.ReadFile(uc.params.template)
		if err != nil {
			cmdutil.Failed("Error: %s", err)
		}
		defaultBody = string(b)
	}

	if uc.params.noInput {
		uc.params.body = defaultBody
		return nil
	}

	if uc.params.template == "" {
		originalComment, error := uc.client.GetIssueComment(uc.params.issueKey, uc.params.commentId)
		cmdutil.ExitIfError(error)

		// from internal/view/issue.go:381 how to DRY?
		var body string
		if adfNode, ok := originalComment.Body.(*adf.ADF); ok {
			body = adf.NewTranslator(adfNode, adf.NewMarkdownTranslator()).Translate()
		} else {
			body = originalComment.Body.(string)
			body = md.FromJiraMD(body)
		}
		defaultBody = body
	}

	if uc.params.body == "" {
		qs = append(qs, &survey.Question{
			Name: "body",
			Prompt: &surveyext.JiraEditor{
				Editor: &survey.Editor{
					Message:       "Comment body",
					Default:       defaultBody,
					HideDefault:   true,
					AppendDefault: true,
				},
				BlankAllowed: false,
			},
		})
	}

	ans := struct{ Body string }{}
	err := survey.Ask(qs, &ans)
	cmdutil.ExitIfError(err)

	uc.params.body = ans.Body

	return nil
}

func getNextAction() *survey.Question {
	return &survey.Question{
		Name: "action",
		Prompt: &survey.Select{
			Message: "What's next?",
			Options: []string{
				cmdcommon.ActionSubmit,
				cmdcommon.ActionCancel,
			},
		},
		Validate: survey.Required,
	}
}

func (uc *updateCmd) isNonInteractive() bool {
	return cmdutil.StdinHasData() || uc.params.template == "-"
}

func (uc *updateCmd) isMandatoryParamsMissing() bool {
	return uc.params.issueKey == "" || uc.params.commentId == ""
}
