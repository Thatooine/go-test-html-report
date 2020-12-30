package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Thatooine/go-test-html-report/assets"
	"github.com/spf13/cobra"
	"html/template"
	"io/ioutil"
	"log"
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
	packageDetailsIdx map[string]PackageDetails
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
	Test      TestDetails
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
		Short: "go-test-html-report generates a html report of go-test logs",
		RunE: func(cmd *cobra.Command, args []string) (e error) {
			file, _ := cmd.Flags().GetString("file")
			testData := ReadLogsFromFile(file)
			processedTestdata := ProcessTestData(testData)
			GenerateHTMLReport(processedTestdata.TotalTestTime,
				processedTestdata.TestDate,
				processedTestdata.FailedTests,
				processedTestdata.PassedTests,
				processedTestdata.TestSummary,
				processedTestdata.packageDetailsIdx,
			)
			log.Println("Report Generated")
			return nil
		},
	}
	rootCmd.PersistentFlags().StringVarP(&fileName,
		"file",
		"f",
		"./test.log",
		"set the file of the go test json logs")
	err := rootCmd.MarkPersistentFlagRequired("file")
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}

	return rootCmd
}

func ReadLogsFromFile(fileName string) []GoTestJsonRowData {

	file, err := os.Open(fileName)
	if err != nil {
		log.Println("error opening file: ", err)
		os.Exit(1)
	}
	defer func() {
		err := file.Close()
		if err != nil {
			log.Println("error closing file: ", err)
			os.Exit(1)
		}
	}()

	// file scanner
	scanner := bufio.NewScanner(file)
	rowData := make([]GoTestJsonRowData, 0)
	for scanner.Scan() {
		row := GoTestJsonRowData{}
		// unmarshall each line to GoTestJsonRowData
		err := json.Unmarshal([]byte(scanner.Text()), &row)
		if err != nil {
			log.Println("error to unmarshall test logs: ", err)
			os.Exit(1)
		}
		rowData = append(rowData, row)
	}

	if err := scanner.Err(); err != nil {
		log.Println("error with file scanner: ", err)
		os.Exit(1)
	}

	return rowData
}

