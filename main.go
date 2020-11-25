package main

import (
	"bytes"
	"encoding/hex"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Changelog is the root data structure available to the output template.
type Changelog struct {
	Repo    string
	Commits chan DecoratedCommit
}

// DecoratedCommit is a go-git Commit struct with additional metadata.
type DecoratedCommit struct {
	object.Commit
	HashHexDigest string
	Tags          []string
}

const defaultTemplate = `# {{ .Repo }}
{{ range .Commits }}
{{ if .Tags }}
## {{ range .Tags }}{{ . }} {{ end }}
{{ .Author.When }}
{{ else }}{{ end }}
#### ` + "`{{ slice .HashHexDigest 0 7 }}`" + ` {{ .Message }}{{ end }}`

var (
	onlyTag, serve, templatePath string
	maxRevs                      int
)

func init() {
	flag.StringVar(&serve, "http", "", "serves over HTTP at the given address")
	flag.StringVar(&onlyTag, "tag", "", "show the changelog for only the given tag")
	flag.StringVar(&templatePath, "template", "", "load the output template from the given file")
	flag.IntVar(&maxRevs, "max-revs", 0, "max versions to show before exiting")
}

func collectTags(repo *git.Repository) (map[plumbing.Hash][]string, error) {
	tagsByCommit := make(map[plumbing.Hash][]string)
	refIter, err := repo.Tags()
	if err != nil {
		return nil, err
	}
	var (
		ref        *plumbing.Reference
		commit     *object.Commit
		commitHash plumbing.Hash
	)
	for {
		ref, err = refIter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		// Check if the tag is annotated or lightweight.
		tag, err := repo.TagObject(ref.Hash())
		switch err {
		case nil:
			// This is an annotated tag.
			commit, err = tag.Commit()
			if err == object.ErrUnsupportedObject {
				// This tag doesn't point to a commit.
				continue
			}
			commitHash = commit.Hash
		case plumbing.ErrObjectNotFound:
			// This is a lightweight tag and directly points to a commit.
			commitHash = ref.Hash()
		default:
			return nil, err
		}
		tags := tagsByCommit[commitHash]
		cleanTagName := strings.TrimPrefix(ref.Name().String(), "refs/tags/")
		tagsByCommit[commitHash] = append(tags, cleanTagName)
	}
	return tagsByCommit, nil
}

func clone(repoURL, tag string) (repo *git.Repository, destDir string, err error) {
	destDir, err = ioutil.TempDir("", "changelog_")
	if err != nil {
		return
	}

	args := []string{
		"clone",
		"--quiet",
		"--bare",
		"--single-branch",
	}
	if tag != "" {
		args = append(args, "--branch", tag)
	}
	args = append(args, repoURL, destDir)
	cmd := exec.Command("git", args...)

	buf := &bytes.Buffer{}
	cmd.Stderr = buf

	if err = cmd.Run(); err != nil {
		err = fmt.Errorf("%s: %s", err, buf)
		return
	}
	repo, err = git.PlainOpen(destDir)
	return
}

func writeChangelog(
	repoPath,
	tag string,
	maxRevs int,
	tmpl *template.Template,
	out io.Writer,
) (err error) {
	repo, repoDir, err := clone(repoPath, tag)
	defer os.RemoveAll(repoDir)

	if err != nil {
		return err
	}

	if tag != "" && maxRevs == 0 {
		maxRevs = 1
	}

	tagsByCommit, err := collectTags(repo)
	if err != nil {
		return err
	}

	commitIter, err := repo.Log(&git.LogOptions{})
	defer commitIter.Close()
	if err != nil {
		return err
	}

	cl := Changelog{
		Repo:    repoPath,
		Commits: make(chan DecoratedCommit, 32),
	}

	goErr := make(chan error)
	go func() {
		goErr <- tmpl.Execute(out, cl)
		close(goErr)
	}()

	numTaggedCommits := 0
	for {
		commit, err := commitIter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		commitTags, hasTags := tagsByCommit[commit.Hash]
		decoratedCommit := DecoratedCommit{
			Commit:        *commit,
			HashHexDigest: hex.EncodeToString(commit.Hash[:]),
			Tags:          commitTags,
		}
		if hasTags {
			numTaggedCommits++
		}
		if maxRevs > 0 && numTaggedCommits > maxRevs {
			break
		}
		select {
		case cl.Commits <- decoratedCommit:
		case err := <-goErr:
			// Always fail here, because if the templating returned before the
			// commit iterator, we have a problem.
			return err
		}
	}
	close(cl.Commits)
	return <-goErr
}

func parseRequest(req *http.Request, route string) (repoURL, tag string, maxRevs int, err error) {
	repoPath := strings.TrimPrefix(req.URL.Path, route)

	var cloneScheme string
	parts := strings.SplitN(repoPath, "/", 2)
	if len(parts) == 2 {
		cloneScheme, repoPath = parts[0], parts[1]
	} else {
		err = fmt.Errorf("invalid request: %v", req)
		return
	}

	parts = strings.Split(repoPath, "@")
	if len(parts) == 2 {
		repoPath, tag = parts[0], parts[1]
	} else if len(parts) > 2 {
		err = fmt.Errorf("invalid request: %v", req)
		return
	}
	maxRevsStr := req.URL.Query().Get("maxRevs")
	if maxRevsStr != "" {
		maxRevs, err = strconv.Atoi(maxRevsStr)
	}

	if cloneScheme == "ssh" {
		repoPath = "git@" + repoPath
	}

	repoURL = fmt.Sprintf("%s://%s", cloneScheme, repoPath)
	return
}

// SplitLines splits the input on newline characters.
func SplitLines(s string) []string {
	return strings.Split(s, "\n")
}

func main() {
	check := func(err error) {
		if err != nil {
			log.Fatal(err)
		}
	}
	flag.Parse()

	var (
		err error
	)
	tmpl := template.New("changelog").Funcs(template.FuncMap{"SplitLines": SplitLines})
	if templatePath == "" {
		tmpl, err = template.New("changelog").Parse(defaultTemplate)
	} else {
		templateContents, err := ioutil.ReadFile(templatePath)
		check(err)
		tmpl, err = tmpl.Parse(string(templateContents))
	}
	check(err)

	if serve == "" {
		repoPath := flag.Arg(0)
		if repoPath == "" {
			flag.Usage()
			fmt.Println("No repository path specified")
			os.Exit(1)
		}
		check(writeChangelog(repoPath, onlyTag, maxRevs, tmpl, os.Stdout))
	} else {
		primaryRoute := "/"
		http.HandleFunc(primaryRoute, func(w http.ResponseWriter, req *http.Request) {
			repoURL, tag, maxRevs, err := parseRequest(req, primaryRoute)
			if err != nil {
				fmt.Fprintln(w, err)
				log.Println(err)
				return
			}
			log.Printf(
				"%#v -> repo: %#v tag: %#v maxRevs: %d\n",
				req.URL.String(),
				repoURL,
				tag,
				maxRevs,
			)
			err = writeChangelog(repoURL, tag, maxRevs, tmpl, w)
			if err != nil {
				fmt.Fprintln(w, err)
				log.Println(err)
			}
		})
		// Ignore requests for a site icon.
		http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, req *http.Request) {})
		log.Printf("listening at %s\n", serve)
		check(http.ListenAndServe(serve, nil))
	}
}
