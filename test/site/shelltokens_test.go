package site_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// This file holds the shell reading the inventory guide's read-only check stands
// on (issue #716): the tokenizer that splits a shell text into the words a program
// is given and the operators that end them, and the `<<` heredoc parser whose data
// body it skips to a delimiter line.
//
// The reading is deliberately small: it understands the quoting, the line
// continuation, the comment, the two substitutions and the heredoc the guide's
// blocks use. It returns an error rather than tokens for a text it cannot read to
// its end, because a quote or a substitution whose end the check never saw would
// swallow every command after it. The inventory guide's check reads the words and
// the problems this file returns; TestShellTokensSplitEveryShape pins the reading's
// own shapes.

// shellToken is one token of a shell text: a word, or the operator that ends one,
// with the 1-based line of the text the token begins on.
type shellToken struct {
	text     string
	operator bool
	line     int
}

// The shell contexts a word can be inside, besides a quote: a command substitution
// (`$(` or a backtick) inside a double-quoted string, in which the words split as
// they do outside quotes, and the character that closes it returns the scan to the
// quotes.
const (
	substitution         = 's'
	backtickSubstitution = '`'
)

// shellTokens splits a shell text into the words a program is given and the
// operators that end them: a quote keeps its text, newlines and spaces inside it
// included, in one word; a line continuation joins two lines into one; a comment
// runs to the end of its line; white space, a newline, a pipe, a redirection and a
// parenthesis end a word; a backtick is an operator outside quotes and a
// substitution inside them; and a `<<` heredoc's body is no command's text, so it
// is skipped to its delimiter line. newlineEndsCommand says whether an unquoted
// newline ends an invocation, as it does in a command block and does not in a
// comment whose command a program wraps across two lines.
//
// It returns an error, and no tokens, for a text it cannot read to the end: one
// that ends inside a quote or a substitution, and one whose heredoc has no body or
// no delimiter line.
func shellTokens(text string, newlineEndsCommand bool) ([]shellToken, error) {
	var (
		tokens []shellToken
		word   strings.Builder
		// wordLine is the line the word being built began on: a word that spans
		// lines is the one the shell reads in the line its first rune is on.
		wordLine int
		// contexts is the stack of shell contexts the scan is in: a single
		// quote, a double quote, or a command substitution inside a double
		// quote. An empty stack is outside any quote.
		contexts []rune
	)

	runes := []rune(text)
	// lines holds the 1-based line of every rune, and of the text's end, so a
	// token names its line wherever the scan jumps to, as it does over a heredoc
	// body.
	lines := runeLines(runes)

	context := func() rune {
		if len(contexts) == 0 {
			return 0
		}

		return contexts[len(contexts)-1]
	}

	write := func(at int, r rune) {
		if word.Len() == 0 {
			wordLine = lines[at]
		}

		word.WriteRune(r)
	}

	flush := func() {
		if word.Len() == 0 {
			return
		}

		tokens = append(tokens, shellToken{text: word.String(), line: wordLine})
		word.Reset()
	}

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		operator := func(text string) {
			flush()

			tokens = append(tokens, shellToken{text: text, operator: true, line: lines[i]})
		}

		switch context() {
		case '\'':
			if r == '\'' {
				contexts = contexts[:len(contexts)-1]

				continue
			}

			write(i, r)

			continue
		case '"':
			switch {
			case r == '\\' && i+1 < len(runes):
				i++
				write(i, runes[i])
			case r == '"':
				contexts = contexts[:len(contexts)-1]
			case r == '$' && i+1 < len(runes) && runes[i+1] == '(':
				i++
				// A command substitution runs a program of its own, so
				// its words split as they do outside the quotes.
				flush()
				contexts = append(contexts, substitution)
			case r == '`':
				// A backtick substitutes a command too.
				flush()
				contexts = append(contexts, backtickSubstitution)
			default:
				write(i, r)
			}

			continue
		}

		switch {
		case r == '\\' && i+1 < len(runes) && runes[i+1] == '\n':
			i++
		case r == '\\' && i+1 < len(runes):
			i++
			write(i, runes[i])
		case r == '\'' || r == '"':
			contexts = append(contexts, r)
		case r == '`':
			if context() == backtickSubstitution {
				contexts = contexts[:len(contexts)-1]

				continue
			}

			operator("`")
		case r == ')' && context() == substitution:
			contexts = contexts[:len(contexts)-1]

			operator(")")
		case r == '<' && i+1 < len(runes) && runes[i+1] == '<' && (i+2 >= len(runes) || runes[i+2] != '<'):
			operator("<<")

			last, err := heredocEnd(runes, i)
			if err != nil {
				return nil, err
			}

			i = last
		case r == '#' && word.Len() == 0:
			for i+1 < len(runes) && runes[i+1] != '\n' {
				i++
			}
		case r == ' ' || r == '\t' || r == '\r':
			flush()
		case r == '\n':
			if newlineEndsCommand {
				operator("\n")
			} else {
				flush()
			}
		default:
			if r == '|' || r == '&' || r == ';' || r == '(' || r == ')' || r == '<' || r == '>' {
				operator(string(r))

				continue
			}

			write(i, r)
		}
	}

	if len(contexts) > 0 {
		return nil, fmt.Errorf("the text ends inside %s, so the check cannot read the commands after it", contextName(contexts[len(contexts)-1]))
	}

	flush()

	return tokens, nil
}