func ReadLogsFromStdIn() []GoTestJsonRowData {
	// stdin scanner
	scanner := bufio.NewScanner(os.Stdin)
	rowData := make([]GoTestJsonRowData, 0)
	for scanner.Scan() {
		row := GoTestJsonRowData{}
		// unmarshall each line to GoTestJsonRowData
		err := json.Unmarshal([]byte(scanner.Text()), &row)
		if err != nil {
			log.Println("error to unmarshall test logs: ", err)
			os.Exit(1)
		}
		rowData = append(rowData, row)
	}
	if err := scanner.Err(); err != nil {
		log.Println("error with stdin scanner: ", err)
		os.Exit(1)
	}

	return rowData
}
func ProcessTestData(rowData []GoTestJsonRowData) ProcessedTestdata {
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
			if r.Action == "output" {
				// check if output contains coverage data
				coverage := "unk"
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

	//
	// collect gotest test details
	//
	testDetailsIdx := map[string]TestDetails{}
	testCasesIdx := map[string]TestDetails{}
	passedTests := 0
	failedTests := 0
	for _, r := range rowData {
		if r.Test != "" {
			testNameArr := strings.Split(r.Test, "/")

			// if testNameArr not equal 1 then we assume we have a test case
			if len(testNameArr) != 1 {
				if r.Action == "fail" || r.Action == "pass" || r.Action == "skip" {
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
			if r.Action == "fail" || r.Action == "pass" || r.Action == "skip" {
				testDetailsIdx[r.Test] = TestDetails{
					PackageName: r.Package,
					Name:        r.Test,
					ElapsedTime: r.Elapsed,
					Status:      r.Action,
				}
				if r.Action == "fail" {
					failedTests = failedTests + 1
				} else if r.Action == "pass" {
					passedTests = passedTests + 1
				}
			}
		}
	}

	testSummary := make([]TestOverview, 0)
	for _, t := range testDetailsIdx {
		testCases := make([]TestDetails, 0)
		for _, t2 := range testCasesIdx {
			if strings.Contains(t2.Name, t.Name) {
				testCases = append(testCases, t2)
			}
		}
		testSummary = append(testSummary, TestOverview{
			Test:      t,
			TestCases: testCases,
		})
	}

	// determine total test time
	totalTestTime := ""
	if rowData[len(rowData)-1].Time.Sub(rowData[0].Time).Seconds() < 60 {
		totalTestTime = fmt.Sprintf("%f s", rowData[len(rowData)-1].Time.Sub(rowData[0].Time).Seconds())
	} else {
		min := int(math.Trunc(rowData[len(rowData)-1].Time.Sub(rowData[0].Time).Seconds() / 60))
		seconds := int(math.Trunc((rowData[len(rowData)-1].Time.Sub(rowData[0].Time).Minutes() - float64(min)) * 60))
		totalTestTime = fmt.Sprintf("%dm:%ds", min, seconds)
	}
	testDate := rowData[0].Time.Format(time.RFC850)

	return ProcessedTestdata{
		TotalTestTime:     totalTestTime,
		TestDate:          testDate,
		FailedTests:       failedTests,
		PassedTests:       passedTests,
		TestSummary:       testSummary,
		packageDetailsIdx: packageDetailsIdx,
	}
}

func GenerateHTMLReport(totalTestTime, testDate string, failedTests, passedTests int, testSummary []TestOverview, packageDetailsIdx map[string]PackageDetails) {
	templates := make([]template.HTML, 0)
	for _, v := range packageDetailsIdx {
		htmlString := template.HTML("<div type=\"button\" class=\"collapsible\">\n")
		packageInfoTemplateString := template.HTML("")
		packageInfoTemplateString = "<div>{{.packageName}}</div>" + "\n" + "<div>{{.coverage}}</div>" + "\n" + "<div>{{.elapsedTime}}s</div>"

		packageInfoTemplate, err := template.New("packageInfoTemplate").Parse(string(packageInfoTemplateString))
		if err != nil {
			log.Println("error parsing package info template", err)
			os.Exit(1)
		}
		var processedPackageTemplate bytes.Buffer
		err = packageInfoTemplate.Execute(&processedPackageTemplate, map[string]string{
			"packageName": v.Name,
			"elapsedTime": fmt.Sprintf("%f", v.ElapsedTime),
			"coverage":    v.Coverage,
		})
		if err != nil {
			log.Println("error applying package info template: ", err)
			os.Exit(1)
		}
		if v.Status == "pass" {
			packageInfoTemplateString = "<div class=\"collapsibleHeading packageCardLayout successBackgroundColor \">" +
				template.HTML(processedPackageTemplate.Bytes()) + "</div>"
		} else if v.Status == "fail" {
			packageInfoTemplateString = "<div class=\"collapsibleHeading packageCardLayout failBackgroundColor \">" +
				template.HTML(processedPackageTemplate.Bytes()) + "</div>"
		} else {
			packageInfoTemplateString = "<div class=\"collapsibleHeading packageCardLayout skipBackgroundColor \">" +
				template.HTML(processedPackageTemplate.Bytes()) + "</div>"
		}

		htmlString = htmlString + "\n" + packageInfoTemplateString

		packageTests := make([]TestOverview, 0)
		for _, t := range testSummary {
			if t.Test.PackageName == v.Name {
				packageTests = append(packageTests, t)
			}
		}
		testInfoTemplateString := template.HTML("")
		for _, pt := range packageTests {
			testHTMLTemplateString := template.HTML("")
			// check if test contains test cases
			if len(pt.TestCases) == 0 {
				// test does not contain test cases
				testHTMLTemplateString = "<div>{{.testName}}</div>" + "\n" + "<div>{{.elapsedTime}}s</div>"
				testTemplate, err := template.New("standaloneTests").Parse(string(testHTMLTemplateString))
				if err != nil {
					log.Println("error parsing standalone tests template: ", err)
					os.Exit(1)
				}

				var processedTestTemplate bytes.Buffer
				err = testTemplate.Execute(&processedTestTemplate, map[string]string{
					"testName":    pt.Test.Name,
					"elapsedTime": fmt.Sprintf("%f", pt.Test.ElapsedTime),
				})
				if err != nil {
					log.Println("error applying standalone tests template: ", err)
					os.Exit(1)
				}

				if pt.Test.Status == "pass" {
					testHTMLTemplateString = "<div class=\"testCardLayout successBackgroundColor \">" + template.HTML(processedTestTemplate.Bytes()) + "</div>"
				} else if pt.Test.Status == "fail" {
					testHTMLTemplateString = "<div class=\"testCardLayout failBackgroundColor \">" + template.HTML(processedTestTemplate.Bytes()) + "</div>"
				} else {
					testHTMLTemplateString = "<div class=\"testCardLayout skipBackgroundColor \">" + template.HTML(processedTestTemplate.Bytes()) + "</div>"
				}
				testInfoTemplateString = testInfoTemplateString + "\n" + testHTMLTemplateString
				continue
			}

			if pt.Test.Status == "pass" {
				testHTMLTemplateString = "<div type=\"button\" class=\"collapsible \">" +
					"\n" + "<div class=\"collapsibleHeading testCardLayout successBackgroundColor \">" +
					"<div>+ {{.testName}}</div>" + "\n" + "<div>{{.elapsedTime}}s</div>" + "\n" +
					"</div>" + "\n" +
					"<div class=\"collapsibleHeadingContent\">"
			} else if pt.Test.Status == "fail" {
				testHTMLTemplateString = "<div type=\"button\" class=\"collapsible \">" +
					"\n" + "<div class=\"collapsibleHeading testCardLayout failBackgroundColor \">" +
					"<div>+ {{.testName}}</div>" + "\n" + "<div>{{.elapsedTime}}s</div>" + "\n" +
					"</div>" + "\n" +
					"<div class=\"collapsibleHeadingContent\">"
			} else {
				testHTMLTemplateString = "<div type=\"button\" class=\"collapsible \">" +
					"\n" + "<div class=\"collapsibleHeading testCardLayout skipBackgroundColor \">" +
					"<div>+ {{.testName}}</div>" + "\n" + "<div>{{.elapsedTime}}s</div>" + "\n" +
					"</div>" + "\n" +
					"<div class=\"collapsibleHeadingContent\">"
			}

			testTemplate, err := template.New("nonStandaloneTest").Parse(string(testHTMLTemplateString))
			if err != nil {
				log.Println("error parsing non standalone tests template: ", err)
				os.Exit(1)
			}

			var processedTestTemplate bytes.Buffer
			err = testTemplate.Execute(&processedTestTemplate, map[string]string{
				"testName":    pt.Test.Name,
				"elapsedTime": fmt.Sprintf("%f", pt.Test.ElapsedTime),
			})
			if err != nil {
				log.Println("error applying non standalone tests template: ", err)
				os.Exit(1)
			}
			testHTMLTemplateString = template.HTML(processedTestTemplate.Bytes())
			testCaseHTMlTemplateString := template.HTML("")
			for _, tC := range pt.TestCases {
				testCaseHTMlTemplateString = "<div>{{.testName}}</div>" + "\n" + "<div>{{.elapsedTime}}s</div>"
				testCaseTemplate, err := template.New("testCase").Parse(string(testCaseHTMlTemplateString))
				if err != nil {
					log.Println("error parsing test case template: ", err)
					os.Exit(1)
				}

				var processedTestCaseTemplate bytes.Buffer
				err = testCaseTemplate.Execute(&processedTestCaseTemplate, map[string]string{
					"testName":    tC.Name,
					"elapsedTime": fmt.Sprintf("%f", tC.ElapsedTime),
				})
				if err != nil {
					log.Println("error applying test case template: ", err)
					os.Exit(1)
				}
				if tC.Status == "pass" {
					testCaseHTMlTemplateString = "<div class=\"testCardLayout successBackgroundColor \">" + template.HTML(processedTestCaseTemplate.Bytes()) + "</div>"

				} else if tC.Status == "fail" {
					testCaseHTMlTemplateString = "<div  class=\"testCardLayout failBackgroundColor \">" + template.HTML(processedTestCaseTemplate.Bytes()) + "</div>"

				} else {
					testCaseHTMlTemplateString = "<div  class=\"testCardLayout skipBackgroundColor \">" + template.HTML(processedTestCaseTemplate.Bytes()) + "</div>"
				}
				testHTMLTemplateString = testHTMLTemplateString + "\n" + testCaseHTMlTemplateString
			}
			testHTMLTemplateString = testHTMLTemplateString + "\n" + "</div>" + "\n" + "</div>"
			testInfoTemplateString = testInfoTemplateString + "\n" + testHTMLTemplateString
		}

		htmlString = htmlString + "\n" + "<div class=\"collapsibleHeadingContent\">\n" + testInfoTemplateString + "\n" + "</div>"
		htmlString = htmlString + "\n" + "</div>"
		templates = append(templates, htmlString)
	}

	reportTemplate := template.New("report-template.html")
	reportTemplateData, err := assets.Asset("report-template.html")
	if err != nil {
		log.Println("error retrieving report-template.html: ", err)
		os.Exit(1)
	}

	report, err := reportTemplate.Parse(string(reportTemplateData))
	if err != nil {
		log.Println("error parsing report-template.html: ", err)
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
		log.Println("error applying report-template.html: ", err)
		os.Exit(1)
	}

	// write the whole body at once
	err = ioutil.WriteFile("report.html", processedTemplate.Bytes(), 0644)
	if err != nil {
		log.Println("error writing report.html file: ", err)
		os.Exit(1)
	}
}
