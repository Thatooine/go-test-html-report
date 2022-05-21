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
	PackageDetailsIdx map[string]PackageDetails
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
				processedTestdata.PackageDetailsIdx,
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
	packageDetailsIdx := map[string]PackageDetails{}
	for _, r := range rowData {
		if r.Test == "" {
			if r.Action == "fail" || r.Action == "pass" || r.Action == "skip" {
				packageDetailsIdx[r.Package] = PackageDetails{
					Name:        r.Package,
					ElapsedTime: r.Elapsed,
					Status:      r.Action,
					Coverage:    packageDetailsIdx[r.Package].Coverage,
				}
			}

			// get package coverage data
			if r.Action == "output" {
				// check if output contains coverage data
				coverage := "-"
				if strings.Contains(r.Output, "coverage") && strings.Contains(r.Output, "%") {
					coverage = r.Output[strings.Index(r.Output, ":")+1 : strings.Index(r.Output, "%")+1]
				}
				packageDetailsIdx[r.Package] = PackageDetails{
					Name:        packageDetailsIdx[r.Package].Name,
					ElapsedTime: packageDetailsIdx[r.Package].ElapsedTime,
					Status:      packageDetailsIdx[r.Package].Status,
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
		PackageDetailsIdx: packageDetailsIdx,
	}, nil
}

func GenerateHTMLReport(totalTestTime, testDate string, failedTests, passedTests int, testSummary []TestOverview, packageDetailsIdx map[string]PackageDetails) error {
	templates := make([]template.HTML, 0)
	for _, v := range packageDetailsIdx {
		htmlString := template.HTML("<div type=\"button\" class=\"collapsible\">\n")
		packageInfoHTMLTemplateString := template.HTML(`
											<div>{{.packageName}}</div>
											<div>{{.coverage}}</div>
											<div>{{.elapsedTime}}s</div>
											`)

		packageInfoTemplate, err := template.New("packageInfoTemplate").Parse(string(packageInfoHTMLTemplateString))
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
			packageInfoHTMLTemplateString = template.HTML(
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
			packageInfoHTMLTemplateString = template.HTML(
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
			packageInfoHTMLTemplateString = template.HTML(
				fmt.Sprintf(
					`
						<div class="collapsibleHeading packageCardLayout skipBackgroundColor ">
							%s
						</div>
					`,
					template.HTML(processedPackageTemplate.Bytes()),
				),
			)
		}

		htmlString = htmlString + "\n" + packageInfoHTMLTemplateString

		packageTests := make([]TestOverview, 0)
		for _, t := range testSummary {
			if t.TestSuite.PackageName == v.Name {
				packageTests = append(packageTests, t)
			}
		}
		testInfoTemplateString := template.HTML("")
		for _, pt := range packageTests {
			testHTMLTemplate := template.HTML("")
			// check if test suite contains test cases
			if len(pt.TestCases) == 0 {
				// test suite does not contain test cases
				testHTMLTemplate = `<div>{{.testName}}</div>
									<div>{{.elapsedTime}}s</div>
									`

				testTemplate, err := template.New("standaloneTests").Parse(string(testHTMLTemplate))
				if err != nil {
					log.Error().Err(err).Msg("error parsing standalone tests template")
					os.Exit(1)
				}

				var processedTestTemplate bytes.Buffer
				err = testTemplate.Execute(&processedTestTemplate, map[string]string{
					"testName":    pt.TestSuite.Name,
					"elapsedTime": fmt.Sprintf("%f", pt.TestSuite.ElapsedTime),
				})
				if err != nil {
					log.Error().Err(err).Msg("error applying standalone tests template")
					os.Exit(1)
				}

				if pt.TestSuite.Status == "pass" {
					testHTMLTemplate = template.HTML(
						fmt.Sprintf(
							`
										<div class="testCardLayout successBackgroundColor">
											%s 
										</div>
									`,
							template.HTML(processedTestTemplate.Bytes()),
						),
					)
				} else if pt.TestSuite.Status == "fail" {
					testHTMLTemplate = template.HTML(
						fmt.Sprintf(
							`
										<div class="testCardLayout failBackgroundColor">
											%s 
										</div>
													`,
							template.HTML(processedTestTemplate.Bytes()),
						),
					)
				} else {
					testHTMLTemplate = template.HTML(
						fmt.Sprintf(
							`
									<div class="testCardLayout skipBackgroundColor">
										%s 
									</div>
												`,
							template.HTML(processedTestTemplate.Bytes()),
						),
					)
				}
				testInfoTemplateString = testInfoTemplateString + testHTMLTemplate
				continue
			}

			if pt.TestSuite.Status == "pass" {
				testHTMLTemplate = `
											<div type="button" class="collapsible">
												<div class="collapsibleHeading testCardLayout successBackgroundColor">
 												<div>{{.testName}}</div>
												<div>{{.elapsedTime}}s</div>
											</div>
											`
			} else if pt.TestSuite.Status == "fail" {
				testHTMLTemplate = `
											<div type="button" class="collapsible">
												<div class="collapsibleHeading testCardLayout failBackgroundColor ">
												<div>{{.testName}}</div>
												<div>{{.elapsedTime}}s</div>
											</div>
											`
			}

			testTemplate, err := template.New("nonStandaloneTest").Parse(string(testHTMLTemplate))
			if err != nil {
				log.Error().Err(err).Msg("error parsing non standalone tests template")
				return err
			}

			var processedTestTemplate bytes.Buffer
			err = testTemplate.Execute(&processedTestTemplate, map[string]string{
				"testName":    pt.TestSuite.Name,
				"elapsedTime": fmt.Sprintf("%f", pt.TestSuite.ElapsedTime),
			})
			if err != nil {
				log.Error().Err(err).Msg("error applying non standalone tests template")
				return err
			}

			testHTMLTemplate = template.HTML(processedTestTemplate.Bytes())
			testCaseHTMlTemplateString := template.HTML("")
			for _, tC := range pt.TestCases {
				testCaseHTMlTemplateString = `
												<div>{{.testName}}</div>
												<div>{{.elapsedTime}}s</div>
											 `
				testCaseTemplate, err := template.New("testCase").Parse(string(testCaseHTMlTemplateString))
				if err != nil {
					log.Error().Err(err).Msg("error parsing test case template")
					return err
				}

				var processedTestCaseTemplate bytes.Buffer
				err = testCaseTemplate.Execute(&processedTestCaseTemplate, map[string]string{
					"testName":    tC.Name,
					"elapsedTime": fmt.Sprintf("%f", tC.ElapsedTime),
				})
				if err != nil {
					log.Error().Err(err).Msg("error applying test case template")
					return err
				}
				if tC.Status == "pass" {
					testCaseHTMlTemplateString = template.HTML(
						fmt.Sprintf(`
												<div class="testCardLayout successBackgroundColor">
												%s
												</div>
											`,
							template.HTML(processedTestCaseTemplate.Bytes()),
						),
					)

				} else if tC.Status == "fail" {
					testCaseHTMlTemplateString = template.HTML(
						fmt.Sprintf(`
												<div class="testCardLayout failBackgroundColor ">
												%s
												</div>
												`,
							template.HTML(processedTestCaseTemplate.Bytes()),
						),
					)

				}
				testHTMLTemplate = testHTMLTemplate + "\n" + testCaseHTMlTemplateString
			}
			testHTMLTemplate = testHTMLTemplate + "\n" + "</div>" + "\n" + "</div>"
			testInfoTemplateString = testInfoTemplateString + "\n" + testHTMLTemplate
		}

		htmlString = htmlString + "\n" + "<div class=\"collapsibleHeadingContent\">\n" + testInfoTemplateString + "\n" + "</div>"
		htmlString = htmlString + "\n" + "</div>"
		templates = append(templates, htmlString)
	}

	reportTemplate := template.New("report-template.html")
	reportTemplateData, err := assets.Asset("report-template.html")
	if err != nil {
		log.Error().Err(err).Msg("error retrieving report-template.html")
		os.Exit(1)
	}

	report, err := reportTemplate.Parse(string(reportTemplateData))
	if err != nil {
		log.Error().Err(err).Msg("error parsing report-template.html")
		os.Exit(1)
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
			HTMLElements:  templates,
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
