package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed assets/report-template.html
var reportTemplateHTML string

var coveragePattern = regexp.MustCompile(`coverage:\s*([0-9]+(?:\.[0-9]+)?)%`)

type GoTestJsonRowData struct {
	Time    time.Time
	Action  string
	Package string
	Test    string
	Output  string
	Elapsed float64
}

type ReportData struct {
	Title            string
	TestDate         string
	GeneratedAt      string
	TotalTestTime    string
	PackageCount     int
	HasCoverage      bool
	Metrics          ReportMetrics
	Packages         []PackageReport
	SlowestTests     []SpotlightTest
	FailureSpotlight []FailureSpotlight
}

type ReportMetrics struct {
	TotalTests      int
	PassedTests     int
	FailedTests     int
	SkippedTests    int
	SuiteCount      int
	SubtestCount    int
	FailedPackages  int
	PassRate        string
	CoverageAverage string
}

type PackageReport struct {
	Name           string
	Status         string
	StatusLabel    string
	StatusClass    string
	DurationLabel  string
	CoverageLabel  string
	CoverageWidth  string
	PassRate       string
	TotalTests     int
	PassedTests    int
	FailedTests    int
	SkippedTests   int
	FailureCount   int
	SearchText     string
	HasFailures    bool
	SuiteCount     int
	Suites         []SuiteReport
	durationValue  float64
	coverageValue  float64
	coverageExists bool
}

type SuiteReport struct {
	Name          string
	Status        string
	StatusLabel   string
	StatusClass   string
	DurationLabel string
	Output        string
	HasOutput     bool
	HasFailures   bool
	TotalCases    int
	PassedCases   int
	FailedCases   int
	SkippedCases  int
	Cases         []CaseReport
	durationValue float64
}

type CaseReport struct {
	Name          string
	ShortName     string
	Status        string
	StatusLabel   string
	StatusClass   string
	DurationLabel string
	Output        string
	HasOutput     bool
	durationValue float64
}

type SpotlightTest struct {
	Label         string
	PackageName   string
	Status        string
	StatusLabel   string
	StatusClass   string
	DurationLabel string
	durationValue float64
}

type FailureSpotlight struct {
	Label         string
	PackageName   string
	StatusLabel   string
	StatusClass   string
	DurationLabel string
	Output        string
	durationValue float64
}

type packageAccumulator struct {
	name           string
	explicitStatus string
	elapsed        float64
	coverageLabel  string
	coverageValue  float64
	coverageSeen   bool
	suites         map[string]*suiteAccumulator
}

type suiteAccumulator struct {
	name           string
	explicitStatus string
	elapsed        float64
	outputs        []string
	cases          map[string]*caseAccumulator
}

type caseAccumulator struct {
	name           string
	shortName      string
	explicitStatus string
	elapsed        float64
	outputs        []string
}

var fileName string
var outputDirectory string

func main() {
	if err := run(); err != nil {
		log.Printf("go-test-html-report: %v", err)
		os.Exit(1)
	}
}

func run() error {
	flag.StringVar(&fileName, "file", "", "set the file of the go test json logs")
	flag.StringVar(&fileName, "f", "", "set the file of the go test json logs")
	flag.StringVar(&outputDirectory, "output", "", "set the output directory of the HTML report")
	flag.StringVar(&outputDirectory, "o", "", "set the output directory of the HTML report")
	flag.Parse()

	var (
		testData []GoTestJsonRowData
		err      error
	)

	if fileName != "" {
		testData, err = ReadLogsFromFile(fileName)
		if err != nil {
			return fmt.Errorf("read logs from file: %w", err)
		}
	} else {
		testData, err = ReadLogsFromStdIn()
		if err != nil {
			return fmt.Errorf("read logs from stdin: %w", err)
		}
	}

	reportData, err := ProcessTestData(testData)
	if err != nil {
		return fmt.Errorf("process test data: %w", err)
	}

	if err := GenerateHTMLReport(reportData); err != nil {
		return fmt.Errorf("generate report HTML: %w", err)
	}

	log.Print("report generated successfully")
	return nil
}

func ReadLogsFromFile(fileName string) ([]GoTestJsonRowData, error) {
	file, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.Printf("error closing file: %v", closeErr)
		}
	}()

	return scanRows(bufio.NewScanner(file))
}

func ReadLogsFromStdIn() ([]GoTestJsonRowData, error) {
	return scanRows(bufio.NewScanner(os.Stdin))
}