// runeLines returns the 1-based line number of every rune of a text, and of its
// end.
func runeLines(runes []rune) []int {
	lines := make([]int, len(runes)+1)
	number := 1

	for i, r := range runes {
		lines[i] = number

		if r == '\n' {
			number++
		}
	}

	lines[len(runes)] = number

	return lines
}

// heredocEnd returns the index of the last rune of the line that ends the body of
// the `<<` heredoc that starts at the rune index, and whether the body ends: a
// heredoc's body is data, not commands, so the scan skips it, an apostrophe in it
// included. `<<-` strips the body's leading tabs, as the shell does.
func heredocEnd(runes []rune, at int) (int, error) {
	i := at + 2

	stripTabs := false

	if i < len(runes) && runes[i] == '-' {
		stripTabs = true
		i++
	}

	for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
		i++
	}

	var (
		delimiter strings.Builder
		quote     rune
	)

	for ; i < len(runes); i++ {
		r := runes[i]

		if quote != 0 {
			if r == quote {
				quote = 0

				continue
			}

			delimiter.WriteRune(r)

			continue
		}

		if r == '\'' || r == '"' {
			quote = r

			continue
		}

		if r == ' ' || r == '\t' || r == '\n' {
			break
		}

		delimiter.WriteRune(r)
	}

	want := delimiter.String()
	if want == "" {
		return 0, fmt.Errorf("a `<<` heredoc names no delimiter, so the check cannot see where its body stops")
	}

	line := i
	for line < len(runes) && runes[line] != '\n' {
		line++
	}

	if line >= len(runes) {
		return 0, fmt.Errorf("the heredoc `<<%s` begins on a text with no body, so the check cannot see where it stops", want)
	}

	for line++; line < len(runes); {
		end := line
		for end < len(runes) && runes[end] != '\n' {
			end++
		}

		body := strings.TrimRight(string(runes[line:end]), "\r")
		if stripTabs {
			body = strings.TrimLeft(body, "\t")
		}

		if body == want {
			return end, nil
		}

		line = end + 1
	}

	return 0, fmt.Errorf("the heredoc `<<%s` has no line that ends it, so the check cannot see where its body stops", want)
}

// contextName names the shell context a scan ended in, for a failure message.
func contextName(context rune) string {
	switch context {
	case '\'':
		return "a single quote"
	case '"':
		return "a double quote"
	case substitution:
		return "a command substitution"
	case backtickSubstitution:
		return "a backtick substitution"
	}

	return "a quote"
}

