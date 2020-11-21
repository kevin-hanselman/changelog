package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
)

type Changelog struct {
	Repo    string
	Commits chan DecoratedCommit
}

type DecoratedCommit struct {
	object.Commit
	HashHexDigest string
	Tags          []string
}

const baseTemplate = `# {{ .Repo }}
{{ range .Commits }}
{{ if .Tags }}
## {{ range .Tags }}{{ . }} {{ end }}
{{ .Author.When }}
{{ else }}{{ end }}
#### ` + "`{{ slice .HashHexDigest 0 7 }}`" + ` {{ .Message }}{{ end }}`

var serve string

func init() {
	flag.StringVar(&serve, "serve", "", "serves over HTTP at the given address")
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

func cleanRepoPath(repoPath string) string {
	if strings.HasPrefix(repoPath, "git://") {
		return repoPath
	}
	prefixes := []string{
		"http://",
		"https://",
	}
	for _, prefix := range prefixes {
		repoPath = strings.TrimPrefix(repoPath, prefix)
	}
	return strings.Join([]string{"git://", repoPath}, "")
}

func writeChangelog(repoPath string, tmpl *template.Template, out io.Writer) error {
	cloneOptions := git.CloneOptions{
		URL:          repoPath,
		SingleBranch: true,
		NoCheckout:   true,
	}

	// Try cloning the "master" branch first, and if that fails try the "main"
	// branch.
	repo, err := git.Clone(memory.NewStorage(), nil, &cloneOptions)
	if _, ok := err.(git.NoMatchingRefSpecError); ok {
		cloneOptions.ReferenceName = plumbing.NewBranchReferenceName("main")
		repo, err = git.Clone(memory.NewStorage(), nil, &cloneOptions)
	}
	if err != nil {
		return err
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
		Repo:    strings.TrimPrefix(repoPath, "git://"),
		Commits: make(chan DecoratedCommit, 32),
	}

	goErr := make(chan error)
	go func() {
		defer close(goErr)
		goErr <- tmpl.Execute(out, cl)
	}()

	for {
		commit, err := commitIter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		decoratedCommit := DecoratedCommit{
			Commit:        *commit,
			HashHexDigest: hex.EncodeToString(commit.Hash[:]),
			Tags:          tagsByCommit[commit.Hash],
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

func main() {
	check := func(err error) {
		if err != nil {
			log.Fatal(err)
		}
	}
	flag.Parse()

	tmpl, err := template.New("changelog").Parse(baseTemplate)
	check(err)

	if serve != "" {
		primaryRoute := "/"
		http.HandleFunc(primaryRoute, func(w http.ResponseWriter, req *http.Request) {
			repoPath := cleanRepoPath(strings.TrimPrefix(req.URL.Path, primaryRoute))
			log.Printf("processing %#v\n", repoPath)
			writeChangelog(repoPath, tmpl, w)
		})
		// Ignore requests for a site icon.
		http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, req *http.Request) {})
		log.Printf("listening at %s\n", serve)
		check(http.ListenAndServe(serve, nil))
	} else {
		if len(os.Args) < 2 {
			flag.Usage()
			fmt.Println("No repository path specified")
			os.Exit(1)
		}
		repoPath := os.Args[1]
		_, err := os.Lstat(repoPath)
		if os.IsNotExist(err) {
			repoPath = cleanRepoPath(repoPath)
		}
		check(writeChangelog(repoPath, tmpl, os.Stdout))
	}
}
