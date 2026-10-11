// noise-tasks-eval is the offline developer harness for frozen PRD-generation
// corpora and run bundles. It is intentionally not installed as a noise
// plugin and contains no provider or network code.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	taskeval "github.com/noisefloor-oss/tasks/internal/eval"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "compile-corpus":
		return compileCorpusCommand(args[1:], stdout, stderr)
	case "validate-corpus":
		return validateCorpusCommand(args[1:], stdout, stderr)
	case "validate-runs":
		return validateRunsCommand(args[1:], stdout, stderr, false)
	case "report":
		return validateRunsCommand(args[1:], stdout, stderr, true)
	case "help", "--help", "-h":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "noise-tasks-eval: unknown command %q\n", args[0])
		usage(stderr)
		return 2
	}
}

func compileCorpusCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("compile-corpus", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: noise-tasks-eval compile-corpus <source.json>")
		return 2
	}
	file, err := os.Open(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "noise-tasks-eval: open corpus source: %v\n", err)
		return 1
	}
	defer file.Close()
	corpus, err := taskeval.CompileCorpus(file)
	if err != nil {
		fmt.Fprintf(stderr, "noise-tasks-eval: compile corpus: %v\n", err)
		return 1
	}
	raw, err := taskeval.MarshalCorpus(corpus)
	if err != nil {
		fmt.Fprintf(stderr, "noise-tasks-eval: marshal corpus: %v\n", err)
		return 1
	}
	if _, err := stdout.Write(raw); err != nil {
		fmt.Fprintf(stderr, "noise-tasks-eval: write corpus: %v\n", err)
		return 1
	}
	return 0
}

func validateCorpusCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate-corpus", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: noise-tasks-eval validate-corpus <corpus.json>")
		return 2
	}
	corpus, err := loadCorpus(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "noise-tasks-eval: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "ok: corpus %s contains %d validated cases\n", corpus.Version, len(corpus.Cases))
	return 0
}

func validateRunsCommand(args []string, stdout, stderr io.Writer, emitReport bool) int {
	name := "validate-runs"
	if emitReport {
		name = "report"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	corpusPath := fs.String("corpus", "", "path to the bound corpus JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *corpusPath == "" || fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: noise-tasks-eval %s --corpus <corpus.json> <runs.json>\n", name)
		return 2
	}
	corpus, err := loadCorpus(*corpusPath)
	if err != nil {
		fmt.Fprintf(stderr, "noise-tasks-eval: %v\n", err)
		return 1
	}
	bundle, err := loadRuns(fs.Arg(0), corpus)
	if err != nil {
		fmt.Fprintf(stderr, "noise-tasks-eval: %v\n", err)
		return 1
	}
	if !emitReport {
		fmt.Fprintf(stdout, "ok: evaluation %s contains %d validated runs\n", bundle.EvaluationID, len(bundle.Runs))
		return 0
	}
	report, err := taskeval.BuildReport(corpus, bundle)
	if err != nil {
		fmt.Fprintf(stderr, "noise-tasks-eval: %v\n", err)
		return 1
	}
	raw, err := taskeval.MarshalReport(report)
	if err != nil {
		fmt.Fprintf(stderr, "noise-tasks-eval: %v\n", err)
		return 1
	}
	if _, err := stdout.Write(raw); err != nil {
		fmt.Fprintf(stderr, "noise-tasks-eval: write report: %v\n", err)
		return 1
	}
	return 0
}

func loadCorpus(path string) (*taskeval.Corpus, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open corpus: %w", err)
	}
	defer file.Close()
	corpus, err := taskeval.LoadCorpus(file)
	if err != nil {
		return nil, fmt.Errorf("validate corpus: %w", err)
	}
	return corpus, nil
}

func loadRuns(path string, corpus *taskeval.Corpus) (*taskeval.RunBundle, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open runs: %w", err)
	}
	defer file.Close()
	bundle, err := taskeval.LoadRunBundle(file, corpus)
	if err != nil {
		return nil, fmt.Errorf("validate runs: %w", err)
	}
	return bundle, nil
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `usage: noise-tasks-eval <command> [options]

commands:
	compile-corpus  <source.json>
	validate-corpus <corpus.json>
	validate-runs   --corpus <corpus.json> <runs.json>
	report          --corpus <corpus.json> <runs.json>`)
}