// TestShellTokensSplitEveryShape pins the shell reading itself (issue #716): the
// words a text splits into, the operators between them, the line each token begins
// on, the shapes the scan keeps in one word — a quote, a line continuation, a
// quoted word over two lines — the shapes it skips — a comment, a heredoc body —
// and the texts it refuses to read at all, which the check must fail on rather
// than pass over.
func TestShellTokensSplitEveryShape(t *testing.T) {
	t.Parallel()

	word := func(text string, line int) shellToken {
		return shellToken{text: text, line: line}
	}

	operator := func(text string, line int) shellToken {
		return shellToken{text: text, operator: true, line: line}
	}

	tests := []struct {
		name               string
		text               string
		newlineEndsCommand bool
		want               []shellToken
		wantErr            string
	}{{
		name:               "words split on white space",
		text:               "aws  ec2\tdescribe-instances --output json",
		newlineEndsCommand: true,
		want: []shellToken{
			word("aws", 1), word("ec2", 1), word("describe-instances", 1), word("--output", 1), word("json", 1),
		},
	}, {
		name:               "operators split the words around them",
		text:               "jq . f.json | xargs -r aws ec2 x > \"$out/y.json\"",
		newlineEndsCommand: true,
		want: []shellToken{
			word("jq", 1), word(".", 1), word("f.json", 1), operator("|", 1), word("xargs", 1), word("-r", 1),
			word("aws", 1), word("ec2", 1), word("x", 1), operator(">", 1), word("$out/y.json", 1),
		},
	}, {
		name:               "a quoted word keeps its spaces and its inner quotes",
		text:               `az graph query --graph-query "Resources | where type =~ 'microsoft.compute/disks'"`,
		newlineEndsCommand: true,
		want: []shellToken{
			word("az", 1), word("graph", 1), word("query", 1), word("--graph-query", 1),
			word("Resources | where type =~ 'microsoft.compute/disks'", 1),
		},
	}, {
		name:               "a line continuation joins two lines",
		text:               "aws ec2 \\\n  describe-volumes",
		newlineEndsCommand: true,
		want:               []shellToken{word("aws", 1), word("ec2", 1), word("describe-volumes", 2)},
	}, {
		name:               "a comment runs to the end of its line",
		text:               "aws ec2 describe-volumes # then aws ec2 create-tags\naz account show",
		newlineEndsCommand: true,
		want: []shellToken{
			word("aws", 1), word("ec2", 1), word("describe-volumes", 1), operator("\n", 1),
			word("az", 2), word("account", 2), word("show", 2),
		},
	}, {
		name:               "a newline that ends no command only splits the words",
		text:               "aws ec2 describe-volumes # then aws ec2 create-tags\naz account show",
		newlineEndsCommand: false,
		want: []shellToken{
			word("aws", 1), word("ec2", 1), word("describe-volumes", 1),
			word("az", 2), word("account", 2), word("show", 2),
		},
	}, {
		name:               "a heredoc body is data",
		text:               "cat <<'EOF'\naws ec2 create-tags\nEOF\naws ec2 describe-volumes\n",
		newlineEndsCommand: true,
		want: []shellToken{
			word("cat", 1), operator("<<", 1),
			word("aws", 4), word("ec2", 4), word("describe-volumes", 4), operator("\n", 4),
		},
	}, {
		name:               "a heredoc that strips tabs ends on its tabbed delimiter",
		text:               "cat <<-EOF\n\taws ec2 create-tags\n\tEOF\naws ec2 describe-volumes\n",
		newlineEndsCommand: true,
		want: []shellToken{
			word("cat", 1), operator("<<", 1),
			word("aws", 4), word("ec2", 4), word("describe-volumes", 4), operator("\n", 4),
		},
	}, {
		name:               "a heredoc the text never ends",
		text:               "cat <<EOF\nthe body\n",
		newlineEndsCommand: true,
		wantErr:            "has no line that ends it",
	}, {
		name:               "a command substitution in quotes splits into words",
		text:               `s="$(az account show --query id)"`,
		newlineEndsCommand: true,
		want: []shellToken{
			word("s=", 1), word("az", 1), word("account", 1), word("show", 1), word("--query", 1), word("id", 1),
			operator(")", 1),
		},
	}, {
		name:               "a backtick outside quotes is an operator",
		text:               "out=`aws ec2 create-tags`",
		newlineEndsCommand: true,
		want: []shellToken{
			word("out=", 1), operator("`", 1), word("aws", 1), word("ec2", 1), word("create-tags", 1), operator("`", 1),
		},
	}, {
		name:               "a backtick inside quotes substitutes a command",
		text:               "v=\"`aws ec2 create-tags`\"",
		newlineEndsCommand: true,
		want:               []shellToken{word("v=", 1), word("aws", 1), word("ec2", 1), word("create-tags", 1)},
	}, {
		name:               "a quoted word that spans lines is one word",
		text:               "azure='{\n  id: id}'",
		newlineEndsCommand: true,
		want:               []shellToken{word("azure={\n  id: id}", 1)},
	}, {
		name:               "a backslash escapes a space into the word",
		text:               `key=a\ b`,
		newlineEndsCommand: true,
		want:               []shellToken{word("key=a b", 1)},
	}, {
		name:               "a single quote the text never closes",
		text:               "aws ec2 describe-volumes --query 'Volumes\n",
		newlineEndsCommand: true,
		wantErr:            "ends inside a single quote",
	}, {
		name:               "a command substitution the text never closes",
		text:               `out="$(aws ec2 describe-volumes`,
		newlineEndsCommand: true,
		wantErr:            "ends inside a command substitution",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := shellTokens(tc.text, tc.newlineEndsCommand)

			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("the reading of %q reports %v, want no problem: it reads the shape", tc.text, err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("the reading of %q reports %v, want a problem that holds %q: a text the check cannot read to its end must not pass",
					tc.text, err, tc.wantErr)
			}

			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(shellToken{})); diff != "" {
				t.Errorf("the tokens of %q differ from the ones it holds (-want +got):\n%s", tc.text, diff)
			}
		})
	}
}
