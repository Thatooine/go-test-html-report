package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Thatooine/go-test-html-report/assets"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"html/template"
	"io/ioutil"
	"math"
	"os"
	"strings"
	"time"
)

type GoTestJsonRowData struct {
	Time    time.Time
	Action  string
	Package string
	Test    string
	Output  string
	Elapsed float64
}

type ProcessedTestdata struct {
	TotalTestTime     string
	TestDate          string
	FailedTests       int
	PassedTests       int
	TestSummary       []TestOverview
	PackageDetailsMap map[string]PackageDetails
}

type PackageDetails struct {
	Name        string
	ElapsedTime float64
	Status      string
	Coverage    string
}

type TestDetails struct {
	PackageName string
	Name        string
	ElapsedTime float64
	Status      string
}

type TestOverview struct {
	TestSuite TestDetails
	TestCases []TestDetails
}

func main() {
	rootCmd := initCommand()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var fileName string

func initCommand() *cobra.Command {
	var rootCmd = &cobra.Command{
		Use:   "go-test-html-report",
		Long:  "go-test-html-report generates a html report of go-test logs",
		Short: "go-test-html-report generates a html report of go-test logs",
		RunE: func(cmd *cobra.Command, args []string) (e error) {
			testData := make([]GoTestJsonRowData, 0)

			file, _ := cmd.Flags().GetString("file")
			if file != "" {
				fileLogData, err := ReadLogsFromFile(file)
				if err != nil {
					log.Error().Err(err).Msg("error reading logs from a file")
					return err
				}

				testData = *fileLogData
			} else {
				stdInLogData, err := ReadLogsFromStdIn()
				if err != nil {
					log.Error().Err(err).Msg("error reading logs from standard input ")
					return err
				}

				testData = *stdInLogData
			}

			processedTestdata, err := ProcessTestData(testData)
			if err != nil {
				log.Error().Err(err).Msg("error processing test logs")
				return err
			}

			err = GenerateHTMLReport(processedTestdata.TotalTestTime,
				processedTestdata.TestDate,
				processedTestdata.FailedTests,
				processedTestdata.PassedTests,
				processedTestdata.TestSummary,
				processedTestdata.PackageDetailsMap,
			)
			if err != nil {
				log.Error().Err(err).Msg("error generating report html")
				return err
			}

			log.Info().Msgf("Report generated successfully")
			return nil
		},
	}
	rootCmd.PersistentFlags().StringVarP(&fileName,
		"file",
		"f",
		"",
		"set the file of the go test json logs")
	return rootCmd
}

func ReadLogsFromFile(fileName string) (*[]GoTestJsonRowData, error) {
	file, err := os.Open(fileName)
	if err != nil {
		log.Error().Err(err).Msg("error opening file")
		return nil, err
	}
	defer func() {
		err := file.Close()
		if err != nil {
			log.Error().Err(err).Msg("error closing file")
		}
	}()

	// file scanner
	scanner := bufio.NewScanner(file)
	rowData := make([]GoTestJsonRowData, 0)
	for scanner.Scan() {
		row := GoTestJsonRowData{}
		// unmarshall each line to GoTestJsonRowData
		err = json.Unmarshal([]byte(scanner.Text()), &row)
		if err != nil {
			log.Error().Err(err).Msg("error unmarshalling go test logs")
			return nil, err
		}
		rowData = append(rowData, row)
	}

	if err = scanner.Err(); err != nil {
		log.Error().Err(err).Msg("error scanning file")
		return nil, err
	}

	return &rowData, nil
}

func ReadLogsFromStdIn() (*[]GoTestJsonRowData, error) {
	// stdin scanner
	scanner := bufio.NewScanner(os.Stdin)
	rowData := make([]GoTestJsonRowData, 0)
	for scanner.Scan() {
		row := GoTestJsonRowData{}
		// unmarshall each line into GoTestJsonRowData
		err := json.Unmarshal([]byte(scanner.Text()), &row)
		if err != nil {
			log.Error().Err(err).Msg("error unmarshalling the test logs")
			return nil, err
		}
		rowData = append(rowData, row)
	}
	if err := scanner.Err(); err != nil {
		log.Error().Err(err).Msg("error with stdin scanner")
		return nil, err
	}

	return &rowData, nil
}
func ProcessTestData(rowData []GoTestJsonRowData) (*ProcessedTestdata, error) {
	packageDetailsMap := map[string]PackageDetails{}
	for _, r := range rowData {
		if r.Test == "" {
			if r.Action == "fail" || r.Action == "pass" || r.Action == "skip" {
				packageDetailsMap[r.Package] = PackageDetails{
					Name:        r.Package,
					ElapsedTime: r.Elapsed,
					Status:      r.Action,
					Coverage:    packageDetailsMap[r.Package].Coverage,
				}
			}

			// get package coverage data
			if r.Action == "output" {
				// check if output contains coverage data
				coverage := "-"
				if strings.Contains(r.Output, "coverage") && strings.Contains(r.Output, "%") {
					coverage = r.Output[strings.Index(r.Output, ":")+1 : strings.Index(r.Output, "%")+1]
				}
				packageDetailsMap[r.Package] = PackageDetails{
					Name:        packageDetailsMap[r.Package].Name,
					ElapsedTime: packageDetailsMap[r.Package].ElapsedTime,
					Status:      packageDetailsMap[r.Package].Status,
					Coverage:    coverage,
				}
			}
		}
	}

	testSuiteIdx := map[string]TestDetails{}
	testCasesIdx := map[string]TestDetails{}
	passedTests := 0
	failedTests := 0
	for _, r := range rowData {
		if r.Test != "" {
			testNameSlice := strings.Split(r.Test, "/")

			// if testNameSlice is not equal 1 then we assume we have a test case information. Record test case info
			if len(testNameSlice) != 1 {
				if r.Action == "fail" || r.Action == "pass" {
					testCasesIdx[r.Test] = TestDetails{
						PackageName: r.Package,
						Name:        r.Test,
						ElapsedTime: r.Elapsed,
						Status:      r.Action,
					}
				}
				if r.Action == "fail" {
					failedTests = failedTests + 1
				} else if r.Action == "pass" {
					passedTests = passedTests + 1
				}
				continue
			}

			// record test suite info
			if r.Action == "fail" || r.Action == "pass" {
				testSuiteIdx[r.Test] = TestDetails{
					PackageName: r.Package,
					Name:        r.Test,
					ElapsedTime: r.Elapsed,
					Status:      r.Action,
				}
			}
		}
	}

	//
	// group the test cases by their test suite
	//
	testSummary := make([]TestOverview, 0)
	for _, t := range testSuiteIdx {
		testCases := make([]TestDetails, 0)
		for _, t2 := range testCasesIdx {
			if strings.Contains(t2.Name, t.Name) {
				testCases = append(testCases, t2)
			}
		}
		testSummary = append(testSummary, TestOverview{
			TestSuite: t,
			TestCases: testCases,
		})
	}

	//
	// determine total test time
	//
	totalTestTime := ""
	if rowData[len(rowData)-1].Time.Sub(rowData[0].Time).Seconds() < 60 {
		totalTestTime = fmt.Sprintf("%f s", rowData[len(rowData)-1].Time.Sub(rowData[0].Time).Seconds())
	} else {
		min := int(math.Trunc(rowData[len(rowData)-1].Time.Sub(rowData[0].Time).Seconds() / 60))
		seconds := int(math.Trunc((rowData[len(rowData)-1].Time.Sub(rowData[0].Time).Minutes() - float64(min)) * 60))
		totalTestTime = fmt.Sprintf("%dm:%ds", min, seconds)
	}
	testDate := rowData[0].Time.Format(time.RFC850)

	return &ProcessedTestdata{
		TotalTestTime:     totalTestTime,
		TestDate:          testDate,
		FailedTests:       failedTests,
		PassedTests:       passedTests,
		TestSummary:       testSummary,
		PackageDetailsMap: packageDetailsMap,
	}, nil
}

func GenerateHTMLReport(totalTestTime, testDate string, failedTests, passedTests int, testSummary []TestOverview, packageDetailsMap map[string]PackageDetails) error {

	testCases, _ := generateTestCaseHTMLElements(testSummary)

	testSuites, _ := generateTestSuiteHTMLElements(testSummary, *testCases)

	reportData, _ := generatePackageDetailsHTMLElements(*testSuites, packageDetailsMap)

	reportTemplate := template.New("report-template.html")
	reportTemplateData, err := assets.Asset("report-template.html")
	if err != nil {
		log.Error().Err(err).Msg("error retrieving report-template.html")
		return err
	}

	report, err := reportTemplate.Parse(string(reportTemplateData))
	if err != nil {
		log.Error().Err(err).Msg("error parsing report-template.html")
		return err
	}

	var processedTemplate bytes.Buffer
	type templateData struct {
		HTMLElements  []template.HTML
		FailedTests   int
		PassedTests   int
		TotalTestTime string
		TestDate      string
	}

	err = report.Execute(&processedTemplate,
		&templateData{
			HTMLElements:  []template.HTML{template.HTML(reportData)},
			FailedTests:   failedTests,
			PassedTests:   passedTests,
			TotalTestTime: totalTestTime,
			TestDate:      testDate,
		},
	)
	if err != nil {
		log.Error().Err(err).Msg("error applying report-template.html")
		return err
	}

	// write the whole body at once
	err = ioutil.WriteFile("report.html", processedTemplate.Bytes(), 0644)
	if err != nil {
		log.Error().Err(err).Msg("error writing report.html file")
		return err
	}

	return nil
}

// generate test cases cards
func generateTestCaseHTMLElements(testsLogOverview []TestOverview) (*map[string][]string, error) {
	testCasesCardsMap := make(map[string][]string)
	testCaseCard := template.HTML("")

	for _, testSuite := range testsLogOverview {
		for _, testCaseDetails := range testSuite.TestCases {
			testCaseCard = `
										<div>{{.testName}}</div>
										<div>{{.elapsedTime}}s</div>
									`
			testCaseTemplate, err := template.New("testCase").Parse(string(testCaseCard))
			if err != nil {
				log.Error().Err(err).Msg("error parsing test case template")
				return nil, err
			}

			var processedTestCaseTemplate bytes.Buffer
			err = testCaseTemplate.Execute(&processedTestCaseTemplate, map[string]string{
				"testName":    testCaseDetails.Name,
				"elapsedTime": fmt.Sprintf("%f", testCaseDetails.ElapsedTime),
			})

			if err != nil {
				log.Error().Err(err).Msg("error applying test case template")
				return nil, err
			}
			if testCaseDetails.Status == "pass" {
				testCaseCard = template.HTML(
					fmt.Sprintf(`
												<div class="testCardLayout successBackgroundColor">
												%s
												</div>
											`,
						template.HTML(processedTestCaseTemplate.Bytes()),
					),
				)

			} else if testCaseDetails.Status == "fail" {
				testCaseCard = template.HTML(
					fmt.Sprintf(`
												<div class="testCardLayout failBackgroundColor ">
												%s
												</div>
												`,
						template.HTML(processedTestCaseTemplate.Bytes()),
					),
				)

			}
			testCasesCardsMap[testSuite.TestSuite.Name] = append(testCasesCardsMap[testSuite.TestSuite.Name], string(testCaseCard))
		}
	}

	return &testCasesCardsMap, nil
}

// generate test suites cards
func generateTestSuiteHTMLElements(testLogOverview []TestOverview, testCaseHTMLCards map[string][]string) (*map[string][]string, error) {
	testSuiteCollapsibleCardsMap := make(map[string][]string)
	collapsible := template.HTML("")
	collapsibleHeading := template.HTML("")
	collapsibleHeadingTemplate := ""
	collapsibleContent := template.HTML("")

	for _, testSuite := range testLogOverview {
		collapsibleHeadingTemplate = `		
										<div><p>{{.testName}}</p></div>
										<div>{{.elapsedTime}}s</div>
									`
		testCaseTemplate, err := template.New("testSuite").Parse(collapsibleHeadingTemplate)
		if err != nil {
			log.Error().Err(err).Msg("error parsing test case template")
			return nil, err
		}

		var processedTestCaseTemplate bytes.Buffer
		err = testCaseTemplate.Execute(&processedTestCaseTemplate, map[string]string{
			"testName":    testSuite.TestSuite.Name,
			"elapsedTime": fmt.Sprintf("%f", testSuite.TestSuite.ElapsedTime),
		})
		if err != nil {
			log.Error().Err(err).Msg("error applying test case template")
			return nil, err
		}

		if testSuite.TestSuite.Status == "pass" {
			collapsibleHeading = template.HTML(
				fmt.Sprintf(`
											<div class="testCardLayout successBackgroundColor collapsibleHeading">
											%s
											</div>
										`,
					template.HTML(processedTestCaseTemplate.Bytes()),
				),
			)

		} else if testSuite.TestSuite.Status == "fail" {
			collapsibleHeading = template.HTML(
				fmt.Sprintf(`
											<div class="testCardLayout failBackgroundColor collapsibleHeading">
											%s
											</div>
										`,
					template.HTML(processedTestCaseTemplate.Bytes()),
				),
			)
		}

		// construct a collapsible content
		collapsibleContent = template.HTML(
			fmt.Sprintf(`
									<div class="collapsibleHeadingContent">
										%s
									</div>
							`,
				strings.Join(testCaseHTMLCards[testSuite.TestSuite.Name], "\n"),
			),
		)

		// wrap in a collapsible
		collapsible = template.HTML(
			fmt.Sprintf(`
						<div type="button" class="collapsible">
							%s
							%s
						</div>
							`,
				string(collapsibleHeading),
				string(collapsibleContent),
			),
		)

		testSuiteCollapsibleCardsMap[testSuite.TestSuite.PackageName] = append(testSuiteCollapsibleCardsMap[testSuite.TestSuite.PackageName], string(collapsible))
	}

	return &testSuiteCollapsibleCardsMap, nil
}

// generate package cards
func generatePackageDetailsHTMLElements(testSuiteOverview map[string][]string, packageDetailsMap map[string]PackageDetails) (string, error) {
	collapsibleHeading := template.HTML("")
	collapsible := template.HTML("")
	collapsibleHeadingTemplate := ""
	collapsibleContent := template.HTML("")
	elem := make([]string, 0)

	for _, v := range packageDetailsMap {
		collapsibleHeadingTemplate = `
											<div>{{.packageName}}</div>
											<div>{{.coverage}}</div>
											<div>{{.elapsedTime}}s</div>
											`

		packageInfoTemplate, err := template.New("packageInfoTemplate").Parse(string(collapsibleHeadingTemplate))
		if err != nil {
			log.Error().Err(err).Msg("error parsing package info template")
			os.Exit(1)
		}
		var processedPackageTemplate bytes.Buffer
		err = packageInfoTemplate.Execute(&processedPackageTemplate, map[string]string{
			"packageName": v.Name,
			"elapsedTime": fmt.Sprintf("%f", v.ElapsedTime),
			"coverage":    v.Coverage,
		})
		if err != nil {
			log.Error().Err(err).Msg("error applying package info template")
			os.Exit(1)
		}

		if v.Status == "pass" {
			collapsibleHeading = template.HTML(
				fmt.Sprintf(
					`
							<div class="collapsibleHeading packageCardLayout successBackgroundColor ">
								%s
							</div>
						`,
					template.HTML(processedPackageTemplate.Bytes()),
				),
			)

		} else if v.Status == "fail" {
			collapsibleHeading = template.HTML(
				fmt.Sprintf(
					`
							<div class="collapsibleHeading packageCardLayout failBackgroundColor ">
								%s
							</div>
						`,
					template.HTML(processedPackageTemplate.Bytes()),
				),
			)

		} else {
			collapsibleHeading = template.HTML(
				fmt.Sprintf(
					`
							<div class="collapsibleHeading packageCardLayout skipBackgroundColor">
								%s
							</div>
						`,
					template.HTML(processedPackageTemplate.Bytes()),
				),
			)
		}

		// construct a collapsible content
		collapsibleContent = template.HTML(
			fmt.Sprintf(`
									<div class="collapsibleHeadingContent">
										%s
									</div>
							`,
				strings.Join(testSuiteOverview[v.Name], "\n"),
			),
		)

		// wrap in a collapsible
		collapsible = template.HTML(
			fmt.Sprintf(`
						<div type="button" class="collapsible">
							%s
							%s
						</div>
							`,
				string(collapsibleHeading),
				string(collapsibleContent),
			),
		)

		elem = append(elem, string(collapsible))
	}

	return strings.Join(elem, "\n"), nil
}