func scanRows(scanner *bufio.Scanner) ([]GoTestJsonRowData, error) {
	rows := make([]GoTestJsonRowData, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var row GoTestJsonRowData
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return rows, nil
}

func ProcessTestData(rowData []GoTestJsonRowData) (*ReportData, error) {
	if len(rowData) == 0 {
		return nil, fmt.Errorf("no go test rows were supplied")
	}

	packages := make(map[string]*packageAccumulator)
	firstTimestamp := rowData[0].Time
	lastTimestamp := rowData[0].Time

	for _, row := range rowData {
		if !row.Time.IsZero() {
			if row.Time.Before(firstTimestamp) {
				firstTimestamp = row.Time
			}
			if row.Time.After(lastTimestamp) {
				lastTimestamp = row.Time
			}
		}

		if row.Package == "" {
			continue
		}

		pkg := ensurePackage(packages, row.Package)
		if row.Test == "" {
			processPackageRow(pkg, row)
			continue
		}

		processTestRow(pkg, row)
	}

	report := buildReport(packages)
	report.Title = "Go Test Report"
	report.TestDate = firstTimestamp.Format("02 Jan 2006 15:04 MST")
	report.GeneratedAt = time.Now().Format("02 Jan 2006 15:04 MST")
	report.TotalTestTime = formatDuration(lastTimestamp.Sub(firstTimestamp).Seconds())

	return report, nil
}

func processPackageRow(pkg *packageAccumulator, row GoTestJsonRowData) {
	if row.Action == "output" {
		if coverageLabel, coverageValue, ok := extractCoverage(row.Output); ok {
			pkg.coverageLabel = coverageLabel
			pkg.coverageValue = coverageValue
			pkg.coverageSeen = true
		}
		return
	}

	if isTerminalAction(row.Action) {
		pkg.explicitStatus = row.Action
		pkg.elapsed = row.Elapsed
	}
}

func processTestRow(pkg *packageAccumulator, row GoTestJsonRowData) {
	parts := strings.Split(row.Test, "/")
	suite := ensureSuite(pkg, parts[0])

	if len(parts) == 1 {
		if row.Action == "output" {
			suite.outputs = appendCleanOutput(suite.outputs, row.Output)
			return
		}
		if isTerminalAction(row.Action) {
			suite.explicitStatus = row.Action
			suite.elapsed = row.Elapsed
		}
		return
	}

	testCase := ensureCase(suite, row.Test)
	if row.Action == "output" {
		testCase.outputs = appendCleanOutput(testCase.outputs, row.Output)
		return
	}
	if isTerminalAction(row.Action) {
		testCase.explicitStatus = row.Action
		testCase.elapsed = row.Elapsed
	}
}

func buildReport(packages map[string]*packageAccumulator) *ReportData {
	report := &ReportData{}
	packageNames := make([]string, 0, len(packages))
	for name := range packages {
		packageNames = append(packageNames, name)
	}
	sort.Strings(packageNames)

	slowest := make([]SpotlightTest, 0)
	failures := make([]FailureSpotlight, 0)

	for _, packageName := range packageNames {
		pkgReport, pkgSlowest, pkgFailures := buildPackageReport(packages[packageName])
		report.Packages = append(report.Packages, pkgReport)
		slowest = append(slowest, pkgSlowest...)
		failures = append(failures, pkgFailures...)

		report.Metrics.PassedTests += pkgReport.PassedTests
		report.Metrics.FailedTests += pkgReport.FailedTests
		report.Metrics.SkippedTests += pkgReport.SkippedTests
		report.Metrics.SuiteCount += pkgReport.SuiteCount
		for _, suite := range pkgReport.Suites {
			report.Metrics.SubtestCount += suite.TotalCases
		}
		if pkgReport.HasFailures {
			report.Metrics.FailedPackages++
		}
		if pkgReport.coverageExists {
			report.HasCoverage = true
		}
	}

	sortPackages(report.Packages)
	report.PackageCount = len(report.Packages)
	report.Metrics.TotalTests = report.Metrics.PassedTests + report.Metrics.FailedTests + report.Metrics.SkippedTests
	report.Metrics.PassRate = formatPercent(report.Metrics.PassedTests, report.Metrics.TotalTests)
	report.Metrics.CoverageAverage = calculateCoverageAverage(report.Packages)
	report.SlowestTests = topSlowest(slowest, 8)
	report.FailureSpotlight = topFailures(failures, 6)

	return report
}

func buildPackageReport(pkg *packageAccumulator) (PackageReport, []SpotlightTest, []FailureSpotlight) {
	report := PackageReport{
		Name:           pkg.name,
		DurationLabel:  formatDuration(pkg.elapsed),
		CoverageLabel:  "No coverage data",
		CoverageWidth:  "0%",
		coverageValue:  pkg.coverageValue,
		coverageExists: pkg.coverageSeen,
		durationValue:  pkg.elapsed,
	}
	if pkg.coverageSeen {
		report.CoverageLabel = pkg.coverageLabel
		report.CoverageWidth = fmt.Sprintf("%.1f%%", pkg.coverageValue)
	}

	suiteNames := make([]string, 0, len(pkg.suites))
	for name := range pkg.suites {
		suiteNames = append(suiteNames, name)
	}
	sort.Strings(suiteNames)

	slowest := make([]SpotlightTest, 0)
	failures := make([]FailureSpotlight, 0)
	childStatuses := make([]string, 0, len(suiteNames))

	for _, suiteName := range suiteNames {
		suiteReport, suiteSlowest, suiteFailures := buildSuiteReport(pkg.name, pkg.suites[suiteName])
		report.Suites = append(report.Suites, suiteReport)
		slowest = append(slowest, suiteSlowest...)
		failures = append(failures, suiteFailures...)
		report.PassedTests += countByStatus(suiteReport.Status, "pass")
		report.FailedTests += countByStatus(suiteReport.Status, "fail")
		report.SkippedTests += countByStatus(suiteReport.Status, "skip")
		report.PassedTests += suiteReport.PassedCases
		report.FailedTests += suiteReport.FailedCases
		report.SkippedTests += suiteReport.SkippedCases
		report.SuiteCount++
		report.TotalTests += 1 + suiteReport.TotalCases
		report.FailureCount += suiteReport.FailedCases
		if suiteReport.Status == "fail" {
			report.FailureCount++
		}
		if suiteReport.Status == "fail" || suiteReport.FailedCases > 0 {
			report.HasFailures = true
		}
		childStatuses = append(childStatuses, suiteReport.Status)
	}

	report.Status = aggregateStatus(pkg.explicitStatus, childStatuses)
	report.StatusLabel = statusLabel(report.Status)
	report.StatusClass = statusClass(report.Status)
	report.PassRate = formatPercent(report.PassedTests, report.TotalTests)
	report.SearchText = strings.ToLower(buildPackageSearchText(report))

	sortSuites(report.Suites)

	return report, slowest, failures
}

func buildSuiteReport(packageName string, suite *suiteAccumulator) (SuiteReport, []SpotlightTest, []FailureSpotlight) {
	report := SuiteReport{
		Name:          suite.name,
		DurationLabel: formatDuration(suite.elapsed),
		Output:        strings.Join(suite.outputs, "\n"),
		HasOutput:     len(suite.outputs) > 0,
		durationValue: suite.elapsed,
	}

	caseNames := make([]string, 0, len(suite.cases))
	for name := range suite.cases {
		caseNames = append(caseNames, name)
	}
	sort.Strings(caseNames)

	childStatuses := make([]string, 0, len(caseNames))
	slowest := []SpotlightTest{
		{
			Label:         suite.name,
			PackageName:   packageName,
			Status:        report.Status,
			StatusLabel:   statusLabel(report.Status),
			StatusClass:   statusClass(report.Status),
			DurationLabel: report.DurationLabel,
			durationValue: suite.elapsed,
		},
	}
	failures := make([]FailureSpotlight, 0)

	for _, caseName := range caseNames {
		caseReport := buildCaseReport(suite.cases[caseName])
		report.Cases = append(report.Cases, caseReport)
		report.TotalCases++
		report.PassedCases += countByStatus(caseReport.Status, "pass")
		report.FailedCases += countByStatus(caseReport.Status, "fail")
		report.SkippedCases += countByStatus(caseReport.Status, "skip")
		if caseReport.Status == "fail" {
			report.HasFailures = true
			failures = append(failures, FailureSpotlight{
				Label:         caseReport.Name,
				PackageName:   packageName,
				StatusLabel:   caseReport.StatusLabel,
				StatusClass:   caseReport.StatusClass,
				DurationLabel: caseReport.DurationLabel,
				Output:        caseReport.Output,
				durationValue: caseReport.durationValue,
			})
		}
		slowest = append(slowest, SpotlightTest{
			Label:         caseReport.Name,
			PackageName:   packageName,
			Status:        caseReport.Status,
			StatusLabel:   caseReport.StatusLabel,
			StatusClass:   caseReport.StatusClass,
			DurationLabel: caseReport.DurationLabel,
			durationValue: caseReport.durationValue,
		})
		childStatuses = append(childStatuses, caseReport.Status)
	}

	report.Status = aggregateStatus(suite.explicitStatus, childStatuses)
	report.StatusLabel = statusLabel(report.Status)
	report.StatusClass = statusClass(report.Status)
	if report.HasOutput && report.Status == "fail" {
		failures = append(failures, FailureSpotlight{
			Label:         suite.name,
			PackageName:   packageName,
			StatusLabel:   report.StatusLabel,
			StatusClass:   report.StatusClass,
			DurationLabel: report.DurationLabel,
			Output:        report.Output,
			durationValue: suite.elapsed,
		})
	}
	sortCases(report.Cases)
	slowest[0].Status = report.Status
	slowest[0].StatusLabel = report.StatusLabel
	slowest[0].StatusClass = report.StatusClass

	return report, slowest, failures
}

func buildCaseReport(testCase *caseAccumulator) CaseReport {
	status := aggregateStatus(testCase.explicitStatus, nil)
	return CaseReport{
		Name:          testCase.name,
		ShortName:     testCase.shortName,
		Status:        status,
		StatusLabel:   statusLabel(status),
		StatusClass:   statusClass(status),
		DurationLabel: formatDuration(testCase.elapsed),
		Output:        strings.Join(testCase.outputs, "\n"),
		HasOutput:     len(testCase.outputs) > 0,
		durationValue: testCase.elapsed,
	}
}

func GenerateHTMLReport(data *ReportData) error {
	report, err := template.New("report-template").Parse(reportTemplateHTML)
	if err != nil {
		return err
	}

	var processedTemplate bytes.Buffer
	if err := report.Execute(&processedTemplate, data); err != nil {
		return err
	}

	path := "report.html"
	if outputDirectory != "" {
		if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
			return err
		}
		path = filepath.Join(outputDirectory, "report.html")
	}

	return os.WriteFile(path, processedTemplate.Bytes(), 0o644)
}

