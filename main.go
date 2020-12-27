package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
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
	//
	// Collect Data
	//
	file, err := os.Open("./test.log")
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		err := file.Close()
		if err != nil {
			log.Fatal("error closing file ")
		}
	}()

	// file scanner
	scanner := bufio.NewScanner(file)
	rowData := make([]GoTestJsonRowData, 0)
	// iterate through each line of the file
	for scanner.Scan() {
		row := GoTestJsonRowData{}
		// unmarshall each line to
		err := json.Unmarshal([]byte(scanner.Text()), &row)
		if err != nil {
			log.Fatal("unable to unmarshall test log")
		}
		rowData = append(rowData, row)
		if err := scanner.Err(); err != nil {
			log.Fatal(err)
		}
	}
	//
	// Organize data
	//
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

	//
	// Build html report
	//
	templates := make([]template.HTML, 0)
	for _, v := range packageDetailsIdx {
		htmlString := template.HTML("<div type=\"button\" class=\"collapsible\">\n")
		packageInfoTemplateString := template.HTML("")
		packageInfoTemplateString = "<div>{{.packageName}}</div>" + "\n" + "<div>{{.coverage}}</div>" + "\n" + "<div>{{.elapsedTime}}s</div>"

		packageInfoTemplate, err := template.New("package").Parse(string(packageInfoTemplateString))
		if err != nil {
			panic(err)
		}
		var processedPackageTemplate bytes.Buffer
		err = packageInfoTemplate.Execute(&processedPackageTemplate, map[string]string{
			"packageName": v.Name,
			"elapsedTime": fmt.Sprintf("%f", v.ElapsedTime),
			"coverage":    v.Coverage,
		})
		if err != nil {
			panic(err)
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
		for _, t1 := range packageTests {
			testHTMLTemplateString := template.HTML("")
			// check if test contains test cases
			if len(t1.TestCases) == 0 {
				// test does not contain test cases
				testHTMLTemplateString = "<div>{{.testName}}</div>" + "\n" + "<div>{{.elapsedTime}}s</div>"
				testTemplate, err := template.New("test").Parse(string(testHTMLTemplateString))
				if err != nil {
					panic(err)
				}

				var processedTestTemplate bytes.Buffer
				err = testTemplate.Execute(&processedTestTemplate, map[string]string{
					"testName":    t1.Test.Name,
					"elapsedTime": fmt.Sprintf("%f", t1.Test.ElapsedTime),
				})
				if err != nil {
					panic(err)
				}

				if t1.Test.Status == "pass" {
					testHTMLTemplateString = "<div class=\"testCardLayout successBackgroundColor \">" + template.HTML(processedTestTemplate.Bytes()) + "</div>"
				} else if t1.Test.Status == "fail" {
					testHTMLTemplateString = "<div class=\"testCardLayout failBackgroundColor \">" + template.HTML(processedTestTemplate.Bytes()) + "</div>"
				} else {
					testHTMLTemplateString = "<div class=\"testCardLayout skipBackgroundColor \">" + template.HTML(processedTestTemplate.Bytes()) + "</div>"
				}
				testInfoTemplateString = testInfoTemplateString + "\n" + testHTMLTemplateString
				continue
			}

			if t1.Test.Status == "pass" {
				testHTMLTemplateString = "<div type=\"button\" class=\"collapsible \">" +
					"\n" + "<div class=\"collapsibleHeading testCardLayout successBackgroundColor \">" +
					"<div>+ {{.testName}}</div>" + "\n" + "<div>{{.elapsedTime}}s</div>" + "\n" +
					"</div>" + "\n" +
					"<div class=\"collapsibleHeadingContent\">"
			} else if t1.Test.Status == "fail" {
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

			testTemplate, err := template.New("test").Parse(string(testHTMLTemplateString))
			if err != nil {
				panic(err)
			}

			var processedTestTemplate bytes.Buffer
			err = testTemplate.Execute(&processedTestTemplate, map[string]string{
				"testName":    t1.Test.Name,
				"elapsedTime": fmt.Sprintf("%f", t1.Test.ElapsedTime),
			})
			if err != nil {
				panic(err)
			}
			testHTMLTemplateString = template.HTML(processedTestTemplate.Bytes())
			testCaseHTMlTemplateString := template.HTML("")
			for _, tC := range t1.TestCases {
				testCaseHTMlTemplateString = "<div>{{.testName}}</div>" + "\n" + "<div>{{.elapsedTime}}s</div>"
				testCaseTemplate, err := template.New("testCase").Parse(string(testCaseHTMlTemplateString))
				if err != nil {
					panic(err)
				}

				var processedTestCaseTemplate bytes.Buffer
				err = testCaseTemplate.Execute(&processedTestCaseTemplate, map[string]string{
					"testName":    tC.Name,
					"elapsedTime": fmt.Sprintf("%f", tC.ElapsedTime),
				})
				if err != nil {
					panic(err)
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

	report, err := template.ParseFiles("./report-template.html")
	if err != nil {
		log.Fatal("error parsing report html")
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
			TestDate:      rowData[0].Time.Format(time.RFC850),
		},
	)
	if err != nil {
		log.Print(err.Error())
	}

	// write the whole body at once
	err = ioutil.WriteFile("report.html", processedTemplate.Bytes(), 0644)
	if err != nil {
		panic(err)
	}
}
