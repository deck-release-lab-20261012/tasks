package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	taskeval "github.com/noisefloor-oss/tasks/internal/eval"
	"github.com/noisefloor-oss/tasks/internal/genai"
)

func TestRunUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help exit = %d", code)
	}
	if !strings.Contains(stdout.String(), "validate-corpus") || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCommittedCorpusMatchesAuthoringSource(t *testing.T) {
	source, err := os.Open("../../evals/corpus/v1/source.json")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	corpus, err := taskeval.CompileCorpus(source)
	if err != nil {
		t.Fatal(err)
	}
	got, err := taskeval.MarshalCorpus(corpus)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../../evals/corpus/v1/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("committed corpus.json is stale; run noise-tasks-eval compile-corpus")
	}
}

func TestRunRefusesMissingFiles(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"validate-corpus", "/definitely/missing/corpus.json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "open corpus") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunValidatesAndReportsCompleteToyBundle(t *testing.T) {
	const corpusPath = "../../evals/corpus/v1/corpus.json"
	corpusFile, err := os.Open(corpusPath)
	if err != nil {
		t.Fatal(err)
	}
	corpus, err := taskeval.LoadCorpus(corpusFile)
	corpusFile.Close()
	if err != nil {
		t.Fatal(err)
	}
	bundle := toyRunBundle(t, corpus, "development")
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	runsPath := filepath.Join(t.TempDir(), "runs.json")
	if err := os.WriteFile(runsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"validate-runs", "--corpus", corpusPath, runsPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("validate-runs exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "contains 24 validated runs") {
		t.Fatalf("validate-runs stdout=%q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"report", "--corpus", corpusPath, runsPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("report exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var report taskeval.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.EvaluationID != bundle.EvaluationID || len(report.Configurations) != 2 || report.Conclusion != "insufficient-evidence" {
		t.Fatalf("report summary = %#v", report)
	}
}

func toyRunBundle(t *testing.T, corpus *taskeval.Corpus, split string) *taskeval.RunBundle {
	t.Helper()
	promptHash := sha256Hex([]byte("toy prompt"))
	bundle := &taskeval.RunBundle{
		Version: taskeval.RunBundleVersion, EvaluationID: "toy-cli-evaluation",
		CorpusVersion: corpus.Version, RubricVersion: corpus.RubricVersion,
		Split: split, Repetitions: 3,
		Configurations: []taskeval.Configuration{
			{ID: "baseline", Role: "baseline", ToolCommit: strings.Repeat("a", 40), ToolVersion: "toy-baseline", PromptSHA256: promptHash, Provider: "fixture", RequestedModel: "fixture-v1", ResolvedModel: "fixture-v1", ModelPinned: true, Transport: "offline", GenerationSettings: "requested-tasks=1"},
			{ID: "candidate", Role: "candidate", ToolCommit: strings.Repeat("b", 40), ToolVersion: "toy-candidate", PromptSHA256: promptHash, Provider: "fixture", RequestedModel: "fixture-v2", ResolvedModel: "fixture-v2", ModelPinned: true, Transport: "offline", GenerationSettings: "requested-tasks=1"},
		},
	}
	for _, config := range bundle.Configurations {
		for i := range corpus.Cases {
			caseDef := &corpus.Cases[i]
			if caseDef.Split != split {
				continue
			}
			for repetition := 1; repetition <= bundle.Repetitions; repetition++ {
				bundle.Runs = append(bundle.Runs, toyRun(t, config.ID, caseDef, repetition))
			}
		}
	}
	return bundle
}

func toyRun(t *testing.T, configurationID string, caseDef *taskeval.Case, repetition int) taskeval.Run {
	t.Helper()
	drafts := []genai.Draft{{
		Title: "Implement the evaluated requirements", Description: "Implement the PRD requirements.",
		Details: "Use the adjudicated requirement ledger as evaluation evidence.", Priority: "high", Dependencies: []int{},
	}}
	raw, err := json.Marshal(drafts)
	if err != nil {
		t.Fatal(err)
	}
	requirements := make([]string, len(caseDef.Requirements))
	for i := range caseDef.Requirements {
		requirements[i] = caseDef.Requirements[i].ID
	}
	orderings := make([]taskeval.OrderingLabel, len(caseDef.OrderingAxioms))
	for i := range caseDef.OrderingAxioms {
		orderings[i] = taskeval.OrderingLabel{AxiomID: caseDef.OrderingAxioms[i].ID, Satisfied: true}
	}
	return taskeval.Run{
		ID:              configurationID + "-" + caseDef.ID + "-" + string(rune('0'+repetition)),
		ConfigurationID: configurationID, CaseID: caseDef.ID, Repetition: repetition,
		PRDSHA256: caseDef.PRD.SHA256, RequestedTasks: 1,
		StartedAt: "2026-08-11T00:00:00Z", DurationMilliseconds: 1,
		RawResponse: string(raw), RawResponseSHA256: sha256Hex(raw), ParsedDraftsSHA256: sha256Hex(raw),
		Outcome: "success", StopClassification: "completed", Disposition: caseDef.ExpectedDisposition,
		Drafts: drafts, Publication: taskeval.PublicationObservation{Attempted: true, Atomic: true, PostRemapVerified: true},
		Adjudication: &taskeval.Adjudication{
			Blind: true, Graders: []string{"toy-grader-a", "toy-grader-b"}, Adjudicator: "toy-adjudicator",
			TaskLabels:     []taskeval.TaskLabel{{TaskIndex: 1, RequirementIDs: requirements, Grounded: true, Actionability: 1, AcceptanceQuality: 1, PriorityCalibration: 1}},
			OrderingLabels: orderings, EdgeLabels: []taskeval.EdgeLabel{}, Usability: "no-revision",
		},
	}
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