func ensurePackage(packages map[string]*packageAccumulator, packageName string) *packageAccumulator {
	if pkg, ok := packages[packageName]; ok {
		return pkg
	}

	pkg := &packageAccumulator{
		name:   packageName,
		suites: make(map[string]*suiteAccumulator),
	}
	packages[packageName] = pkg
	return pkg
}

func ensureSuite(pkg *packageAccumulator, suiteName string) *suiteAccumulator {
	if suite, ok := pkg.suites[suiteName]; ok {
		return suite
	}

	suite := &suiteAccumulator{
		name:  suiteName,
		cases: make(map[string]*caseAccumulator),
	}
	pkg.suites[suiteName] = suite
	return suite
}

func ensureCase(suite *suiteAccumulator, caseName string) *caseAccumulator {
	if testCase, ok := suite.cases[caseName]; ok {
		return testCase
	}

	parts := strings.Split(caseName, "/")
	testCase := &caseAccumulator{
		name:      caseName,
		shortName: parts[len(parts)-1],
	}
	suite.cases[caseName] = testCase
	return testCase
}

func appendCleanOutput(existing []string, raw string) []string {
	for _, line := range strings.Split(raw, "\n") {
		cleaned, keep := sanitizeOutputLine(line)
		if keep {
			existing = append(existing, cleaned)
		}
	}
	return existing
}

func sanitizeOutputLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", false
	}

	skipPrefixes := []string{
		"=== RUN",
		"=== PAUSE",
		"=== CONT",
		"--- PASS:",
		"--- FAIL:",
		"--- SKIP:",
		"PASS",
		"FAIL",
		"ok  ",
		"?   ",
	}
	for _, prefix := range skipPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return "", false
		}
	}

	return trimmed, true
}

func extractCoverage(output string) (string, float64, bool) {
	match := coveragePattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return "", 0, false
	}

	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return "", 0, false
	}

	return fmt.Sprintf("%.1f%% covered", value), value, true
}

func aggregateStatus(explicitStatus string, childStatuses []string) string {
	if explicitStatus != "" {
		return explicitStatus
	}
	if hasStatus(childStatuses, "fail") {
		return "fail"
	}
	if hasStatus(childStatuses, "pass") {
		return "pass"
	}
	if hasStatus(childStatuses, "skip") {
		return "skip"
	}
	return "unknown"
}

func hasStatus(statuses []string, target string) bool {
	for _, status := range statuses {
		if status == target {
			return true
		}
	}
	return false
}

func formatDuration(seconds float64) string {
	if seconds <= 0 {
		return "0ms"
	}
	if seconds >= 60 {
		duration := time.Duration(seconds * float64(time.Second))
		minutes := int(duration / time.Minute)
		remainder := duration - (time.Duration(minutes) * time.Minute)
		return fmt.Sprintf("%dm %02ds", minutes, int(remainder/time.Second))
	}
	if seconds >= 1 {
		return fmt.Sprintf("%.2fs", seconds)
	}
	return fmt.Sprintf("%dms", int(seconds*1000))
}

