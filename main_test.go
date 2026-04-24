package main

import "testing"

func TestProcessTestDataBuildsStructuredReport(t *testing.T) {
	rows := []GoTestJsonRowData{
		{Package: "example/math", Test: "TestAdd", Action: "run"},
		{Package: "example/math", Test: "TestAdd", Action: "output", Output: "=== RUN   TestAdd\n"},
		{Package: "example/math", Test: "TestAdd/carries", Action: "output", Output: "expected carry propagation\n"},
		{Package: "example/math", Test: "TestAdd/carries", Action: "pass", Elapsed: 0.008},
		{Package: "example/math", Test: "TestAdd", Action: "pass", Elapsed: 0.012},
		{Package: "example/math", Test: "TestDivide", Action: "output", Output: "division by zero panic\nstack trace line\n"},
		{Package: "example/math", Test: "TestDivide", Action: "fail", Elapsed: 1.24},
		{Package: "example/math", Action: "output", Output: "coverage: 83.3% of statements\n"},
		{Package: "example/math", Action: "fail", Elapsed: 1.40},
		{Package: "example/api", Test: "TestClient", Action: "skip", Elapsed: 0.004},
		{Package: "example/api", Action: "pass", Elapsed: 0.01},
	}

	report, err := ProcessTestData(rows)
	if err != nil {
		t.Fatalf("ProcessTestData returned error: %v", err)
	}

	if got, want := report.Metrics.FailedTests, 1; got != want {
		t.Fatalf("failed tests = %d, want %d", got, want)
	}
	if got, want := report.Metrics.PassedTests, 2; got != want {
		t.Fatalf("passed tests = %d, want %d", got, want)
	}
	if got, want := report.Metrics.SkippedTests, 1; got != want {
		t.Fatalf("skipped tests = %d, want %d", got, want)
	}
	if got, want := report.Metrics.CoverageAverage, "83.3%"; got != want {
		t.Fatalf("coverage average = %q, want %q", got, want)
	}
	if len(report.Packages) != 2 {
		t.Fatalf("packages = %d, want 2", len(report.Packages))
	}

	failingPkg := report.Packages[0]
	if failingPkg.Name != "example/math" {
		t.Fatalf("first package = %q, want failing package first", failingPkg.Name)
	}
	if failingPkg.Status != "fail" {
		t.Fatalf("package status = %q, want fail", failingPkg.Status)
	}
	if failingPkg.CoverageLabel != "83.3% covered" {
		t.Fatalf("coverage label = %q", failingPkg.CoverageLabel)
	}

	if len(failingPkg.Suites) != 2 {
		t.Fatalf("suite count = %d, want 2", len(failingPkg.Suites))
	}

	if failingPkg.Suites[0].Name != "TestDivide" {
		t.Fatalf("first suite = %q, want failed suite first", failingPkg.Suites[0].Name)
	}
	if !failingPkg.Suites[0].HasOutput {
		t.Fatalf("expected failed suite output to be captured")
	}
	if failingPkg.Suites[1].TotalCases != 1 {
		t.Fatalf("subtest count = %d, want 1", failingPkg.Suites[1].TotalCases)
	}
	if failingPkg.Suites[1].Cases[0].ShortName != "carries" {
		t.Fatalf("short case name = %q, want carries", failingPkg.Suites[1].Cases[0].ShortName)
	}
}

func TestProcessTestDataRejectsEmptyInput(t *testing.T) {
	if _, err := ProcessTestData(nil); err == nil {
		t.Fatal("expected error for empty input")
	}
}