func formatPercent(part, total int) string {
	if total == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.0f%%", (float64(part)/float64(total))*100)
}

func calculateCoverageAverage(packages []PackageReport) string {
	total := 0.0
	count := 0
	for _, pkg := range packages {
		if pkg.coverageExists {
			total += pkg.coverageValue
			count++
		}
	}
	if count == 0 {
		return "N/A"
	}
	return fmt.Sprintf("%.1f%%", total/float64(count))
}

func buildPackageSearchText(pkg PackageReport) string {
	parts := []string{pkg.Name}
	for _, suite := range pkg.Suites {
		parts = append(parts, suite.Name, suite.Output)
		for _, testCase := range suite.Cases {
			parts = append(parts, testCase.Name, testCase.Output)
		}
	}
	return strings.Join(parts, " ")
}

func sortPackages(packages []PackageReport) {
	sort.SliceStable(packages, func(i, j int) bool {
		if statusPriority(packages[i].Status) != statusPriority(packages[j].Status) {
			return statusPriority(packages[i].Status) < statusPriority(packages[j].Status)
		}
		if packages[i].FailedTests != packages[j].FailedTests {
			return packages[i].FailedTests > packages[j].FailedTests
		}
		if packages[i].durationValue != packages[j].durationValue {
			return packages[i].durationValue > packages[j].durationValue
		}
		return packages[i].Name < packages[j].Name
	})
}

func sortSuites(suites []SuiteReport) {
	sort.SliceStable(suites, func(i, j int) bool {
		if statusPriority(suites[i].Status) != statusPriority(suites[j].Status) {
			return statusPriority(suites[i].Status) < statusPriority(suites[j].Status)
		}
		if suites[i].FailedCases != suites[j].FailedCases {
			return suites[i].FailedCases > suites[j].FailedCases
		}
		if suites[i].durationValue != suites[j].durationValue {
			return suites[i].durationValue > suites[j].durationValue
		}
		return suites[i].Name < suites[j].Name
	})
}

func sortCases(cases []CaseReport) {
	sort.SliceStable(cases, func(i, j int) bool {
		if statusPriority(cases[i].Status) != statusPriority(cases[j].Status) {
			return statusPriority(cases[i].Status) < statusPriority(cases[j].Status)
		}
		if cases[i].durationValue != cases[j].durationValue {
			return cases[i].durationValue > cases[j].durationValue
		}
		return cases[i].Name < cases[j].Name
	})
}

func topSlowest(items []SpotlightTest, limit int) []SpotlightTest {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].durationValue != items[j].durationValue {
			return items[i].durationValue > items[j].durationValue
		}
		return items[i].Label < items[j].Label
	})
	if len(items) > limit {
		return items[:limit]
	}
	return items
}

func topFailures(items []FailureSpotlight, limit int) []FailureSpotlight {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].durationValue != items[j].durationValue {
			return items[i].durationValue > items[j].durationValue
		}
		return items[i].Label < items[j].Label
	})
	if len(items) > limit {
		return items[:limit]
	}
	return items
}

func isTerminalAction(action string) bool {
	return action == "pass" || action == "fail" || action == "skip"
}

func countByStatus(status, target string) int {
	if status == target {
		return 1
	}
	return 0
}

func statusPriority(status string) int {
	switch status {
	case "fail":
		return 0
	case "pass":
		return 1
	case "skip":
		return 2
	default:
		return 3
	}
}

func statusLabel(status string) string {
	switch status {
	case "pass":
		return "Passed"
	case "fail":
		return "Failed"
	case "skip":
		return "Skipped"
	default:
		return "Unknown"
	}
}

func statusClass(status string) string {
	switch status {
	case "pass":
		return "status-pass"
	case "fail":
		return "status-fail"
	case "skip":
		return "status-skip"
	default:
		return "status-unknown"
	}
}
